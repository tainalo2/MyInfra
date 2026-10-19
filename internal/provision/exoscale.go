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
	"myinfra/internal/exo"
)

var pollEvery = 3 * time.Second

const exoscaleGroup = "myinfra"

// exoscaleCall signs and sends one request to the API of a zone.
func (c Client) exoscaleCall(ctx context.Context, creds catalog.Creds, zone, method, path string, payload any) (int, []byte, error) {
	url := exo.Host(c.ExoscaleBase, zone) + path
	var raw []byte
	if payload != nil {
		var err error
		raw, err = json.Marshal(payload)
		if err != nil {
			return 0, nil, err
		}
	}
	header, err := exo.Header(method, url, raw, creds.ApplicationKey, creds.ApplicationSecret)
	if err != nil {
		return 0, nil, err
	}
	return c.call(ctx, method, url, header, payload)
}

func (c Client) createExoscale(ctx context.Context, creds catalog.Creds, spec Spec) (Machine, error) {
	zone := spec.Location
	status, body, err := c.exoscaleCall(ctx, creds, zone, http.MethodGet, "/v2/template?family=debian&visibility=public", nil)
	if err != nil {
		return Machine{}, err
	}
	if status >= 300 {
		return Machine{}, refused(status, body)
	}
	template, err := exoscaleTemplate(body)
	if err != nil {
		return Machine{}, err
	}
	disk := spec.RootGB
	if disk < 10 {
		disk = 10
	}
	request := map[string]any{
		"name":                 machineLabel(spec.Name),
		"instance-type":        map[string]string{"id": spec.OfferID},
		"template":             map[string]string{"id": template},
		"disk-size":            disk,
		"public-ip-assignment": "inet4",
		"user-data":            base64.StdEncoding.EncodeToString([]byte(spec.UserData)),
	}
	if group, err := c.exoscaleGroup(ctx, creds, zone); err == nil && group != "" {
		request["security-groups"] = []map[string]string{{"id": group}}
	}
	status, body, err = c.exoscaleCall(ctx, creds, zone, http.MethodPost, "/v2/instance", request)
	if err != nil {
		return Machine{}, err
	}
	if status >= 300 {
		return Machine{}, refused(status, body)
	}
	id, err := c.exoscaleOperation(ctx, creds, zone, body)
	if err != nil {
		return Machine{}, err
	}
	machine := Machine{ID: zone + ":" + id}
	machine.IPv4, err = waitIP(ctx, func() (string, error) {
		status, body, err := c.exoscaleCall(ctx, creds, zone, http.MethodGet, "/v2/instance/"+id, nil)
		if err != nil {
			return "", err
		}
		if status >= 300 {
			return "", refused(status, body)
		}
		var payload struct {
			IP string `json:"public-ip"`
		}
		_ = json.Unmarshal(body, &payload)
		return strings.TrimSpace(payload.IP), nil
	})
	if err != nil {
		return machine, err
	}
	return machine, nil
}

// exoscaleTarget returns the instance type as FAMILY.SIZE, the Debian
// template and the security group that lets traffic in: the machine filters
// it itself with nftables.
func (c Client) exoscaleTarget(ctx context.Context, creds catalog.Creds, spec Spec) (string, string, string, error) {
	zone := spec.Location
	kind := spec.OfferID
	if !strings.Contains(kind, ".") {
		status, body, err := c.exoscaleCall(ctx, creds, zone, http.MethodGet, "/v2/instance-type/"+kind, nil)
		if err != nil {
			return "", "", "", err
		}
		if status >= 300 {
			return "", "", "", refused(status, body)
		}
		var payload struct {
			Family string `json:"family"`
			Size   string `json:"size"`
		}
		if json.Unmarshal(body, &payload) != nil || payload.Family == "" || payload.Size == "" {
			return "", "", "", fmt.Errorf("Ce type de machine n'est plus proposé à cet endroit.")
		}
		kind = payload.Family + "." + payload.Size
	}
	status, body, err := c.exoscaleCall(ctx, creds, zone, http.MethodGet, "/v2/template?family=debian&visibility=public", nil)
	if err != nil {
		return "", "", "", err
	}
	if status >= 300 {
		return "", "", "", refused(status, body)
	}
	template, err := exoscaleTemplate(body)
	if err != nil {
		return "", "", "", err
	}
	group, err := c.exoscaleGroup(ctx, creds, zone)
	if err != nil {
		return "", "", "", err
	}
	return kind, template, group, nil
}

var exoscaleDebian = regexp.MustCompile(`(?i)debian\s+(\d+)`)

