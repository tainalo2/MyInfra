package provision

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

func (c Client) createDigitalOcean(ctx context.Context, token string, spec Spec) (Machine, error) {
	base := c.base(c.DigitalOceanBase, "https://api.digitalocean.com")
	header := bearer(token)
	status, body, err := c.call(ctx, http.MethodPost, base+"/v2/droplets", header, map[string]any{
		"name":      spec.Name,
		"region":    spec.Location,
		"size":      spec.OfferID,
		"image":     "debian-13-x64",
		"user_data": spec.UserData,
		"ipv6":      false,
	})
	if err != nil {
		return Machine{}, err
	}
	if status >= 300 {
		return Machine{}, refused(status, body)
	}
	droplet, err := parseDroplet(body)
	if err != nil {
		return Machine{}, err
	}
	if project := strings.TrimSpace(spec.ProjectID); project != "" {
		status, body, err = c.call(ctx, http.MethodPost, base+"/v2/projects/"+project+"/resources", header, map[string]any{
			"resources": []string{"do:droplet:" + droplet.ID},
		})
		if err != nil || status >= 300 {
			_, _, _ = c.call(ctx, http.MethodDelete, base+"/v2/droplets/"+droplet.ID, header, nil)
			if err != nil {
				return Machine{}, err
			}
			return Machine{}, refused(status, body)
		}
	}
	ip, err := waitIP(ctx, func() (string, error) {
		if droplet.IP != "" {
			ip := droplet.IP
			droplet.IP = ""
			return ip, nil
		}
		status, body, err := c.call(ctx, http.MethodGet, base+"/v2/droplets/"+droplet.ID, header, nil)
		if err != nil {
			return "", err
		}
		if status >= 300 {
			return "", refused(status, body)
		}
		next, err := parseDroplet(body)
		if err != nil {
			return "", err
		}
		return next.IP, nil
	})
	if err != nil {
		return Machine{}, err
	}
	return Machine{ID: droplet.ID, IPv4: ip}, nil
}

type droplet struct {
	ID string
	IP string
}

func parseDroplet(body []byte) (droplet, error) {
	var payload struct {
		Droplet struct {
			ID       flexID `json:"id"`
			Networks struct {
				V4 []struct {
					IPAddress string `json:"ip_address"`
					Type      string `json:"type"`
				} `json:"v4"`
			} `json:"networks"`
		} `json:"droplet"`
	}
	if err := json.Unmarshal(body, &payload); err != nil || payload.Droplet.ID == "" {
		return droplet{}, refused(http.StatusBadGateway, body)
	}
	var ips []string
	for _, item := range payload.Droplet.Networks.V4 {
		if item.Type == "" || item.Type == "public" {
			ips = append(ips, item.IPAddress)
		}
	}
	return droplet{ID: string(payload.Droplet.ID), IP: publicIP(ips)}, nil
}

func (c Client) digitalOceanProjects(ctx context.Context, token string) ([]Project, error) {
	base := c.base(c.DigitalOceanBase, "https://api.digitalocean.com")
	status, body, err := c.call(ctx, http.MethodGet, base+"/v2/projects?per_page=200", bearer(token), nil)
	if err != nil {
		return nil, err
	}
	if status >= 300 {
		return nil, refused(status, body)
	}
	var payload struct {
		Projects []struct {
			ID        string `json:"id"`
			Name      string `json:"name"`
			IsDefault bool   `json:"is_default"`
		} `json:"projects"`
	}
	if json.Unmarshal(body, &payload) != nil {
		return nil, fmt.Errorf("Le provider ne répond pas.")
	}
	out := make([]Project, 0, len(payload.Projects))
	var rest []Project
	for _, project := range payload.Projects {
		if project.ID == "" {
			continue
		}
		item := Project{ID: project.ID, Name: project.Name}
		if item.Name == "" {
			item.Name = project.ID
		}
		if project.IsDefault {
			out = append(out, item)
			continue
		}
		rest = append(rest, item)
	}
	return append(out, rest...), nil
}
