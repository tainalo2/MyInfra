package dns

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
)

// Record is one address record of a zone, as its provider holds it.
type Record struct {
	Name   string `json:"name"`
	Type   string `json:"type"`
	Target string `json:"target"`
}

// recordLimit keeps a zone with thousands of records from holding a page.
const recordLimit = 500

// Records lists the A and AAAA records of a zone: those MyInfra writes for
// applications and for itself, and the others of the zone.
func (c Client) Records(ctx context.Context, provider string, creds Creds, zone string) ([]Record, error) {
	zone = strings.ToLower(strings.Trim(strings.TrimSpace(zone), "."))
	var out []Record
	var err error
	switch provider {
	case "cloudflare":
		out, err = c.cloudflareRecords(ctx, creds.Token, zone)
	case "ovh":
		out, err = c.ovhRecords(ctx, creds, zone)
	default:
		return nil, fmt.Errorf("Ce fournisseur DNS n'est pas pris en charge.")
	}
	if err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].Target < out[j].Target
	})
	return out, nil
}

func (c Client) ovhRecords(ctx context.Context, creds Creds, zone string) ([]Record, error) {
	var out []Record
	for _, kind := range []string{"A", "AAAA"} {
		var ids []int64
		path := "/domain/zone/" + url.PathEscape(zone) + "/record?fieldType=" + kind
		if err := c.ovh(ctx, creds, http.MethodGet, path, nil, &ids); err != nil {
			return nil, err
		}
		for _, id := range ids {
			if len(out) >= recordLimit {
				return out, nil
			}
			var record struct {
				SubDomain string `json:"subDomain"`
				Target    string `json:"target"`
			}
			if err := c.ovh(ctx, creds, http.MethodGet, fmt.Sprintf("/domain/zone/%s/record/%d", url.PathEscape(zone), id), nil, &record); err != nil {
				return nil, err
			}
			name := zone
			if record.SubDomain != "" {
				name = strings.ToLower(record.SubDomain) + "." + zone
			}
			out = append(out, Record{Name: name, Type: kind, Target: record.Target})
		}
	}
	return out, nil
}

func (c Client) cloudflareRecords(ctx context.Context, token, zone string) ([]Record, error) {
	var listed cfPage
	if err := c.cf(ctx, token, http.MethodGet, "/zones?name="+url.QueryEscape(zone), nil, &listed); err != nil {
		return nil, err
	}
	if len(listed.Result) == 0 || listed.Result[0].ID == "" {
		return nil, fmt.Errorf("Ce domaine est introuvable.")
	}
	id := listed.Result[0].ID
	var out []Record
	for _, kind := range []string{"A", "AAAA"} {
		for page := 1; page <= 5; page++ {
			var records cfPage
			path := fmt.Sprintf("/zones/%s/dns_records?type=%s&per_page=100&page=%d", url.PathEscape(id), kind, page)
			if err := c.cf(ctx, token, http.MethodGet, path, nil, &records); err != nil {
				return nil, err
			}
			for _, record := range records.Result {
				out = append(out, Record{Name: strings.ToLower(record.Name), Type: kind, Target: record.Content})
			}
			if records.ResultInfo.Page >= records.ResultInfo.TotalPages || len(records.Result) == 0 {
				break
			}
		}
	}
	return out, nil
}
