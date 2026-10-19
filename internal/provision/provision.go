package provision

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"time"

	"myinfra/internal/catalog"
)

type Spec struct {
	Name      string
	Location  string
	OfferID   string
	OfferName string
	UserData  string
	RootGB    int
	ProjectID string
	SSHKeys   []string
	// Key names the purchase: the same key finds the same machine again
	// after an interruption, instead of buying a second one.
	Key string
	// Filled by prepare, for the providers whose OpenTofu resource only takes
	// their own identifiers.
	ImageID  string
	GroupID  string
	DiskTier string
	KeyName  string
}

type Machine struct {
	ID   string
	IPv4 string
}

type Project struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func (c Client) Projects(ctx context.Context, provider string, creds catalog.Creds) ([]Project, error) {
	switch provider {
	case "ovh":
		return c.ovhProjects(ctx, creds)
	case "digitalocean":
		return c.digitalOceanProjects(ctx, creds.Token)
	default:
		return nil, nil
	}
}

type Client struct {
	HTTP             *http.Client
	HetznerBase      string
	TofuDir          string // si renseigné, Hetzner passe par OpenTofu (prototype)
	TofuBin          string
	DigitalOceanBase string
	ScalewayBase     string
	OVHBase          string
	InfomaniakBase   string
	UpCloudBase      string
	VultrBase        string
	AkamaiBase       string
	ExoscaleBase     string
	GcoreBase        string
	KamateraBase     string
	IonosBase        string
}

func (c Client) http() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 30 * time.Second}
}

func (c Client) Create(ctx context.Context, provider string, creds catalog.Creds, spec Spec) (Machine, error) {
	switch provider {
	case "hetzner", "digitalocean", "scaleway", "ovh", "infomaniak", "upcloud", "vultr", "akamai", "exoscale", "gcore", "kamatera", "ionos", "openstack", "proxmox":
		return c.createStack(ctx, provider, creds, spec)
	default:
		return Machine{}, fmt.Errorf("Provider inconnu.")
	}
}

func (c Client) Destroy(ctx context.Context, provider string, creds catalog.Creds, id string) error {
	if strings.TrimSpace(id) == "" {
		return nil
	}
	stackErr := c.destroyStack(ctx, provider, creds, id)
	if stackErr == nil {
		return nil
	}
	if err := c.destroyHTTP(ctx, provider, creds, id); err == nil {
		return nil
	}
	return stackErr
}

func (c Client) destroyHTTP(ctx context.Context, provider string, creds catalog.Creds, id string) error {
	switch provider {
	case "hetzner":
		return c.destroy(ctx, c.base(c.HetznerBase, "https://api.hetzner.cloud")+"/v1/servers/"+id, bearer(creds.Token))
	case "digitalocean":
		return c.destroy(ctx, c.base(c.DigitalOceanBase, "https://api.digitalocean.com")+"/v2/droplets/"+id, bearer(creds.Token))
	case "scaleway":
		zone, serverID := splitScaleway(id)
		return c.destroyScaleway(ctx, c.base(c.ScalewayBase, "https://api.scaleway.com"), zone, serverID, scalewayAuth(creds.Token))
	case "ovh":
		return c.destroyOVH(ctx, creds, id)
	case "infomaniak":
		return c.destroyInfomaniak(ctx, creds.ApplicationKey, creds.ApplicationSecret, id)
	case "upcloud":
		return c.destroyUpCloud(ctx, creds.Token, id)
	case "vultr":
		return c.destroy(ctx, c.base(c.VultrBase, "https://api.vultr.com")+"/v2/instances/"+id, bearer(creds.Token))
	case "akamai":
		return c.destroy(ctx, c.base(c.AkamaiBase, "https://api.linode.com")+"/v4/linode/instances/"+id, bearer(creds.Token))
	case "exoscale":
		return c.destroyExoscale(ctx, creds, id)
	case "gcore":
		return c.destroyGcore(ctx, creds.Token, id)
	case "kamatera":
		return c.destroyKamatera(ctx, creds, id)
	case "ionos":
		return c.destroyIonos(ctx, creds.Token, id)
	default:
		return fmt.Errorf("Provider inconnu.")
	}
}

func (c Client) base(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return strings.TrimRight(value, "/")
}

func bearer(token string) map[string]string {
	return map[string]string{"Authorization": "Bearer " + strings.TrimSpace(token)}
}

func basicAuth(user, password string) map[string]string {
	raw := strings.TrimSpace(user) + ":" + password
	return map[string]string{"Authorization": "Basic " + base64.StdEncoding.EncodeToString([]byte(raw))}
}

