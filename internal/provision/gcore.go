package provision

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"myinfra/internal/catalog"
)

const gcoreGroup = "myinfra"

func gcoreHeader(token string) map[string]string {
	return map[string]string{"Authorization": "APIKey " + strings.TrimSpace(token)}
}

func (c Client) gcoreProject(ctx context.Context, base string, header map[string]string) (string, error) {
	status, body, err := c.call(ctx, http.MethodGet, base+"/cloud/v1/projects", header, nil)
	if err != nil {
		return "", err
	}
	if status >= 300 {
		return "", refused(status, body)
	}
	project := catalog.GcoreProject(body)
	if project == "" {
		return "", fmt.Errorf("Aucun projet Gcore.")
	}
	return project, nil
}

func (c Client) createGcore(ctx context.Context, token string, spec Spec) (Machine, error) {
	base := c.base(c.GcoreBase, "https://api.gcore.com")
	header := gcoreHeader(token)
	region := catalog.GcoreRegionID(spec.Location)
	if region == "" {
		return Machine{}, fmt.Errorf("Lieu inconnu.")
	}
	project, err := c.gcoreProject(ctx, base, header)
	if err != nil {
		return Machine{}, err
	}
	scope := project + "/" + region
	status, body, err := c.call(ctx, http.MethodGet, base+"/cloud/v1/images/"+scope+"?visibility=public", header, nil)
	if err != nil {
		return Machine{}, err
	}
	if status >= 300 {
		return Machine{}, refused(status, body)
	}
	image, minDisk, err := gcoreDebian(body)
	if err != nil {
		return Machine{}, err
	}
	disk := spec.RootGB
	if disk < minDisk {
		disk = minDisk
	}
	if disk < 10 {
		disk = 10
	}
	iface := map[string]any{"type": "external", "interface_name": "eth0"}
	if group := c.gcoreGroup(ctx, base, scope, header); group != "" {
		iface["security_groups"] = []map[string]string{{"id": group}}
	}
	status, body, err = c.call(ctx, http.MethodPost, base+"/cloud/v2/instances/"+scope, header, map[string]any{
		"name":       machineLabel(spec.Name),
		"flavor":     spec.OfferID,
		"interfaces": []any{iface},
		"volumes": []any{map[string]any{
			"source": "image", "image_id": image, "size": disk, "boot_index": 0,
			"type_name": "standard", "delete_on_termination": true,
		}},
		"user_data": base64.StdEncoding.EncodeToString([]byte(spec.UserData)),
	})
	if err != nil {
		return Machine{}, err
	}
	if status >= 300 {
		return Machine{}, refused(status, body)
	}
	task, err := gcoreTaskID(body)
	if err != nil {
		return Machine{}, err
	}
	created, err := c.gcoreWait(ctx, base, header, task)
	if err != nil {
		return Machine{}, err
	}
	var instance string
	if len(created.Instances) > 0 {
		instance = created.Instances[0]
	}
	if instance == "" {
		return Machine{}, fmt.Errorf("Le provider a refusé la machine.")
	}
	machine := Machine{ID: region + ":" + project + ":" + instance}
	machine.IPv4, err = waitIP(ctx, func() (string, error) {
		status, body, err := c.call(ctx, http.MethodGet, base+"/cloud/v1/instances/"+scope+"/"+instance, header, nil)
		if err != nil {
			return "", err
		}
		if status >= 300 {
			return "", refused(status, body)
		}
		return gcoreAddress(body), nil
	})
	if err != nil {
		return machine, err
	}
	return machine, nil
}

var gcoreVersion = regexp.MustCompile(`^\d+`)

func gcoreDebian(body []byte) (string, int, error) {
	var payload struct {
		Results []struct {
			ID        string `json:"id"`
			Name      string `json:"name"`
			OsDistro  string `json:"os_distro"`
			OsVersion string `json:"os_version"`
			OsType    string `json:"os_type"`
			MinDisk   int    `json:"min_disk"`
			Status    string `json:"status"`
		} `json:"results"`
	}
	if json.Unmarshal(body, &payload) != nil {
		return "", 0, fmt.Errorf("Debian est introuvable.")
	}
	best, score, minDisk := "", 0, 0
	for _, item := range payload.Results {
		if item.ID == "" || (item.Status != "" && !strings.EqualFold(item.Status, "active")) {
			continue
		}
		if strings.EqualFold(item.OsType, "windows") {
			continue
		}
		name := strings.ToLower(item.Name)
		if !strings.EqualFold(item.OsDistro, "debian") && !strings.Contains(name, "debian") {
			continue
		}
		version, _ := strconv.Atoi(gcoreVersion.FindString(item.OsVersion))
		if version == 0 {
			if match := exoscaleDebian.FindStringSubmatch(item.Name); len(match) > 1 {
				version, _ = strconv.Atoi(match[1])
			}
		}
		if version > score {
			best, score, minDisk = item.ID, version, item.MinDisk
		}
	}
	if best == "" {
		return "", 0, fmt.Errorf("Debian est introuvable.")
	}
	return best, minDisk, nil
}

