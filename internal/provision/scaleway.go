package provision

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode"
)

func (c Client) createScaleway(ctx context.Context, token, projectID string, spec Spec) (Machine, error) {
	base := c.base(c.ScalewayBase, "https://api.scaleway.com")
	zone := spec.Location
	header := scalewayAuth(token)
	if err := c.ensureScalewaySSHKeys(ctx, base, projectID, header, spec.SSHKeys); err != nil {
		return Machine{}, err
	}
	image, err := c.scalewayImage(ctx, base, zone, header, spec)
	if err != nil {
		return Machine{}, err
	}
	payload := map[string]any{
		"name":                scalewayName(spec.Name),
		"commercial_type":     spec.OfferID,
		"image":               image,
		"project":             projectID,
		"dynamic_ip_required": true,
		"enable_ipv6":         false,
	}
	if spec.RootGB > 0 {
		payload["volumes"] = map[string]any{
			"0": map[string]any{"size": spec.RootGB * 1000000000, "volume_type": "sbs_volume"},
		}
	}
	status, body, err := c.call(ctx, http.MethodPost, base+"/instance/v1/zones/"+zone+"/servers", header, payload)
	if err != nil {
		return Machine{}, err
	}
	if status >= 300 {
		return Machine{}, refused(status, body)
	}
	server, err := parseScalewayServer(body)
	if err != nil {
		return Machine{}, err
	}
	abandon := func() {
		clean, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = c.destroyScaleway(clean, base, zone, server.ID, header)
	}
	status, body, err = c.patchPlain(ctx, base+"/instance/v1/zones/"+zone+"/servers/"+server.ID+"/user_data/cloud-init", header, spec.UserData)
	if err != nil || status >= 300 {
		abandon()
		if err != nil {
			return Machine{}, err
		}
		return Machine{}, refused(status, body)
	}
	status, body, err = c.call(ctx, http.MethodPost, base+"/instance/v1/zones/"+zone+"/servers/"+server.ID+"/action", header, map[string]string{"action": "reboot"})
	if err != nil {
		abandon()
		return Machine{}, err
	}
	if status >= 300 {
		status, body, err = c.call(ctx, http.MethodPost, base+"/instance/v1/zones/"+zone+"/servers/"+server.ID+"/action", header, map[string]string{"action": "poweron"})
		if err != nil || status >= 300 {
			abandon()
			if err != nil {
				return Machine{}, err
			}
			return Machine{}, refused(status, body)
		}
	}
	ip, err := waitIP(ctx, func() (string, error) {
		if server.IP != "" {
			ip := server.IP
			server.IP = ""
			return ip, nil
		}
		status, body, err := c.call(ctx, http.MethodGet, base+"/instance/v1/zones/"+zone+"/servers/"+server.ID, header, nil)
		if err != nil {
			return "", err
		}
		if status >= 300 {
			return "", refused(status, body)
		}
		next, err := parseScalewayServer(body)
		if err != nil {
			return "", err
		}
		return next.IP, nil
	})
	if err != nil {
		abandon()
		return Machine{}, err
	}
	return Machine{ID: zone + "/" + server.ID, IPv4: ip}, nil
}

