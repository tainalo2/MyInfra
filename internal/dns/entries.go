package dns

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

// Entry is one record of a zone, of any type, as its provider holds it.
// Content is the value alone: the target of a CNAME or an MX, the text of a
// TXT without its quotes. Priority is that of an MX.
type Entry struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Content  string `json:"content"`
	TTL      int    `json:"ttl,omitempty"`
	Priority int    `json:"priority,omitempty"`
	Proxied  *bool  `json:"proxied,omitempty"`
	id       string
}

// Writable are the types MyInfra writes; the others are read only.
var Writable = []string{"A", "AAAA", "CNAME", "TXT", "MX"}

func writable(kind string) bool {
	for _, k := range Writable {
		if k == kind {
			return true
		}
	}
	return false
}

// RecordSet is every record of one name and one type: setting it replaces
// what the provider has for that pair, and nothing else.
type RecordSet struct {
	Name    string  `json:"name"`
	Type    string  `json:"type"`
	Values  []Value `json:"values"`
	TTL     int     `json:"ttl,omitempty"`
	Proxied *bool   `json:"proxied,omitempty"`
}

type Value struct {
	Content  string `json:"content"`
	Priority int    `json:"priority,omitempty"`
}

func (v Value) key() string { return strconv.Itoa(v.Priority) + " " + v.Content }

// Entries lists the records of a zone, of every type, or those of one name
// when name is given; a zone of thousands stops at recordLimit.
func (c Client) Entries(ctx context.Context, provider string, creds Creds, zone, name string) ([]Entry, error) {
	zone = cleanHost(zone)
	name = cleanHost(name)
	var out []Entry
	var err error
	switch provider {
	case "cloudflare":
		out, err = c.cloudflareEntries(ctx, creds.Token, zone, name)
	case "ovh":
		out, err = c.ovhEntries(ctx, creds, zone, name)
	default:
		return nil, fmt.Errorf("Ce fournisseur DNS n'est pas pris en charge.")
	}
	if err != nil {
		return nil, err
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		if out[i].Type != out[j].Type {
			return out[i].Type < out[j].Type
		}
		if out[i].Priority != out[j].Priority {
			return out[i].Priority < out[j].Priority
		}
		return out[i].Content < out[j].Content
	})
	return out, nil
}

// SetRecordSet makes the provider hold exactly these values for this name
// and type: missing ones are added, others removed, the TTL followed.
func (c Client) SetRecordSet(ctx context.Context, provider string, creds Creds, zone string, set RecordSet) error {
	zone = cleanHost(zone)
	set.Name = cleanHost(set.Name)
	set.Type = strings.ToUpper(strings.TrimSpace(set.Type))
	if !writable(set.Type) {
		return fmt.Errorf("MyInfra n'écrit pas les enregistrements %s.", set.Type)
	}
	if len(set.Values) == 0 {
		return c.DeleteRecordSet(ctx, provider, creds, zone, set.Name, set.Type)
	}
	switch provider {
	case "cloudflare":
		return c.cloudflareSet(ctx, creds.Token, zone, set)
	case "ovh":
		return c.ovhSet(ctx, creds, zone, set)
	}
	return fmt.Errorf("Ce fournisseur DNS n'est pas pris en charge.")
}

// DeleteRecordSet removes every record of this name and type.
func (c Client) DeleteRecordSet(ctx context.Context, provider string, creds Creds, zone, name, kind string) error {
	zone = cleanHost(zone)
	name = cleanHost(name)
	kind = strings.ToUpper(strings.TrimSpace(kind))
	if !writable(kind) {
		return fmt.Errorf("MyInfra n'écrit pas les enregistrements %s.", kind)
	}
	switch provider {
	case "cloudflare":
		return c.cloudflareSet(ctx, creds.Token, zone, RecordSet{Name: name, Type: kind})
	case "ovh":
		return c.ovhSet(ctx, creds, zone, RecordSet{Name: name, Type: kind})
	}
	return fmt.Errorf("Ce fournisseur DNS n'est pas pris en charge.")
}

func cleanHost(name string) string {
	return strings.ToLower(strings.Trim(strings.TrimSpace(name), "."))
}

// unquote reads a TXT value as people write it, without the quotes some
// providers keep around it.
func unquote(content string) string {
	content = strings.TrimSpace(content)
	if len(content) >= 2 && strings.HasPrefix(content, `"`) && strings.HasSuffix(content, `"`) {
		parts := strings.Split(content[1:len(content)-1], `" "`)
		return strings.Join(parts, "")
	}
	return content
}

