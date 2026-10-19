package provision

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

func (c Client) createVultr(ctx context.Context, token string, spec Spec) (Machine, error) {
	base := c.base(c.VultrBase, "https://api.vultr.com")
	header := bearer(token)
	status, body, err := c.call(ctx, http.MethodGet, base+"/v2/os?per_page=500", header, nil)
	if err != nil {
		return Machine{}, err
	}
	if status >= 300 {
		return Machine{}, refused(status, body)
	}
	osID, err := vultrDebian(body)
	if err != nil {
		return Machine{}, err
	}
	status, body, err = c.call(ctx, http.MethodPost, base+"/v2/instances", header, map[string]any{
		"region":      spec.Location,
		"plan":        spec.OfferID,
		"os_id":       osID,
		"label":       machineLabel(spec.Name),
		"hostname":    machineHost(spec.Name),
		"user_data":   base64.StdEncoding.EncodeToString([]byte(spec.UserData)),
		"user_scheme": "root",
	})
	if err != nil {
		return Machine{}, err
	}
	if status >= 300 {
		return Machine{}, refused(status, body)
	}
	id, ip := vultrInstance(body)
	if id == "" {
		return Machine{}, fmt.Errorf("Le provider a refusé la machine.")
	}
	if !usableIP(ip) {
		ip, err = waitIP(ctx, func() (string, error) {
			status, body, err := c.call(ctx, http.MethodGet, base+"/v2/instances/"+id, header, nil)
			if err != nil {
				return "", err
			}
			if status >= 300 {
				return "", refused(status, body)
			}
			_, next := vultrInstance(body)
			if !usableIP(next) {
				return "", nil
			}
			return next, nil
		})
		if err != nil {
			return Machine{}, err
		}
	}
	return Machine{ID: id, IPv4: ip}, nil
}

func usableIP(ip string) bool {
	ip = strings.TrimSpace(ip)
	return ip != "" && ip != "0.0.0.0"
}

func vultrDebian(body []byte) (int, error) {
	var payload struct {
		OS []struct {
			ID     int    `json:"id"`
			Name   string `json:"name"`
			Arch   string `json:"arch"`
			Family string `json:"family"`
		} `json:"os"`
	}
	if json.Unmarshal(body, &payload) != nil {
		return 0, fmt.Errorf("Debian est introuvable.")
	}
	best := 0
	score := 0
	for _, item := range payload.OS {
		if item.ID == 0 || !strings.EqualFold(item.Family, "debian") || !strings.EqualFold(item.Arch, "x64") {
			continue
		}
		name := strings.ToLower(item.Name)
		rank := 1
		if strings.Contains(name, "13") || strings.Contains(name, "trixie") {
			rank = 3
		} else if strings.Contains(name, "12") || strings.Contains(name, "bookworm") {
			rank = 2
		}
		if rank > score {
			score = rank
			best = item.ID
		}
	}
	if best == 0 {
		return 0, fmt.Errorf("Debian est introuvable.")
	}
	return best, nil
}

func vultrInstance(body []byte) (string, string) {
	var payload struct {
		Instance struct {
			ID     flexID `json:"id"`
			MainIP string `json:"main_ip"`
		} `json:"instance"`
	}
	if json.Unmarshal(body, &payload) != nil {
		return "", ""
	}
	return string(payload.Instance.ID), payload.Instance.MainIP
}
