package provision

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// upcloudTarget returns the disk size and tier the plan includes, and the
// Debian template to clone.
func (c Client) upcloudTarget(ctx context.Context, token string, spec Spec) (int, string, string, error) {
	base := c.base(c.UpCloudBase, "https://api.upcloud.com")
	header := bearer(token)
	status, plans, err := c.call(ctx, http.MethodGet, base+"/1.3/plan", header, nil)
	if err != nil {
		return 0, "", "", err
	}
	if status >= 300 {
		return 0, "", "", refused(status, plans)
	}
	status, templates, err := c.call(ctx, http.MethodGet, base+"/1.3/storage/template", header, nil)
	if err != nil {
		return 0, "", "", err
	}
	if status >= 300 {
		return 0, "", "", refused(status, templates)
	}
	size, tier := upcloudPlan(plans, spec.OfferID)
	if spec.RootGB > size {
		size = spec.RootGB
	}
	if size < 10 {
		size = 10
	}
	if tier == "" {
		tier = "maxiops"
	}
	template, err := upcloudDebian(templates)
	if err != nil {
		return 0, "", "", err
	}
	return size, tier, template, nil
}

func (c Client) createUpCloud(ctx context.Context, token string, spec Spec) (Machine, error) {
	base := c.base(c.UpCloudBase, "https://api.upcloud.com")
	header := bearer(token)
	size, tier, template, err := c.upcloudTarget(ctx, token, spec)
	if err != nil {
		return Machine{}, err
	}
	server := map[string]any{
		"zone":      spec.Location,
		"title":     spec.Name,
		"hostname":  machineHost(spec.Name),
		"plan":      spec.OfferID,
		"metadata":  "yes",
		"firewall":  "off",
		"user_data": spec.UserData,
		"storage_devices": map[string]any{
			"storage_device": []map[string]any{{
				"action":  "clone",
				"storage": template,
				"title":   "debian",
				"size":    size,
				"tier":    tier,
			}},
		},
		"networking": map[string]any{
			"interfaces": map[string]any{
				"interface": []map[string]any{{
					"type": "public",
					"ip_addresses": map[string]any{
						"ip_address": []map[string]string{{"family": "IPv4"}},
					},
				}},
			},
		},
	}
	if len(spec.SSHKeys) > 0 {
		server["login_user"] = map[string]any{
			"username": "root",
			"ssh_keys": map[string]any{"ssh_key": spec.SSHKeys},
		}
	}
	status, body, err := c.call(ctx, http.MethodPost, base+"/1.3/server", header, map[string]any{"server": server})
	if err != nil {
		return Machine{}, err
	}
	if status >= 300 {
		return Machine{}, refused(status, body)
	}
	id, ip := upcloudServer(body)
	if id == "" {
		return Machine{}, fmt.Errorf("Le provider a refusé la machine.")
	}
	if ip == "" {
		ip, err = waitIP(ctx, func() (string, error) {
			status, body, err := c.call(ctx, http.MethodGet, base+"/1.3/server/"+id, header, nil)
			if err != nil {
				return "", err
			}
			if status >= 300 {
				return "", refused(status, body)
			}
			_, next := upcloudServer(body)
			return next, nil
		})
		if err != nil {
			return Machine{}, err
		}
	}
	return Machine{ID: id, IPv4: ip}, nil
}

// destroyUpCloud arrête la machine avant de la supprimer : UpCloud refuse
// de supprimer un serveur qui tourne (SERVER_STATE_ILLEGAL).
func (c Client) destroyUpCloud(ctx context.Context, token, id string) error {
	base := c.base(c.UpCloudBase, "https://api.upcloud.com")
	header := bearer(token)
	url := base + "/1.3/server/" + id
	status, body, err := c.call(ctx, http.MethodGet, url, header, nil)
	if err != nil {
		return err
	}
	if status == http.StatusNotFound {
		return nil
	}
	if status >= 300 {
		return refused(status, body)
	}
	state := upcloudState(body)
	if state != "stopped" {
		if state != "maintenance" {
			status, body, err = c.call(ctx, http.MethodPost, url+"/stop", header, map[string]any{
				"stop_server": map[string]any{"stop_type": "hard", "timeout": "60"},
			})
			if err != nil {
				return err
			}
			if status == http.StatusNotFound {
				return nil
			}
			if status >= 300 && status != http.StatusConflict {
				return refused(status, body)
			}
		}
		deadline := time.Now().Add(3 * time.Minute)
		for {
			status, body, err = c.call(ctx, http.MethodGet, url, header, nil)
			if err != nil {
				return err
			}
			if status == http.StatusNotFound {
				return nil
			}
			if status < 300 && upcloudState(body) == "stopped" {
				break
			}
			if time.Now().After(deadline) {
				return fmt.Errorf("La machine ne s'arrête pas chez le provider.")
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(pollEvery):
			}
		}
	}
	return c.destroy(ctx, url+"?storages=1", header)
}

func upcloudState(body []byte) string {
	var payload struct {
		Server struct {
			State string `json:"state"`
		} `json:"server"`
	}
	_ = json.Unmarshal(body, &payload)
	return payload.Server.State
}

func upcloudPlan(body []byte, name string) (int, string) {
	var payload struct {
		Plans struct {
			Plan []struct {
				Name    string `json:"name"`
				Storage int    `json:"storage_size"`
				Tier    string `json:"storage_tier"`
			} `json:"plan"`
		} `json:"plans"`
	}
	if json.Unmarshal(body, &payload) != nil {
		return 0, ""
	}
	for _, plan := range payload.Plans.Plan {
		if plan.Name == name {
			return plan.Storage, plan.Tier
		}
	}
	return 0, ""
}

func upcloudDebian(body []byte) (string, error) {
	var payload struct {
		Storages struct {
			Storage []struct {
				UUID  string `json:"uuid"`
				Title string `json:"title"`
			} `json:"storage"`
		} `json:"storages"`
	}
	if json.Unmarshal(body, &payload) != nil {
		return "", fmt.Errorf("Debian est introuvable.")
	}
	best := ""
	score := 0
	for _, item := range payload.Storages.Storage {
		title := strings.ToLower(item.Title)
		if item.UUID == "" || !strings.Contains(title, "debian") {
			continue
		}
		if strings.Contains(title, "docker") || strings.Contains(title, "plesk") || strings.Contains(title, "windows") {
			continue
		}
		rank := 1
		if strings.Contains(title, "13") || strings.Contains(title, "trixie") {
			rank = 3
		} else if strings.Contains(title, "12") || strings.Contains(title, "bookworm") {
			rank = 2
		}
		if rank > score {
			score = rank
			best = item.UUID
		}
	}
	if best == "" {
		return "", fmt.Errorf("Debian est introuvable.")
	}
	return best, nil
}

func upcloudServer(body []byte) (string, string) {
	var payload struct {
		Server struct {
			UUID        string `json:"uuid"`
			IPAddresses struct {
				Address []struct {
					Access  string `json:"access"`
					Family  string `json:"family"`
					Address string `json:"address"`
				} `json:"ip_address"`
			} `json:"ip_addresses"`
		} `json:"server"`
	}
	if json.Unmarshal(body, &payload) != nil {
		return "", ""
	}
	for _, item := range payload.Server.IPAddresses.Address {
		if strings.EqualFold(item.Access, "public") && strings.EqualFold(item.Family, "IPv4") && item.Address != "" {
			return payload.Server.UUID, item.Address
		}
	}
	return payload.Server.UUID, ""
}