func machineHost(name string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(name)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-', r == '.':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	host := strings.Trim(b.String(), "-.")
	if host == "" {
		host = "myinfra"
	}
	if len(host) > 60 {
		host = strings.Trim(host[:60], "-.")
	}
	if host == "" {
		host = "myinfra"
	}
	if !strings.Contains(host, ".") {
		host += ".local"
	}
	return host
}

func machineLabel(name string) string {
	label := strings.TrimSuffix(machineHost(name), ".local")
	if len(label) > 32 {
		label = strings.Trim(label[:32], "-.")
	}
	if label == "" {
		return "myinfra"
	}
	return label
}

func randomPass() string {
	raw := make([]byte, 18)
	_, _ = rand.Read(raw)
	return base64.RawURLEncoding.EncodeToString(raw) + "aA1"
}

func scalewayAuth(token string) map[string]string {
	return map[string]string{"X-Auth-Token": strings.TrimSpace(token)}
}

func (c Client) call(ctx context.Context, method, url string, header map[string]string, payload any) (int, []byte, error) {
	status, raw, _, err := c.callWithHeaders(ctx, method, url, header, payload)
	return status, raw, err
}

func (c Client) callWithHeaders(ctx context.Context, method, url string, header map[string]string, payload any) (int, []byte, http.Header, error) {
	var body io.Reader
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			return 0, nil, nil, err
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return 0, nil, nil, fmt.Errorf("Le provider ne répond pas.")
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for key, value := range header {
		req.Header.Set(key, value)
	}
	res, err := c.http().Do(req)
	if err != nil {
		return 0, nil, nil, fmt.Errorf("Le provider ne répond pas.")
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	if err != nil {
		return res.StatusCode, nil, res.Header, fmt.Errorf("Le provider ne répond pas.")
	}
	return res.StatusCode, raw, res.Header, nil
}

func (c Client) destroy(ctx context.Context, url string, header map[string]string) error {
	status, body, err := c.call(ctx, http.MethodDelete, url, header, nil)
	if err != nil {
		return err
	}
	if status == http.StatusNotFound || status == http.StatusNoContent || status < 300 {
		return nil
	}
	return refused(status, body)
}

func refused(status int, body []byte) error {
	text := string(body)
	if len(text) > 400 {
		text = text[:400]
	}
	slog.Error("provider", "status", status, "body", text)
	if ip := unauthorizedIP(text); ip != "" {
		return fmt.Errorf("Le provider refuse l'adresse IP de MyInfra (%s) : autorisez-la dans les accès de la clé API.", ip)
	}
	if strings.Contains(strings.ToLower(text), "restricted") && strings.Contains(strings.ToLower(text), "region") {
		return fmt.Errorf("Le provider a refusé la machine : ce lieu est restreint pour ce compte, choisissez un autre lieu.")
	}
	if strings.Contains(text, "CORE_LIMIT_REACHED") || strings.Contains(text, "quota") {
		return fmt.Errorf("Le provider a refusé la machine : le quota de ce compte est atteint (compte d'essai ?). Supprimez une machine ou augmentez la limite.")
	}
	return fmt.Errorf("Le provider a refusé la machine.")
}

var unauthorizedAddress = regexp.MustCompile(`Unauthorized IP address: ([0-9A-Fa-f:.]+)`)

// unauthorizedIP returns the address a key restricted to some IPs refused
// (Vultr), or "".
func unauthorizedIP(text string) string {
	match := unauthorizedAddress.FindStringSubmatch(text)
	if len(match) < 2 {
		return ""
	}
	return match[1]
}

func waitIP(ctx context.Context, read func() (string, error)) (string, error) {
	deadline := time.Now().Add(4 * time.Minute)
	for {
		ip, err := read()
		if err != nil {
			return "", err
		}
		if ip != "" {
			return ip, nil
		}
		if time.Now().After(deadline) {
			return "", fmt.Errorf("La machine n'a pas d'adresse.")
		}
		select {
		case <-ctx.Done():
			return "", fmt.Errorf("La machine n'a pas d'adresse.")
		case <-time.After(3 * time.Second):
		}
	}
}

type flexID string

func (f *flexID) UnmarshalJSON(raw []byte) error {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	if raw[0] == '"' {
		var text string
		if err := json.Unmarshal(raw, &text); err != nil {
			return err
		}
		*f = flexID(text)
		return nil
	}
	*f = flexID(raw)
	return nil
}

func publicIP(values []string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || strings.Contains(value, ":") {
			continue
		}
		if strings.HasPrefix(value, "10.") || strings.HasPrefix(value, "192.168.") || strings.HasPrefix(value, "172.16.") || strings.HasPrefix(value, "172.17.") || strings.HasPrefix(value, "172.18.") || strings.HasPrefix(value, "172.19.") || strings.HasPrefix(value, "172.2") || strings.HasPrefix(value, "172.3") {
			continue
		}
		return value
	}
	return ""
}