// gcoreGroup finds or creates a security group that lets traffic in. Failing is harmless: the default group is used.
func (c Client) gcoreGroup(ctx context.Context, base, scope string, header map[string]string) string {
	find := func() string {
		status, body, err := c.call(ctx, http.MethodGet, base+"/cloud/v1/securitygroups/"+scope, header, nil)
		if err != nil || status >= 300 {
			return ""
		}
		var payload struct {
			Results []struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"results"`
		}
		_ = json.Unmarshal(body, &payload)
		for _, item := range payload.Results {
			if item.Name == gcoreGroup {
				return item.ID
			}
		}
		return ""
	}
	if id := find(); id != "" {
		return id
	}
	rule := func(direction, protocol string, min, max int) map[string]any {
		out := map[string]any{"direction": direction, "ethertype": "IPv4", "remote_ip_prefix": "0.0.0.0/0"}
		if protocol != "" {
			out["protocol"] = protocol
		}
		if max > 0 {
			out["port_range_min"] = min
			out["port_range_max"] = max
		}
		return out
	}
	status, body, err := c.call(ctx, http.MethodPost, base+"/cloud/v2/security_groups/"+scope, header, map[string]any{
		"name": gcoreGroup, "description": "MyInfra",
		"rules": []any{
			rule("ingress", "tcp", 1, 65535), rule("ingress", "udp", 1, 65535), rule("ingress", "icmp", 0, 0),
			rule("egress", "", 0, 0),
		},
	})
	if err != nil || status >= 300 {
		return ""
	}
	if task, err := gcoreTaskID(body); err == nil {
		_, _ = c.gcoreWait(ctx, base, header, task)
	}
	return find()
}

func gcoreTaskID(body []byte) (string, error) {
	var payload struct {
		Tasks []string `json:"tasks"`
	}
	if json.Unmarshal(body, &payload) != nil || len(payload.Tasks) == 0 || payload.Tasks[0] == "" {
		return "", fmt.Errorf("Le provider a refusé la machine.")
	}
	return payload.Tasks[0], nil
}

type gcoreCreated struct {
	Instances []string `json:"instances"`
}

func (c Client) gcoreWait(ctx context.Context, base string, header map[string]string, task string) (gcoreCreated, error) {
	deadline := time.Now().Add(6 * time.Minute)
	for {
		status, body, err := c.call(ctx, http.MethodGet, base+"/cloud/v1/tasks/"+task, header, nil)
		if err != nil {
			return gcoreCreated{}, err
		}
		if status >= 300 {
			return gcoreCreated{}, refused(status, body)
		}
		var payload struct {
			State            string       `json:"state"`
			Error            string       `json:"error"`
			CreatedResources gcoreCreated `json:"created_resources"`
		}
		if json.Unmarshal(body, &payload) != nil {
			return gcoreCreated{}, fmt.Errorf("Le provider ne répond pas.")
		}
		switch strings.ToUpper(payload.State) {
		case "FINISHED":
			return payload.CreatedResources, nil
		case "ERROR":
			return gcoreCreated{}, refused(http.StatusOK, []byte(payload.Error))
		}
		if time.Now().After(deadline) {
			return gcoreCreated{}, fmt.Errorf("Le provider ne répond pas.")
		}
		select {
		case <-ctx.Done():
			return gcoreCreated{}, fmt.Errorf("Le provider ne répond pas.")
		case <-time.After(pollEvery):
		}
	}
}

// gcoreAddress returns the first public IPv4 found under an "addr" or "ip_address" key.
func gcoreAddress(body []byte) string {
	var doc any
	if json.Unmarshal(body, &doc) != nil {
		return ""
	}
	var found []string
	var walk func(value any)
	walk = func(value any) {
		switch item := value.(type) {
		case map[string]any:
			for key, inner := range item {
				if text, ok := inner.(string); ok && (key == "addr" || key == "ip_address") {
					found = append(found, text)
					continue
				}
				walk(inner)
			}
		case []any:
			for _, inner := range item {
				walk(inner)
			}
		}
	}
	walk(doc)
	return publicIP(found)
}

func (c Client) destroyGcore(ctx context.Context, token, id string) error {
	parts := strings.Split(id, ":")
	if len(parts) != 3 {
		return nil
	}
	base := c.base(c.GcoreBase, "https://api.gcore.com")
	url := base + "/cloud/v1/instances/" + parts[1] + "/" + parts[0] + "/" + parts[2] + "?delete_floatings=true"
	return c.destroy(ctx, url, gcoreHeader(token))
}
