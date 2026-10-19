package provision

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"myinfra/internal/catalog"
)

func ionosOffer(id string) (cores, ramGB int, err error) {
	parts := strings.Split(strings.TrimSpace(id), "-")
	if len(parts) != 3 || parts[0] != "vcpu" {
		return 0, 0, fmt.Errorf("Type de machine inconnu.")
	}
	cores, errCores := strconv.Atoi(parts[1])
	ramGB, errRAM := strconv.Atoi(parts[2])
	if errCores != nil || errRAM != nil || cores <= 0 || ramGB <= 0 {
		return 0, 0, fmt.Errorf("Type de machine inconnu.")
	}
	return cores, ramGB, nil
}

func alnumPass() string {
	var b strings.Builder
	for _, r := range randomPass() {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// createIonos opens a data center per machine: deleting it later removes everything it holds.
func (c Client) createIonos(ctx context.Context, token string, spec Spec) (Machine, error) {
	base := c.base(c.IonosBase, "https://api.ionos.com") + "/cloudapi/v6"
	header := bearer(token)
	cores, ramGB, err := ionosOffer(spec.OfferID)
	if err != nil {
		return Machine{}, err
	}
	disk := spec.RootGB
	if disk < 10 {
		disk = 10
	}
	status, body, headers, err := c.callWithHeaders(ctx, http.MethodPost, base+"/datacenters", header, map[string]any{
		"properties": map[string]any{"name": machineLabel(spec.Name), "location": catalog.IonosLocation(spec.Location)},
	})
	if err != nil {
		return Machine{}, err
	}
	if status >= 300 {
		return Machine{}, refused(status, body)
	}
	var dc struct {
		ID string `json:"id"`
	}
	if json.Unmarshal(body, &dc) != nil || dc.ID == "" {
		return Machine{}, fmt.Errorf("Le provider a refusé la machine.")
	}
	machine := Machine{ID: dc.ID}
	if err := c.ionosWait(ctx, header, headers.Get("Location")); err != nil {
		return machine, err
	}
	status, body, headers, err = c.callWithHeaders(ctx, http.MethodPost, base+"/datacenters/"+dc.ID+"/lans", header, map[string]any{
		"properties": map[string]any{"name": "public", "public": true},
	})
	if err != nil {
		return machine, err
	}
	if status >= 300 {
		return machine, refused(status, body)
	}
	var lan struct {
		ID flexID `json:"id"`
	}
	if json.Unmarshal(body, &lan) != nil || lan.ID == "" {
		return machine, fmt.Errorf("Le provider a refusé la machine.")
	}
	if err := c.ionosWait(ctx, header, headers.Get("Location")); err != nil {
		return machine, err
	}
	lanID, _ := strconv.Atoi(string(lan.ID))
	volume := map[string]any{
		"name": "root", "size": disk, "type": "HDD", "bus": "VIRTIO",
		"imageAlias":    "debian:latest",
		"imagePassword": alnumPass(),
		"userData":      base64.StdEncoding.EncodeToString([]byte(spec.UserData)),
	}
	if len(spec.SSHKeys) > 0 {
		volume["sshKeys"] = spec.SSHKeys
	}
	status, body, headers, err = c.callWithHeaders(ctx, http.MethodPost, base+"/datacenters/"+dc.ID+"/servers", header, map[string]any{
		"properties": map[string]any{
			"name": machineLabel(spec.Name), "type": "VCPU", "cores": cores, "ram": ramGB * 1024, "availabilityZone": "AUTO",
		},
		"entities": map[string]any{
			"volumes": map[string]any{"items": []any{map[string]any{"properties": volume}}},
			"nics": map[string]any{"items": []any{map[string]any{"properties": map[string]any{
				"name": "public", "lan": lanID, "dhcp": true, "firewallActive": false,
			}}}},
		},
	})
	if err != nil {
		return machine, err
	}
	if status >= 300 {
		return machine, refused(status, body)
	}
	var server struct {
		ID string `json:"id"`
	}
	if json.Unmarshal(body, &server) != nil || server.ID == "" {
		return machine, fmt.Errorf("Le provider a refusé la machine.")
	}
	if err := c.ionosWait(ctx, header, headers.Get("Location")); err != nil {
		return machine, err
	}
	machine.IPv4, err = waitIP(ctx, func() (string, error) {
		status, body, err := c.call(ctx, http.MethodGet, base+"/datacenters/"+dc.ID+"/servers/"+server.ID+"?depth=3", header, nil)
		if err != nil {
			return "", err
		}
		if status >= 300 {
			return "", refused(status, body)
		}
		return ionosAddress(body), nil
	})
	if err != nil {
		return machine, err
	}
	return machine, nil
}

func ionosAddress(body []byte) string {
	var payload struct {
		Entities struct {
			Nics struct {
				Items []struct {
					Properties struct {
						IPs []string `json:"ips"`
					} `json:"properties"`
				} `json:"items"`
			} `json:"nics"`
		} `json:"entities"`
	}
	if json.Unmarshal(body, &payload) != nil {
		return ""
	}
	var all []string
	for _, nic := range payload.Entities.Nics.Items {
		all = append(all, nic.Properties.IPs...)
	}
	return publicIP(all)
}

// ionosWait follows the request created by an asynchronous call until it is done.
func (c Client) ionosWait(ctx context.Context, header map[string]string, location string) error {
	location = strings.TrimSpace(location)
	if location == "" {
		return nil
	}
	if !strings.HasSuffix(location, "/status") {
		location = strings.TrimRight(location, "/") + "/status"
	}
	deadline := time.Now().Add(8 * time.Minute)
	for {
		status, body, err := c.call(ctx, http.MethodGet, location, header, nil)
		if err != nil {
			return err
		}
		if status >= 300 {
			return refused(status, body)
		}
		var payload struct {
			Metadata struct {
				Status  string `json:"status"`
				Message string `json:"message"`
			} `json:"metadata"`
		}
		_ = json.Unmarshal(body, &payload)
		switch strings.ToUpper(payload.Metadata.Status) {
		case "DONE":
			return nil
		case "FAILED":
			return refused(http.StatusOK, []byte(payload.Metadata.Message))
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("Le provider ne répond pas.")
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("Le provider ne répond pas.")
		case <-time.After(pollEvery):
		}
	}
}

func (c Client) destroyIonos(ctx context.Context, token, id string) error {
	base := c.base(c.IonosBase, "https://api.ionos.com") + "/cloudapi/v6"
	return c.destroy(ctx, base+"/datacenters/"+id, bearer(token))
}
