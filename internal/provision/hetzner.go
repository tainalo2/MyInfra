package provision

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

func (c Client) createHetzner(ctx context.Context, token string, spec Spec) (Machine, error) {
	base := c.base(c.HetznerBase, "https://api.hetzner.cloud")
	header := bearer(token)
	status, body, err := c.call(ctx, http.MethodPost, base+"/v1/servers", header, map[string]any{
		"name":        spec.Name,
		"server_type": spec.OfferID,
		"image":       "debian-13",
		"location":    spec.Location,
		"user_data":   spec.UserData,
		"public_net":  map[string]bool{"enable_ipv4": true, "enable_ipv6": false},
	})
	if err != nil {
		return Machine{}, err
	}
	if status >= 300 {
		if bytes.Contains(bytes.ToLower(body), []byte("unsupported location")) {
			return Machine{}, fmt.Errorf("Ce type de machine n'est plus proposé à cet endroit.")
		}
		return Machine{}, refused(status, body)
	}
	server, err := parseHetznerServer(body)
	if err != nil {
		return Machine{}, err
	}
	ip, err := waitIP(ctx, func() (string, error) {
		if server.IP != "" {
			ip := server.IP
			server.IP = ""
			return ip, nil
		}
		status, body, err := c.call(ctx, http.MethodGet, base+"/v1/servers/"+server.ID, header, nil)
		if err != nil {
			return "", err
		}
		if status >= 300 {
			return "", refused(status, body)
		}
		next, err := parseHetznerServer(body)
		if err != nil {
			return "", err
		}
		return next.IP, nil
	})
	if err != nil {
		return Machine{}, err
	}
	return Machine{ID: server.ID, IPv4: ip}, nil
}

type hetznerServer struct {
	ID string
	IP string
}

func parseHetznerServer(body []byte) (hetznerServer, error) {
	var payload struct {
		Server struct {
			ID        flexID `json:"id"`
			PublicNet struct {
				IPv4 struct {
					IP string `json:"ip"`
				} `json:"ipv4"`
			} `json:"public_net"`
		} `json:"server"`
	}
	if err := json.Unmarshal(body, &payload); err != nil || payload.Server.ID == "" {
		return hetznerServer{}, refused(http.StatusBadGateway, body)
	}
	return hetznerServer{ID: string(payload.Server.ID), IP: payload.Server.PublicNet.IPv4.IP}, nil
}
