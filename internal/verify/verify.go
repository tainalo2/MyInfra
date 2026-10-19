package verify

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"myinfra/internal/exo"
)

type Client struct {
	HTTP             *http.Client
	HetznerBase      string
	ScalewayBase     string
	OVHBase          string
	DigitalOceanBase string
	InfomaniakBase   string
	UpCloudBase      string
	VultrBase        string
	AkamaiBase       string
	ExoscaleBase     string
	GcoreBase        string
	KamateraBase     string
	IonosBase        string
	AllowTest        bool
}

func (c Client) http() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 10 * time.Second}
}

func (c Client) testToken(token string) bool {
	return c.AllowTest && strings.HasPrefix(strings.TrimSpace(token), "test-")
}

func (c Client) Hetzner(ctx context.Context, token string) error {
	if c.testToken(token) {
		return nil
	}
	base := c.HetznerBase
	if base == "" {
		base = "https://api.hetzner.cloud"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(base, "/")+"/v1/locations", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(token))
	return expectOK(c.http().Do(req))
}

func (c Client) Scaleway(ctx context.Context, token string) error {
	if c.testToken(token) {
		return nil
	}
	base := c.ScalewayBase
	if base == "" {
		base = "https://api.scaleway.com"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(base, "/")+"/instance/v1/zones/fr-par-1/servers", nil)
	if err != nil {
		return err
	}
	req.Header.Set("X-Auth-Token", strings.TrimSpace(token))
	return expectOK(c.http().Do(req))
}

func (c Client) ScalewayProject(ctx context.Context, token, projectID string) error {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return fmt.Errorf("Indiquez le projet.")
	}
	if c.testToken(token) {
		return nil
	}
	base := c.ScalewayBase
	if base == "" {
		base = "https://api.scaleway.com"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(base, "/")+"/account/v3/projects/"+url.PathEscape(projectID), nil)
	if err != nil {
		return err
	}
	req.Header.Set("X-Auth-Token", strings.TrimSpace(token))
	res, err := c.http().Do(req)
	if err != nil {
		return fmt.Errorf("La connexion au provider a échoué.")
	}
	defer res.Body.Close()
	io.Copy(io.Discard, io.LimitReader(res.Body, 1<<20))
	if res.StatusCode == http.StatusNotFound {
		return fmt.Errorf("Projet introuvable.")
	}
	if res.StatusCode >= 300 {
		return fmt.Errorf("Projet refusé.")
	}
	return nil
}

func (c Client) Cloudflare(ctx context.Context, token string) error {
	if c.testToken(token) {
		return nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.cloudflare.com/client/v4/user/tokens/verify", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(token))
	return expectOK(c.http().Do(req))
}

type OVH struct {
	ApplicationKey    string
	ApplicationSecret string
	ConsumerKey       string
}

func (c Client) OVH(ctx context.Context, creds OVH) error {
	if c.testToken(creds.ApplicationKey) && c.testToken(creds.ApplicationSecret) && c.testToken(creds.ConsumerKey) {
		return nil
	}
	base := c.OVHBase
	if base == "" {
		base = "https://eu.api.ovh.com/1.0"
	}
	base = strings.TrimRight(base, "/")
	client := c.http()

	timeReq, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/auth/time", nil)
	if err != nil {
		return err
	}
	timeRes, err := client.Do(timeReq)
	if err != nil {
		return fmt.Errorf("OVH ne répond pas")
	}
	defer timeRes.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(timeRes.Body, 64))
	if timeRes.StatusCode >= 300 {
		return fmt.Errorf("OVH ne répond pas")
	}
	serverTime, err := strconv.ParseInt(strings.TrimSpace(string(raw)), 10, 64)
	if err != nil {
		return fmt.Errorf("OVH ne répond pas")
	}

	method := http.MethodGet
	query := base + "/me"
	body := ""
	stamp := strconv.FormatInt(serverTime, 10)
	sum := sha1.Sum([]byte(creds.ApplicationSecret + "+" + creds.ConsumerKey + "+" + method + "+" + query + "+" + body + "+" + stamp))
	signature := fmt.Sprintf("$1$%x", sum)

	req, err := http.NewRequestWithContext(ctx, method, query, nil)
	if err != nil {
		return err
	}
	req.Header.Set("X-Ovh-Application", creds.ApplicationKey)
	req.Header.Set("X-Ovh-Consumer", creds.ConsumerKey)
	req.Header.Set("X-Ovh-Timestamp", stamp)
	req.Header.Set("X-Ovh-Signature", signature)
	return expectOK(client.Do(req))
}

func (c Client) GitHub(ctx context.Context, token string) error {
	if c.testToken(token) {
		return nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/user", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(token))
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "myinfra")
	return expectOK(c.http().Do(req))
}

