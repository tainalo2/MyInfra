package provision

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

func (c Client) createInfomaniak(ctx context.Context, id, secret string, spec Spec) (Machine, error) {
	base := c.base(c.InfomaniakBase, "https://api.pub1.infomaniak.cloud")
	token, endpoints, err := infomaniakToken(ctx, c, base, id, secret)
	if err != nil {
		return Machine{}, err
	}
	header := map[string]string{"X-Auth-Token": token}
	compute := endpoint(endpoints, "compute", spec.Location)
	imageURL := endpoint(endpoints, "image", spec.Location)
	networkURL := endpoint(endpoints, "network", spec.Location)
	if compute == "" || imageURL == "" || networkURL == "" {
		return Machine{}, fmt.Errorf("Le provider ne répond pas.")
	}
	image, err := c.infomaniakImage(ctx, imageURL, header)
	if err != nil {
		return Machine{}, err
	}
	network, err := c.infomaniakNetwork(ctx, networkURL, header)
	if err != nil {
		return Machine{}, err
	}
	status, body, err := c.call(ctx, http.MethodPost, strings.TrimRight(compute, "/")+"/servers", header, map[string]any{
		"server": map[string]any{
			"name":      spec.Name,
			"flavorRef": spec.OfferID,
			"imageRef":  image,
			"user_data": base64.StdEncoding.EncodeToString([]byte(spec.UserData)),
			"networks":  []map[string]string{{"uuid": network}},
		},
	})
	if err != nil {
		return Machine{}, err
	}
	if status >= 300 {
		return Machine{}, refused(status, body)
	}
	var created struct {
		Server struct {
			ID flexID `json:"id"`
		} `json:"server"`
	}
	if json.Unmarshal(body, &created) != nil || created.Server.ID == "" {
		return Machine{}, refused(status, body)
	}
	serverID := string(created.Server.ID)
	ip, err := waitIP(ctx, func() (string, error) {
		status, body, err := c.call(ctx, http.MethodGet, strings.TrimRight(compute, "/")+"/servers/"+serverID, header, nil)
		if err != nil {
			return "", err
		}
		if status >= 300 {
			return "", refused(status, body)
		}
		return infomaniakIP(body), nil
	})
	if err != nil {
		return Machine{}, err
	}
	return Machine{ID: spec.Location + "/" + serverID, IPv4: ip}, nil
}

func (c Client) destroyInfomaniak(ctx context.Context, id, secret, server string) error {
	region, serverID, ok := strings.Cut(server, "/")
	if !ok {
		return fmt.Errorf("Machine introuvable.")
	}
	base := c.base(c.InfomaniakBase, "https://api.pub1.infomaniak.cloud")
	token, endpoints, err := infomaniakToken(ctx, c, base, id, secret)
	if err != nil {
		return err
	}
	compute := endpoint(endpoints, "compute", region)
	if compute == "" {
		return fmt.Errorf("Le provider ne répond pas.")
	}
	return c.destroy(ctx, strings.TrimRight(compute, "/")+"/servers/"+serverID, map[string]string{"X-Auth-Token": token})
}

type infoEndpoint struct {
	Type   string
	Region string
	URL    string
}