func exoscaleTemplate(body []byte) (string, error) {
	var payload struct {
		Templates []struct {
			ID      string `json:"id"`
			Name    string `json:"name"`
			Version string `json:"version"`
		} `json:"templates"`
	}
	if json.Unmarshal(body, &payload) != nil {
		return "", fmt.Errorf("Debian est introuvable.")
	}
	best, score := "", 0
	for _, item := range payload.Templates {
		if item.ID == "" {
			continue
		}
		match := exoscaleDebian.FindStringSubmatch(item.Name)
		if len(match) < 2 {
			continue
		}
		version, _ := strconv.Atoi(match[1])
		if version > score {
			best, score = item.ID, version
		}
	}
	if best == "" {
		return "", fmt.Errorf("Debian est introuvable.")
	}
	return best, nil
}

// exoscaleGroup returns a security group that lets traffic in, creating it once.
func (c Client) exoscaleGroup(ctx context.Context, creds catalog.Creds, zone string) (string, error) {
	find := func() (string, error) {
		status, body, err := c.exoscaleCall(ctx, creds, zone, http.MethodGet, "/v2/security-group", nil)
		if err != nil {
			return "", err
		}
		if status >= 300 {
			return "", refused(status, body)
		}
		var payload struct {
			Groups []struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"security-groups"`
		}
		_ = json.Unmarshal(body, &payload)
		for _, group := range payload.Groups {
			if group.Name == exoscaleGroup {
				return group.ID, nil
			}
		}
		return "", nil
	}
	if id, err := find(); err != nil || id != "" {
		return id, err
	}
	status, body, err := c.exoscaleCall(ctx, creds, zone, http.MethodPost, "/v2/security-group", map[string]string{
		"name": exoscaleGroup, "description": "MyInfra",
	})
	if err != nil {
		return "", err
	}
	if status >= 300 {
		return "", refused(status, body)
	}
	id, err := c.exoscaleOperation(ctx, creds, zone, body)
	if err != nil {
		return "", err
	}
	rules := []map[string]any{
		{"flow-direction": "ingress", "protocol": "tcp", "start-port": 1, "end-port": 65535, "network": "0.0.0.0/0", "description": "MyInfra tcp"},
		{"flow-direction": "ingress", "protocol": "udp", "start-port": 1, "end-port": 65535, "network": "0.0.0.0/0", "description": "MyInfra udp"},
		{"flow-direction": "ingress", "protocol": "icmp", "icmp": map[string]int{"type": 8, "code": 0}, "network": "0.0.0.0/0", "description": "MyInfra ping"},
	}
	for _, rule := range rules {
		status, body, err := c.exoscaleCall(ctx, creds, zone, http.MethodPost, "/v2/security-group/"+id+"/rules", rule)
		if err != nil {
			return id, err
		}
		if status >= 300 {
			return id, refused(status, body)
		}
		if _, err := c.exoscaleOperation(ctx, creds, zone, body); err != nil {
			return id, err
		}
	}
	return id, nil
}

// exoscaleOperation waits for an asynchronous operation and returns the id of the resource it touched.
func (c Client) exoscaleOperation(ctx context.Context, creds catalog.Creds, zone string, body []byte) (string, error) {
	var op struct {
		ID        string `json:"id"`
		State     string `json:"state"`
		Reference struct {
			ID string `json:"id"`
		} `json:"reference"`
	}
	if json.Unmarshal(body, &op) != nil || op.ID == "" {
		return "", fmt.Errorf("Le provider a refusé la machine.")
	}
	deadline := time.Now().Add(4 * time.Minute)
	for {
		switch op.State {
		case "success":
			return op.Reference.ID, nil
		case "failure", "timeout":
			return "", fmt.Errorf("Le provider a refusé la machine.")
		}
		if time.Now().After(deadline) {
			return "", fmt.Errorf("Le provider ne répond pas.")
		}
		select {
		case <-ctx.Done():
			return "", fmt.Errorf("Le provider ne répond pas.")
		case <-time.After(pollEvery):
		}
		status, raw, err := c.exoscaleCall(ctx, creds, zone, http.MethodGet, "/v2/operation/"+op.ID, nil)
		if err != nil {
			return "", err
		}
		if status >= 300 {
			return "", refused(status, raw)
		}
		if json.Unmarshal(raw, &op) != nil {
			return "", fmt.Errorf("Le provider ne répond pas.")
		}
		if op.ID == "" {
			return "", fmt.Errorf("Le provider ne répond pas.")
		}
	}
}

func (c Client) destroyExoscale(ctx context.Context, creds catalog.Creds, id string) error {
	zone, instance, ok := strings.Cut(id, ":")
	if !ok || instance == "" {
		return nil
	}
	status, body, err := c.exoscaleCall(ctx, creds, zone, http.MethodDelete, "/v2/instance/"+instance, nil)
	if err != nil {
		return err
	}
	if status == http.StatusNotFound || status < 300 {
		return nil
	}
	return refused(status, body)
}
