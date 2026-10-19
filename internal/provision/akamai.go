package provision

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

func (c Client) createAkamai(ctx context.Context, token string, spec Spec) (Machine, error) {
	base := c.base(c.AkamaiBase, "https://api.linode.com")
	header := bearer(token)
	image := c.linodeImage(ctx, base, header)
	body := map[string]any{
		"type":      spec.OfferID,
		"region":    spec.Location,
		"image":     image,
		"label":     machineLabel(spec.Name),
		"root_pass": randomPass(),
		"booted":    true,
		"metadata": map[string]any{
			"user_data": cloudConfig(spec.UserData),
		},
	}
	if len(spec.SSHKeys) > 0 {
		body["authorized_keys"] = spec.SSHKeys
	}
	status, raw, err := c.call(ctx, http.MethodPost, base+"/v4/linode/instances", header, body)
	if err != nil {
		return Machine{}, err
	}
	if status >= 300 {
		return Machine{}, refused(status, raw)
	}
	id, ip := linodeInstance(raw)
	if id == "" {
		return Machine{}, fmt.Errorf("Le provider a refusé la machine.")
	}
	if ip == "" {
		ip, err = waitIP(ctx, func() (string, error) {
			status, raw, err := c.call(ctx, http.MethodGet, base+"/v4/linode/instances/"+id, header, nil)
			if err != nil {
				return "", err
			}
			if status >= 300 {
				return "", refused(status, raw)
			}
			_, next := linodeInstance(raw)
			return next, nil
		})
		if err != nil {
			return Machine{}, err
		}
	}
	return Machine{ID: id, IPv4: ip}, nil
}

func (c Client) linodeImage(ctx context.Context, base string, header map[string]string) string {
	for _, id := range []string{"linode/debian13", "linode/debian12"} {
		status, _, err := c.call(ctx, http.MethodGet, base+"/v4/images/"+id, header, nil)
		if err == nil && status < 300 {
			return id
		}
	}
	return "linode/debian13"
}

func cloudConfig(script string) string {
	var b strings.Builder
	b.WriteString("#cloud-config\nwrite_files:\n  - path: /root/myinfra-join.sh\n    permissions: \"0755\"\n    encoding: b64\n    content: ")
	b.WriteString(base64.StdEncoding.EncodeToString([]byte(script)))
	b.WriteString("\nruncmd:\n  - [/root/myinfra-join.sh]\n")
	return base64.StdEncoding.EncodeToString([]byte(b.String()))
}

func linodeInstance(body []byte) (string, string) {
	var payload struct {
		ID   flexID   `json:"id"`
		IPv4 []string `json:"ipv4"`
	}
	if json.Unmarshal(body, &payload) != nil {
		return "", ""
	}
	return string(payload.ID), publicIP(payload.IPv4)
}