func (c Client) GitLab(ctx context.Context, token string) error {
	if c.testToken(token) {
		return nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://gitlab.com/api/v4/user", nil)
	if err != nil {
		return err
	}
	req.Header.Set("PRIVATE-TOKEN", strings.TrimSpace(token))
	return expectOK(c.http().Do(req))
}

func (c Client) DigitalOcean(ctx context.Context, token string) error {
	if c.testToken(token) {
		return nil
	}
	base := c.DigitalOceanBase
	if base == "" {
		base = "https://api.digitalocean.com"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(base, "/")+"/v2/account", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(token))
	return expectOK(c.http().Do(req))
}

func (c Client) Vultr(ctx context.Context, token string) error {
	if c.testToken(token) {
		return nil
	}
	base := c.VultrBase
	if base == "" {
		base = "https://api.vultr.com"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(base, "/")+"/v2/account", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(token))
	return expectOK(c.http().Do(req))
}

func (c Client) Akamai(ctx context.Context, token string) error {
	if c.testToken(token) {
		return nil
	}
	base := c.AkamaiBase
	if base == "" {
		base = "https://api.linode.com"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(base, "/")+"/v4/profile", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(token))
	return expectOK(c.http().Do(req))
}

func (c Client) UpCloud(ctx context.Context, token string) error {
	if c.testToken(token) {
		return nil
	}
	base := c.UpCloudBase
	if base == "" {
		base = "https://api.upcloud.com"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(base, "/")+"/1.3/account", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(token))
	return expectOK(c.http().Do(req))
}

func (c Client) Infomaniak(ctx context.Context, id, secret string) error {
	if c.testToken(id) && c.testToken(secret) {
		return nil
	}
	base := c.InfomaniakBase
	if base == "" {
		base = "https://api.pub1.infomaniak.cloud"
	}
	payload, err := json.Marshal(map[string]any{
		"auth": map[string]any{
			"identity": map[string]any{
				"methods": []string{"application_credential"},
				"application_credential": map[string]string{
					"id":     strings.TrimSpace(id),
					"secret": strings.TrimSpace(secret),
				},
			},
		},
	})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(base, "/")+"/identity/v3/auth/tokens", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := c.http().Do(req)
	if err != nil {
		return fmt.Errorf("La connexion au provider a échoué.")
	}
	defer res.Body.Close()
	io.Copy(io.Discard, io.LimitReader(res.Body, 1<<20))
	if res.StatusCode != http.StatusOK && res.StatusCode != http.StatusCreated {
		return fmt.Errorf("Les identifiants ont été refusés.")
	}
	if strings.TrimSpace(res.Header.Get("X-Subject-Token")) == "" {
		return fmt.Errorf("Les identifiants ont été refusés.")
	}
	return nil
}

func (c Client) Exoscale(ctx context.Context, key, secret string) error {
	if c.testToken(key) && c.testToken(secret) {
		return nil
	}
	url := exo.Host(c.ExoscaleBase, "") + "/v2/zone"
	header, err := exo.Header(http.MethodGet, url, nil, key, secret)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	for name, value := range header {
		req.Header.Set(name, value)
	}
	return expectOK(c.http().Do(req))
}

func (c Client) Gcore(ctx context.Context, token string) error {
	if c.testToken(token) {
		return nil
	}
	base := c.GcoreBase
	if base == "" {
		base = "https://api.gcore.com"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(base, "/")+"/cloud/v1/projects", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "APIKey "+strings.TrimSpace(token))
	return expectOK(c.http().Do(req))
}

func (c Client) Kamatera(ctx context.Context, id, secret string) error {
	if c.testToken(id) && c.testToken(secret) {
		return nil
	}
	base := c.KamateraBase
	if base == "" {
		base = "https://cloudcli.cloudwm.com"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(base, "/")+"/service/servers", nil)
	if err != nil {
		return err
	}
	req.Header.Set("AuthClientId", strings.TrimSpace(id))
	req.Header.Set("AuthSecret", strings.TrimSpace(secret))
	req.Header.Set("Accept", "application/json")
	res, err := c.http().Do(req)
	if err != nil {
		return fmt.Errorf("La connexion au provider a échoué.")
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(res.Body, 8<<20))
	// Kamatera answers 200 with an error object when the keys are wrong.
	if res.StatusCode >= 300 || !strings.HasPrefix(strings.TrimSpace(string(body)), "[") {
		return fmt.Errorf("Les identifiants ont été refusés.")
	}
	return nil
}

func (c Client) Ionos(ctx context.Context, token string) error {
	if c.testToken(token) {
		return nil
	}
	base := c.IonosBase
	if base == "" {
		base = "https://api.ionos.com"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(base, "/")+"/cloudapi/v6/datacenters?depth=0&limit=1", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(token))
	return expectOK(c.http().Do(req))
}

func expectOK(res *http.Response, err error) error {
	if err != nil {
		return fmt.Errorf("La connexion au provider a échoué.")
	}
	defer res.Body.Close()
	io.Copy(io.Discard, io.LimitReader(res.Body, 1<<20))
	if res.StatusCode == http.StatusUnauthorized || res.StatusCode == http.StatusForbidden {
		return fmt.Errorf("Le jeton a été refusé.")
	}
	if res.StatusCode >= 300 {
		return fmt.Errorf("Le jeton a été refusé.")
	}
	return nil
}