func infomaniakToken(ctx context.Context, c Client, base, id, secret string) (string, []infoEndpoint, error) {
	payload, _ := json.Marshal(map[string]any{
		"auth": map[string]any{
			"identity": map[string]any{
				"methods": []string{"application_credential"},
				"application_credential": map[string]string{
					"id":     strings.TrimSpace(id),
					"secret": strings.TrimSpace(secret),
				},
			},
		},
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/identity/v3/auth/tokens", bytes.NewReader(payload))
	if err != nil {
		return "", nil, fmt.Errorf("Le provider ne répond pas.")
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := c.http().Do(req)
	if err != nil {
		return "", nil, fmt.Errorf("Le provider ne répond pas.")
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	if res.StatusCode != http.StatusOK && res.StatusCode != http.StatusCreated {
		return "", nil, refused(res.StatusCode, body)
	}
	token := strings.TrimSpace(res.Header.Get("X-Subject-Token"))
	if token == "" {
		return "", nil, fmt.Errorf("identifiants refusés")
	}
	var parsed struct {
		Token struct {
			Catalog []struct {
				Type      string `json:"type"`
				Endpoints []struct {
					Interface string `json:"interface"`
					Region    string `json:"region"`
					RegionID  string `json:"region_id"`
					URL       string `json:"url"`
				} `json:"endpoints"`
			} `json:"catalog"`
		} `json:"token"`
	}
	if json.Unmarshal(body, &parsed) != nil {
		return "", nil, fmt.Errorf("Le provider ne répond pas.")
	}
	var endpoints []infoEndpoint
	for _, service := range parsed.Token.Catalog {
		for _, item := range service.Endpoints {
			if item.Interface != "" && item.Interface != "public" {
				continue
			}
			region := item.Region
			if region == "" {
				region = item.RegionID
			}
			if item.URL == "" || region == "" {
				continue
			}
			endpoints = append(endpoints, infoEndpoint{Type: service.Type, Region: region, URL: item.URL})
		}
	}
	return token, endpoints, nil
}

func endpoint(endpoints []infoEndpoint, kind, region string) string {
	for _, item := range endpoints {
		if item.Type == kind && item.Region == region {
			return item.URL
		}
	}
	return ""
}

func (c Client) infomaniakImage(ctx context.Context, imageURL string, header map[string]string) (string, error) {
	status, body, err := c.call(ctx, http.MethodGet, strings.TrimRight(imageURL, "/")+"/v2/images", header, nil)
	if err != nil {
		return "", err
	}
	if status >= 300 {
		return "", refused(status, body)
	}
	var payload struct {
		Images []struct {
			ID     string `json:"id"`
			Name   string `json:"name"`
			Status string `json:"status"`
		} `json:"images"`
	}
	if json.Unmarshal(body, &payload) != nil {
		return "", fmt.Errorf("Image Debian introuvable.")
	}
	var chosen string
	for _, image := range payload.Images {
		name := strings.ToLower(image.Name)
		if image.ID == "" || (image.Status != "" && image.Status != "active") || !strings.Contains(name, "debian") {
			continue
		}
		chosen = image.ID
		if strings.Contains(name, "13") || strings.Contains(name, "trixie") {
			return image.ID, nil
		}
	}
	if chosen == "" {
		return "", fmt.Errorf("Image Debian introuvable.")
	}
	return chosen, nil
}

func (c Client) infomaniakNetwork(ctx context.Context, networkURL string, header map[string]string) (string, error) {
	status, body, err := c.call(ctx, http.MethodGet, strings.TrimRight(networkURL, "/")+"/v2.0/networks", header, nil)
	if err != nil {
		return "", err
	}
	if status >= 300 {
		return "", refused(status, body)
	}
	var payload struct {
		Networks []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"networks"`
	}
	if json.Unmarshal(body, &payload) != nil || len(payload.Networks) == 0 {
		return "", fmt.Errorf("Réseau introuvable.")
	}
	for _, network := range payload.Networks {
		name := strings.ToLower(network.Name)
		if strings.Contains(name, "ext") || strings.Contains(name, "public") {
			return network.ID, nil
		}
	}
	return payload.Networks[0].ID, nil
}

func infomaniakIP(body []byte) string {
	var payload struct {
		Server struct {
			Addresses map[string][]struct {
				Addr    string `json:"addr"`
				Version int    `json:"version"`
			} `json:"addresses"`
		} `json:"server"`
	}
	if json.Unmarshal(body, &payload) != nil {
		return ""
	}
	var ips []string
	for _, rows := range payload.Server.Addresses {
		for _, row := range rows {
			if row.Version == 0 || row.Version == 4 {
				ips = append(ips, row.Addr)
			}
		}
	}
	return publicIP(ips)
}