func scalewayName(name string) string {
	var b strings.Builder
	prevDash := false
	for _, r := range strings.ToLower(name) {
		ok := unicode.IsLetter(r) || unicode.IsDigit(r)
		if ok {
			b.WriteRune(r)
			prevDash = false
			continue
		}
		if !prevDash && b.Len() > 0 {
			b.WriteByte('-')
			prevDash = true
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		out = "myinfra"
	}
	if len(out) > 50 {
		out = strings.Trim(out[:50], "-")
	}
	return out
}

func (c Client) ensureScalewaySSHKeys(ctx context.Context, base, project string, header map[string]string, keys []string) error {
	var wanted []string
	for _, key := range keys {
		key = strings.TrimSpace(key)
		if key != "" {
			wanted = append(wanted, key)
		}
	}
	if len(wanted) == 0 || strings.TrimSpace(project) == "" {
		return nil
	}
	status, body, err := c.call(ctx, http.MethodGet, base+"/iam/v1alpha1/ssh-keys?page_size=100&project_id="+project, header, nil)
	if err != nil {
		return err
	}
	if status >= 300 {
		return refused(status, body)
	}
	var listed struct {
		SSHKeys []struct {
			PublicKey string `json:"public_key"`
		} `json:"ssh_keys"`
	}
	_ = json.Unmarshal(body, &listed)
	have := map[string]bool{}
	for _, item := range listed.SSHKeys {
		have[sshBlob(item.PublicKey)] = true
	}
	for _, key := range wanted {
		if have[sshBlob(key)] {
			continue
		}
		name := "myinfra-" + sshBlob(key)
		if len(name) > 60 {
			name = name[:60]
		}
		status, body, err = c.call(ctx, http.MethodPost, base+"/iam/v1alpha1/ssh-keys", header, map[string]string{
			"name": name, "project_id": project, "public_key": key,
		})
		if err != nil {
			return err
		}
		if status >= 300 {
			return refused(status, body)
		}
	}
	return nil
}

func sshBlob(key string) string {
	parts := strings.Fields(key)
	if len(parts) >= 2 {
		return parts[1]
	}
	return key
}

func (c Client) scalewayImage(ctx context.Context, base, zone string, header map[string]string, spec Spec) (string, error) {
	if spec.GPUs > 0 {
		if id := c.scalewayGPUImage(ctx, base, zone, header, spec.OfferID); id != "" {
			return id, nil
		}
	}
	status, body, err := c.call(ctx, http.MethodGet, base+"/instance/v1/zones/"+zone+"/images?per_page=100", header, nil)
	if err != nil {
		return "", err
	}
	if status >= 300 {
		return "", refused(status, body)
	}
	var payload struct {
		Images []struct {
			ID    string `json:"id"`
			Name  string `json:"name"`
			Arch  string `json:"arch"`
			State string `json:"state"`
		} `json:"images"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", refused(status, body)
	}
	var chosen string
	for _, image := range payload.Images {
		if image.ID == "" || (image.State != "" && image.State != "available") {
			continue
		}
		name := strings.ToLower(image.Name)
		if !strings.Contains(name, "debian") {
			continue
		}
		if image.Arch != "" && image.Arch != "x86_64" && image.Arch != "amd64" {
			continue
		}
		chosen = image.ID
		if strings.Contains(name, "13") || strings.Contains(name, "trixie") {
			return image.ID, nil
		}
	}
	if chosen == "" {
		return "", refused(http.StatusBadGateway, body)
	}
	return chosen, nil
}

// scalewayGPUImage picks a Debian image the GPU commercial type accepts.
// Empty means the usual Debian image is used instead.
func (c Client) scalewayGPUImage(ctx context.Context, base, zone string, header map[string]string, commercial string) string {
	if commercial == "" {
		return ""
	}
	status, body, err := c.call(ctx, http.MethodGet, base+"/marketplace/v2/local-images?zone="+zone, header, nil)
	if err != nil || status >= 300 {
		return ""
	}
	return scalewayCompatible(body, commercial)
}

func scalewayCompatible(body []byte, commercial string) string {
	var payload struct {
		Images []struct {
			ID         string   `json:"id"`
			Label      string   `json:"label"`
			Compatible []string `json:"compatible_commercial_types"`
		} `json:"local_images"`
	}
	if json.Unmarshal(body, &payload) != nil {
		return ""
	}
	chosen := ""
	for _, image := range payload.Images {
		if image.ID == "" || !strings.Contains(strings.ToLower(image.Label), "debian") {
			continue
		}
		fits := false
		for _, kind := range image.Compatible {
			if kind == commercial {
				fits = true
				break
			}
		}
		if !fits {
			continue
		}
		chosen = image.ID
		if strings.Contains(image.Label, "13") || strings.Contains(image.Label, "trixie") {
			return image.ID
		}
	}
	return chosen
}

func parseScalewayServer(body []byte) (droplet, error) {
	var payload struct {
		Server struct {
			ID       string `json:"id"`
			PublicIP struct {
				Address string `json:"address"`
			} `json:"public_ip"`
		} `json:"server"`
	}
	if err := json.Unmarshal(body, &payload); err != nil || payload.Server.ID == "" {
		return droplet{}, refused(http.StatusBadGateway, body)
	}
	return droplet{ID: payload.Server.ID, IP: publicIP([]string{payload.Server.PublicIP.Address})}, nil
}

func (c Client) destroyScaleway(ctx context.Context, base, zone, id string, header map[string]string) error {
	serverURL := base + "/instance/v1/zones/" + zone + "/servers/" + id
	_, _, _ = c.call(ctx, http.MethodPost, serverURL+"/action", header, map[string]string{"action": "poweroff"})
	deadline := time.Now().Add(45 * time.Second)
	var volumes []string
	var securityGroup string
	gone := false
	for time.Now().Before(deadline) {
		status, body, err := c.call(ctx, http.MethodGet, serverURL, header, nil)
		if err != nil {
			return err
		}
		if status == http.StatusNotFound {
			gone = true
			break
		}
		if found := scalewaySBSVolumes(body); len(found) > 0 {
			volumes = found
		}
		if found := scalewaySecurityGroup(body); found != "" {
			securityGroup = found
		}
		var payload struct {
			Server struct {
				State string `json:"state"`
			} `json:"server"`
		}
		_ = json.Unmarshal(body, &payload)
		if payload.Server.State == "stopped" || payload.Server.State == "stopped in place" {
			break
		}
		select {
		case <-ctx.Done():
			break
		case <-time.After(3 * time.Second):
		}
	}
	if !gone {
		if err := c.destroy(ctx, serverURL+"?delete_ip=true&delete_volumes=true", header); err != nil {
			return err
		}
	}
	if err := c.deleteBlockVolumes(ctx, base, zone, volumes, header); err != nil {
		return err
	}
	return c.deleteSecurityGroup(ctx, base, zone, securityGroup, id, header)
}

func scalewaySBSVolumes(body []byte) []string {
	var payload struct {
		Server struct {
			Volumes map[string]struct {
				ID         string `json:"id"`
				VolumeType string `json:"volume_type"`
			} `json:"volumes"`
		} `json:"server"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil
	}
	ids := make([]string, 0, len(payload.Server.Volumes))
	for _, volume := range payload.Server.Volumes {
		if volume.ID == "" || (volume.VolumeType != "sbs_volume" && volume.VolumeType != "sbs") {
			continue
		}
		ids = append(ids, volume.ID)
	}
	return ids
}

func scalewaySecurityGroup(body []byte) string {
	var payload struct {
		Server struct {
			SecurityGroup struct {
				ID string `json:"id"`
			} `json:"security_group"`
		} `json:"server"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return ""
	}
	return payload.Server.SecurityGroup.ID
}

func (c Client) deleteBlockVolumes(ctx context.Context, base, zone string, ids []string, header map[string]string) error {
	var last error
	for _, id := range ids {
		url := base + "/block/v1/zones/" + zone + "/volumes/" + id
		deadline := time.Now().Add(30 * time.Second)
		for {
			status, body, err := c.call(ctx, http.MethodDelete, url, header, nil)
			if err != nil {
				return err
			}
			if status == http.StatusNotFound || status == http.StatusNoContent || status < 300 {
				last = nil
				break
			}
			last = refused(status, body)
			if time.Now().After(deadline) {
				break
			}
			select {
			case <-ctx.Done():
				return last
			case <-time.After(2 * time.Second):
			}
		}
	}
	return last
}

func (c Client) deleteSecurityGroup(ctx context.Context, base, zone, id, deletedServerID string, header map[string]string) error {
	if id == "" {
		return nil
	}
	url := base + "/instance/v1/zones/" + zone + "/security_groups/" + id
	deadline := time.Now().Add(30 * time.Second)
	for {
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
		var payload struct {
			SecurityGroup struct {
				Servers []struct {
					ID string `json:"id"`
				} `json:"servers"`
			} `json:"security_group"`
		}
		if err := json.Unmarshal(body, &payload); err != nil {
			return refused(status, body)
		}
		hasDeleted := false
		for _, server := range payload.SecurityGroup.Servers {
			if server.ID != deletedServerID {
				return nil
			}
			hasDeleted = true
		}
		if hasDeleted {
			if time.Now().After(deadline) {
				return nil
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(2 * time.Second):
				continue
			}
		}

		status, body, err = c.call(ctx, http.MethodDelete, url, header, nil)
		if err != nil {
			return err
		}
		if status == http.StatusNotFound || status == http.StatusNoContent || status < 300 {
			return nil
		}
		if time.Now().After(deadline) {
			return refused(status, body)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}

func splitScaleway(id string) (string, string) {
	zone, serverID, ok := strings.Cut(id, "/")
	if !ok {
		return "fr-par-1", id
	}
	return zone, serverID
}

func (c Client) patchPlain(ctx context.Context, url string, header map[string]string, payload string) (int, []byte, error) {
	return c.sendPlain(ctx, http.MethodPatch, url, header, payload)
}

func (c Client) sendPlain(ctx context.Context, method, url string, header map[string]string, payload string) (int, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, strings.NewReader(payload))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Content-Type", "text/plain")
	for key, value := range header {
		req.Header.Set(key, value)
	}
	res, err := c.http().Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return res.StatusCode, nil, err
	}
	return res.StatusCode, body, nil
}