type cfRecord struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Name     string `json:"name"`
	Content  string `json:"content"`
	TTL      int    `json:"ttl"`
	Priority *int   `json:"priority"`
	Proxied  *bool  `json:"proxied"`
}

type cfRecords struct {
	Success    bool       `json:"success"`
	Result     []cfRecord `json:"result"`
	ResultInfo struct {
		Page       int `json:"page"`
		TotalPages int `json:"total_pages"`
	} `json:"result_info"`
}

func (c Client) cloudflareZoneID(ctx context.Context, token, zone string) (string, error) {
	var listed cfPage
	if err := c.cf(ctx, token, http.MethodGet, "/zones?name="+url.QueryEscape(zone), nil, &listed); err != nil {
		return "", err
	}
	if len(listed.Result) == 0 || listed.Result[0].ID == "" {
		return "", fmt.Errorf("Ce domaine est introuvable.")
	}
	return listed.Result[0].ID, nil
}

func (c Client) cloudflareEntries(ctx context.Context, token, zone, name string) ([]Entry, error) {
	id, err := c.cloudflareZoneID(ctx, token, zone)
	if err != nil {
		return nil, err
	}
	var out []Entry
	for page := 1; page <= 5; page++ {
		path := fmt.Sprintf("/zones/%s/dns_records?per_page=100&page=%d", url.PathEscape(id), page)
		if name != "" {
			path += "&name=" + url.QueryEscape(name)
		}
		var records cfRecords
		if err := c.cfRaw(ctx, token, http.MethodGet, path, nil, &records); err != nil {
			return nil, err
		}
		for _, r := range records.Result {
			e := Entry{Name: cleanHost(r.Name), Type: strings.ToUpper(r.Type), Content: r.Content, TTL: r.TTL, Proxied: r.Proxied, id: r.ID}
			if r.Priority != nil {
				e.Priority = *r.Priority
			}
			if e.Type == "TXT" {
				e.Content = unquote(e.Content)
			}
			if e.Type == "CNAME" || e.Type == "MX" {
				e.Content = cleanHost(e.Content)
			}
			out = append(out, e)
			if len(out) >= recordLimit {
				return out, nil
			}
		}
		if records.ResultInfo.Page >= records.ResultInfo.TotalPages || len(records.Result) == 0 {
			break
		}
	}
	return out, nil
}

// cfRaw is cf for answers with more fields than cfPage keeps.
func (c Client) cfRaw(ctx context.Context, token, method, path string, payload []byte, dest *cfRecords) error {
	var page json.RawMessage
	if err := c.cf(ctx, token, method, path, payload, &page); err != nil {
		return err
	}
	if err := json.Unmarshal(page, dest); err != nil || !dest.Success {
		return fmt.Errorf("Le compte DNS ne répond pas.")
	}
	return nil
}

func (c Client) cloudflareSet(ctx context.Context, token, zone string, set RecordSet) error {
	id, err := c.cloudflareZoneID(ctx, token, zone)
	if err != nil {
		return err
	}
	have, err := c.cloudflareEntries(ctx, token, zone, set.Name)
	if err != nil {
		return err
	}
	want := map[string]Value{}
	for _, v := range set.Values {
		want[v.key()] = v
	}
	ttl := set.TTL
	if ttl == 0 {
		ttl = 1
	}
	kept := map[string]bool{}
	for _, e := range have {
		if e.Name != set.Name || e.Type != set.Type {
			continue
		}
		key := Value{Content: e.Content, Priority: e.Priority}.key()
		if _, ok := want[key]; ok && !kept[key] {
			kept[key] = true
			proxied := e.Proxied
			if set.Proxied != nil {
				proxied = set.Proxied
			}
			if (set.TTL != 0 && e.TTL != ttl) || (set.Proxied != nil && (e.Proxied == nil || *e.Proxied != *set.Proxied)) {
				payload, _ := json.Marshal(cloudflareBody(set.Type, set.Name, want[key], ttl, proxied))
				if err := c.cf(ctx, token, http.MethodPut, "/zones/"+url.PathEscape(id)+"/dns_records/"+url.PathEscape(e.id), payload, nil); err != nil {
					return err
				}
			}
			continue
		}
		if err := c.cf(ctx, token, http.MethodDelete, "/zones/"+url.PathEscape(id)+"/dns_records/"+url.PathEscape(e.id), nil, nil); err != nil {
			return err
		}
	}
	for key, v := range want {
		if kept[key] {
			continue
		}
		proxied := set.Proxied
		payload, _ := json.Marshal(cloudflareBody(set.Type, set.Name, v, ttl, proxied))
		if err := c.cf(ctx, token, http.MethodPost, "/zones/"+url.PathEscape(id)+"/dns_records", payload, nil); err != nil {
			return err
		}
	}
	return nil
}

