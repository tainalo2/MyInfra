package dns

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

type Creds struct {
	Token             string
	ApplicationKey    string
	ApplicationSecret string
	ConsumerKey       string
}

type Client struct {
	HTTP           *http.Client
	OVHBase        string
	CloudflareBase string
	TofuDir        string
	TofuBin        string
	Confirm        func(context.Context, string, string, string) error
}

func (c Client) http() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 15 * time.Second}
}

func (c Client) Zones(ctx context.Context, provider string, creds Creds) ([]string, error) {
	switch provider {
	case "cloudflare":
		return c.cloudflareZones(ctx, creds.Token)
	case "ovh":
		return c.ovhZones(ctx, creds)
	default:
		return nil, fmt.Errorf("Ce compte DNS est inconnu.")
	}
}

func (c Client) DropAddress(ctx context.Context, provider string, creds Creds, zone, host string) error {
	zone = strings.ToLower(strings.Trim(strings.TrimSpace(zone), "."))
	host = strings.ToLower(strings.Trim(strings.TrimSpace(host), "."))
	if c.TofuDir != "" {
		if err := c.dropStack(ctx, provider, creds, zone, host); err == nil {
			return nil
		}
	}
	switch provider {
	case "cloudflare":
		return c.cloudflareDrop(ctx, creds.Token, zone, host)
	case "ovh":
		return c.ovhDrop(ctx, creds, zone, host)
	default:
		return fmt.Errorf("Ce compte DNS est inconnu.")
	}
}

func (c Client) publish(ctx context.Context, provider string, creds Creds, zone, host string, ips []string, kind string) error {
	if c.TofuDir != "" {
		return c.ensureStack(ctx, provider, creds, zone, host, ips, kind)
	}
	switch provider {
	case "cloudflare":
		return c.cloudflareAddress(ctx, creds.Token, zone, host, ips, kind)
	case "ovh":
		return c.ovhAddress(ctx, creds, zone, host, ips, kind)
	default:
		return fmt.Errorf("Ce compte DNS est inconnu.")
	}
}

func (c Client) EnsureAddress(ctx context.Context, provider string, creds Creds, zone, host, ip string) error {
	return c.EnsureAddresses(ctx, provider, creds, zone, host, []string{ip})
}

// EnsureAddresses makes the name answer with exactly these addresses: one
// record per machine of a cluster, so that losing one node keeps the name up.
func (c Client) EnsureAddresses(ctx context.Context, provider string, creds Creds, zone, host string, ips []string) error {
	byKind := map[string][]string{}
	for _, raw := range ips {
		parsed := net.ParseIP(strings.TrimSpace(raw))
		if parsed == nil {
			continue
		}
		kind := "AAAA"
		if parsed.To4() != nil {
			kind = "A"
		}
		if !contains(byKind[kind], parsed.String()) {
			byKind[kind] = append(byKind[kind], parsed.String())
		}
	}
	if len(byKind) == 0 {
		return fmt.Errorf("La machine n'a pas encore d'adresse.")
	}
	for _, kind := range []string{"A", "AAAA"} {
		list := byKind[kind]
		if len(list) == 0 {
			continue
		}
		sort.Strings(list)
		if err := c.publish(ctx, provider, creds, zone, host, list, kind); err != nil {
			return err
		}
	}
	confirm := c.Confirm
	if confirm == nil {
		confirm = confirmPublished
	}
	for _, list := range byKind {
		for _, ip := range list {
			if err := confirm(ctx, zone, host, ip); err != nil {
				return err
			}
		}
	}
	return nil
}

// Announced is what the zone's own name servers answer for the name now.
func (c Client) Announced(ctx context.Context, zone, host string) []string {
	found, err := lookupVia(ctx, strings.ToLower(strings.Trim(strings.TrimSpace(zone), ".")), host)
	if err != nil {
		return nil
	}
	sort.Strings(found)
	return found
}

func contains(list []string, value string) bool {
	for _, item := range list {
		if item == value {
			return true
		}
	}
	return false
}

// ForeignWildcard reports a catch-all address in the zone that is not the one just announced.
func (c Client) ForeignWildcard(ctx context.Context, zone, ip string) bool {
	zone = strings.ToLower(strings.Trim(strings.TrimSpace(zone), "."))
	found, err := lookupVia(ctx, zone, "myinfra-probe-"+strconv.FormatInt(time.Now().UnixNano(), 36)+"."+zone)
	if err != nil || len(found) == 0 {
		return false
	}
	for _, item := range found {
		if item == ip {
			return false
		}
	}
	return true
}