func cloudflareBody(kind, name string, v Value, ttl int, proxied *bool) map[string]any {
	body := map[string]any{"type": kind, "name": name, "content": v.Content, "ttl": ttl}
	if kind == "MX" {
		body["priority"] = v.Priority
	}
	if kind == "A" || kind == "AAAA" || kind == "CNAME" {
		body["proxied"] = proxied != nil && *proxied
	}
	return body
}

type ovhRecord struct {
	ID        int64  `json:"id"`
	FieldType string `json:"fieldType"`
	SubDomain string `json:"subDomain"`
	Target    string `json:"target"`
	TTL       int    `json:"ttl"`
}

func (c Client) ovhEntries(ctx context.Context, creds Creds, zone, name string) ([]Entry, error) {
	path := "/domain/zone/" + url.PathEscape(zone) + "/record"
	if name != "" {
		sub, err := subdomainOf(name, zone)
		if err != nil {
			return nil, err
		}
		path += "?subDomain=" + url.QueryEscape(sub)
	}
	var ids []int64
	if err := c.ovh(ctx, creds, http.MethodGet, path, nil, &ids); err != nil {
		return nil, err
	}
	var out []Entry
	for _, id := range ids {
		if len(out) >= recordLimit {
			break
		}
		var r ovhRecord
		if err := c.ovh(ctx, creds, http.MethodGet, fmt.Sprintf("/domain/zone/%s/record/%d", url.PathEscape(zone), id), nil, &r); err != nil {
			return nil, err
		}
		host := zone
		if r.SubDomain != "" {
			host = strings.ToLower(r.SubDomain) + "." + zone
		}
		if name != "" && host != name {
			continue
		}
		e := Entry{Name: host, Type: strings.ToUpper(r.FieldType), Content: r.Target, TTL: r.TTL, id: strconv.FormatInt(id, 10)}
		switch e.Type {
		case "TXT":
			e.Content = unquote(e.Content)
		case "MX":
			if prio, target, ok := strings.Cut(strings.TrimSpace(e.Content), " "); ok {
				if n, err := strconv.Atoi(prio); err == nil {
					e.Priority, e.Content = n, cleanHost(target)
				}
			}
		case "CNAME":
			e.Content = cleanHost(e.Content)
		}
		out = append(out, e)
	}
	return out, nil
}

func ovhTarget(kind string, v Value) string {
	switch kind {
	case "MX":
		return strconv.Itoa(v.Priority) + " " + v.Content + "."
	case "CNAME":
		return v.Content + "."
	case "TXT":
		return `"` + strings.ReplaceAll(v.Content, `"`, `\"`) + `"`
	}
	return v.Content
}

func (c Client) ovhSet(ctx context.Context, creds Creds, zone string, set RecordSet) error {
	sub, err := subdomainOf(set.Name, zone)
	if err != nil {
		return err
	}
	have, err := c.ovhEntries(ctx, creds, zone, set.Name)
	if err != nil {
		return err
	}
	want := map[string]Value{}
	for _, v := range set.Values {
		want[v.key()] = v
	}
	kept := map[string]bool{}
	changed := false
	for _, e := range have {
		if e.Type != set.Type {
			continue
		}
		key := Value{Content: e.Content, Priority: e.Priority}.key()
		recordPath := "/domain/zone/" + url.PathEscape(zone) + "/record/" + e.id
		if _, ok := want[key]; ok && !kept[key] {
			kept[key] = true
			if set.TTL != 0 && e.TTL != set.TTL {
				payload, _ := json.Marshal(map[string]any{"ttl": set.TTL})
				if err := c.ovh(ctx, creds, http.MethodPut, recordPath, payload, nil); err != nil {
					return err
				}
				changed = true
			}
			continue
		}
		if err := c.ovh(ctx, creds, http.MethodDelete, recordPath, nil, nil); err != nil {
			return err
		}
		changed = true
	}
	for key, v := range want {
		if kept[key] {
			continue
		}
		payload, _ := json.Marshal(map[string]any{"fieldType": set.Type, "subDomain": sub, "target": ovhTarget(set.Type, v), "ttl": set.TTL})
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