func confirmPublished(ctx context.Context, zone, host, ip string) error {
	deadline := time.Now().Add(8 * time.Second)
	var last error
	for {
		found, err := lookupVia(ctx, zone, host)
		if err == nil {
			for _, item := range found {
				if item == ip {
					return nil
				}
			}
			last = fmt.Errorf("Le nom n'est pas encore annoncé.")
		} else {
			last = err
		}
		if !time.Now().Before(deadline) {
			break
		}
		timer := time.NewTimer(400 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	if last != nil {
		return fmt.Errorf("Le nom n'est pas encore annoncé.")
	}
	return fmt.Errorf("Le nom n'est pas encore annoncé.")
}

func lookupVia(ctx context.Context, zone, host string) ([]string, error) {
	names, err := net.DefaultResolver.LookupNS(ctx, zone)
	if err != nil || len(names) == 0 {
		return nil, fmt.Errorf("Le nom n'est pas encore annoncé.")
	}
	var last error
	for _, item := range names {
		server := strings.TrimSuffix(item.Host, ".")
		ips, err := net.DefaultResolver.LookupHost(ctx, server)
		if err != nil {
			last = err
			continue
		}
		for _, ip := range ips {
			serverIP := ip
			network := "udp"
			if strings.Contains(serverIP, ":") {
				network = "udp6"
			}
			resolver := &net.Resolver{
				PreferGo: true,
				Dial: func(ctx context.Context, _, _ string) (net.Conn, error) {
					return (&net.Dialer{Timeout: 3 * time.Second}).DialContext(ctx, network, net.JoinHostPort(serverIP, "53"))
				},
			}
			found, err := resolver.LookupHost(ctx, host)
			if err == nil {
				return found, nil
			}
			last = err
		}
	}
	if last == nil {
		last = fmt.Errorf("Le nom n'est pas encore annoncé.")
	}
	return nil, last
}

var labelPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)

func Join(subdomain, zone string) (string, error) {
	zone = strings.ToLower(strings.Trim(strings.TrimSpace(zone), "."))
	subdomain = strings.ToLower(strings.Trim(strings.TrimSpace(subdomain), "."))
	if !validName(zone) || !strings.Contains(zone, ".") {
		return "", fmt.Errorf("Ce domaine n'est pas utilisable.")
	}
	if subdomain == "" {
		return "", fmt.Errorf("Indiquez le sous-domaine.")
	}
	if !validName(subdomain) {
		return "", fmt.Errorf("Ce sous-domaine n'est pas utilisable.")
	}
	host := subdomain + "." + zone
	if len(host) > 253 {
		return "", fmt.Errorf("Ce nom est trop long.")
	}
	return host, nil
}

func validName(name string) bool {
	if name == "" || strings.Contains(name, "..") {
		return false
	}
	for _, label := range strings.Split(name, ".") {
		if !labelPattern.MatchString(label) {
			return false
		}
	}
	return true
}

func subdomainOf(host, zone string) (string, error) {
	if host == zone {
		return "", nil
	}
	suffix := "." + zone
	if !strings.HasSuffix(host, suffix) {
		return "", fmt.Errorf("Ce nom n'appartient pas au domaine.")
	}
	return strings.TrimSuffix(host, suffix), nil
}

type cfPage struct {
	Success bool `json:"success"`
	Result  []struct {
		ID      string `json:"id"`
		Name    string `json:"name"`
		Content string `json:"content"`
	} `json:"result"`
	ResultInfo struct {
		Page       int `json:"page"`
		TotalPages int `json:"total_pages"`
	} `json:"result_info"`
}

func (c Client) cloudflareZones(ctx context.Context, token string) ([]string, error) {
	var names []string
	for page := 1; page <= 10; page++ {
		var body cfPage
		if err := c.cf(ctx, token, http.MethodGet, fmt.Sprintf("/zones?per_page=50&page=%d", page), nil, &body); err != nil {
			return nil, err
		}
		for _, zone := range body.Result {
			if zone.Name != "" {
				names = append(names, strings.ToLower(zone.Name))
			}
		}
		if body.ResultInfo.Page >= body.ResultInfo.TotalPages || len(body.Result) == 0 {
			break
		}
	}
	return names, nil
}

func (c Client) cloudflareAddress(ctx context.Context, token, zone, host string, ips []string, kind string) error {
	var listed cfPage
	if err := c.cf(ctx, token, http.MethodGet, "/zones?name="+url.QueryEscape(zone), nil, &listed); err != nil {
		return err
	}
	if len(listed.Result) == 0 || listed.Result[0].ID == "" {
		return fmt.Errorf("Ce domaine est introuvable.")
	}
	id := listed.Result[0].ID
	var records cfPage
	path := "/zones/" + url.PathEscape(id) + "/dns_records?type=" + kind + "&name=" + url.QueryEscape(host)
	if err := c.cf(ctx, token, http.MethodGet, path, nil, &records); err != nil {
		return err
	}
	have := map[string]bool{}
	for _, record := range records.Result {
		if record.ID == "" {
			continue
		}
		if contains(ips, record.Content) && !have[record.Content] {
			have[record.Content] = true
			continue
		}
		if err := c.cf(ctx, token, http.MethodDelete, "/zones/"+url.PathEscape(id)+"/dns_records/"+url.PathEscape(record.ID), nil, nil); err != nil {
			return err
		}
	}
	for _, ip := range ips {
		if have[ip] {
			continue
		}
		payload, err := json.Marshal(map[string]any{"type": kind, "name": host, "content": ip, "ttl": 60, "proxied": false})
		if err != nil {
			return err
		}
		if err := c.cf(ctx, token, http.MethodPost, "/zones/"+url.PathEscape(id)+"/dns_records", payload, nil); err != nil {
			return err
		}
	}
	return nil
}

func (c Client) cloudflareDrop(ctx context.Context, token, zone, host string) error {
	var listed cfPage
	if err := c.cf(ctx, token, http.MethodGet, "/zones?name="+url.QueryEscape(zone), nil, &listed); err != nil {
		return err
	}
	if len(listed.Result) == 0 || listed.Result[0].ID == "" {
		return nil
	}
	id := listed.Result[0].ID
	for _, kind := range []string{"A", "AAAA"} {
		var records cfPage
		path := "/zones/" + url.PathEscape(id) + "/dns_records?type=" + kind + "&name=" + url.QueryEscape(host)
		if err := c.cf(ctx, token, http.MethodGet, path, nil, &records); err != nil {
			return err
		}
		for _, record := range records.Result {
			if record.ID == "" {
				continue
			}
			if err := c.cf(ctx, token, http.MethodDelete, "/zones/"+url.PathEscape(id)+"/dns_records/"+url.PathEscape(record.ID), nil, nil); err != nil {
				return err
			}
		}
	}
	return nil
}

func (c Client) cf(ctx context.Context, token, method, path string, payload []byte, dest any) error {
	base := c.CloudflareBase
	if base == "" {
		base = "https://api.cloudflare.com/client/v4"
	}
	var body io.Reader
	if payload != nil {
		body = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(base, "/")+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(token))
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := c.http().Do(req)
	if err != nil {
		return fmt.Errorf("Le compte DNS ne répond pas.")
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if res.StatusCode == http.StatusUnauthorized || res.StatusCode == http.StatusForbidden {
		return fmt.Errorf("Le jeton a été refusé.")
	}
	if res.StatusCode >= 300 {
		return fmt.Errorf("L'annonce DNS a été refusée.")
	}
	if dest == nil {
		return nil
	}
	if err := json.Unmarshal(raw, dest); err != nil {
		return fmt.Errorf("Le compte DNS ne répond pas.")
	}
	if page, ok := dest.(*cfPage); ok && !page.Success {
		return fmt.Errorf("Le jeton a été refusé.")
	}
	return nil
}

func (c Client) ovhZones(ctx context.Context, creds Creds) ([]string, error) {
	var names []string
	if err := c.ovh(ctx, creds, http.MethodGet, "/domain/zone", nil, &names); err != nil {
		return nil, err
	}
	for i, name := range names {
		names[i] = strings.ToLower(name)
	}
	return names, nil
}

func (c Client) ovhAddress(ctx context.Context, creds Creds, zone, host string, ips []string, kind string) error {
	sub, err := subdomainOf(host, zone)
	if err != nil {
		return err
	}
	path := "/domain/zone/" + url.PathEscape(zone) + "/record?fieldType=" + kind + "&subDomain=" + url.QueryEscape(sub)
	var ids []int64
	if err := c.ovh(ctx, creds, http.MethodGet, path, nil, &ids); err != nil {
		return err
	}
	changed := false
	have := map[string]bool{}
	for _, id := range ids {
		var record struct {
			Target string `json:"target"`
		}
		recordPath := fmt.Sprintf("/domain/zone/%s/record/%d", url.PathEscape(zone), id)
		if err := c.ovh(ctx, creds, http.MethodGet, recordPath, nil, &record); err != nil {
			return err
		}
		if contains(ips, record.Target) && !have[record.Target] {
			have[record.Target] = true
			continue
		}
		if err := c.ovh(ctx, creds, http.MethodDelete, recordPath, nil, nil); err != nil {
			return err
		}
		changed = true
	}
	for _, ip := range ips {
		if have[ip] {
			continue
		}
		payload, err := json.Marshal(map[string]any{"fieldType": kind, "subDomain": sub, "target": ip, "ttl": 60})
		if err != nil {
			return err
		}
		if err := c.ovh(ctx, creds, http.MethodPost, "/domain/zone/"+url.PathEscape(zone)+"/record", payload, nil); err != nil {
			return err
		}
		changed = true
	}
	if !changed {
		return nil
	}
	return c.ovh(ctx, creds, http.MethodPost, "/domain/zone/"+url.PathEscape(zone)+"/refresh", []byte("{}"), nil)
}

func (c Client) ovhDrop(ctx context.Context, creds Creds, zone, host string) error {
	sub, err := subdomainOf(host, zone)
	if err != nil {
		return err
	}
	removed := false
	for _, kind := range []string{"A", "AAAA"} {
		path := "/domain/zone/" + url.PathEscape(zone) + "/record?fieldType=" + kind + "&subDomain=" + url.QueryEscape(sub)
		var ids []int64
		if err := c.ovh(ctx, creds, http.MethodGet, path, nil, &ids); err != nil {
			return err
		}
		for _, id := range ids {
			recordPath := fmt.Sprintf("/domain/zone/%s/record/%d", url.PathEscape(zone), id)
			if err := c.ovh(ctx, creds, http.MethodDelete, recordPath, nil, nil); err != nil {
				return err
			}
			removed = true
		}
	}
	if !removed {
		return nil
	}
	return c.ovh(ctx, creds, http.MethodPost, "/domain/zone/"+url.PathEscape(zone)+"/refresh", []byte("{}"), nil)
}

func (c Client) ovh(ctx context.Context, creds Creds, method, path string, payload []byte, dest any) error {
	base := c.OVHBase
	if base == "" {
		base = "https://eu.api.ovh.com/1.0"
	}
	base = strings.TrimRight(base, "/")
	stamp, err := c.ovhTime(ctx, base)
	if err != nil {
		return err
	}
	body := string(payload)
	query := base + path
	sum := sha1.Sum([]byte(creds.ApplicationSecret + "+" + creds.ConsumerKey + "+" + method + "+" + query + "+" + body + "+" + stamp))
	var reader io.Reader
	if payload != nil {
		reader = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, query, reader)
	if err != nil {
		return err
	}
	req.Header.Set("X-Ovh-Application", creds.ApplicationKey)
	req.Header.Set("X-Ovh-Consumer", creds.ConsumerKey)
	req.Header.Set("X-Ovh-Timestamp", stamp)
	req.Header.Set("X-Ovh-Signature", fmt.Sprintf("$1$%x", sum))
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := c.http().Do(req)
	if err != nil {
		return fmt.Errorf("Le compte DNS ne répond pas.")
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if res.StatusCode == http.StatusUnauthorized || res.StatusCode == http.StatusForbidden {
		return fmt.Errorf("Le jeton a été refusé.")
	}
	if res.StatusCode >= 300 {
		return fmt.Errorf("L'annonce DNS a été refusée.")
	}
	if dest == nil || len(raw) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, dest); err != nil {
		return fmt.Errorf("Le compte DNS ne répond pas.")
	}
	return nil
}

func (c Client) ovhTime(ctx context.Context, base string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/auth/time", nil)
	if err != nil {
		return "", err
	}
	res, err := c.http().Do(req)
	if err != nil {
		return "", fmt.Errorf("Le compte DNS ne répond pas.")
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 64))
	if res.StatusCode >= 300 {
		return "", fmt.Errorf("Le compte DNS ne répond pas.")
	}
	stamp, err := strconv.ParseInt(strings.TrimSpace(string(raw)), 10, 64)
	if err != nil {
		return "", fmt.Errorf("Le compte DNS ne répond pas.")
	}
	return strconv.FormatInt(stamp, 10), nil
}
