package catalog

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"sort"
	"strings"
)

func (c Client) infomaniak(ctx context.Context, id, secret, location string) (Catalog, error) {
	base := c.InfomaniakBase
	if base == "" {
		base = "https://api.pub1.infomaniak.cloud"
	}
	base = strings.TrimRight(base, "/")
	token, project, endpoints, err := infomaniakAuth(ctx, c.http(), base, id, secret)
	if err != nil {
		return Catalog{}, err
	}
	locations := infomaniakLocations(endpoints)
	if len(locations) == 0 {
		return Catalog{}, errEmpty("Aucune région.")
	}
	chosen := pickLocation(locations, location, "dc3-a")
	compute := endpointURL(endpoints, "compute", chosen)
	if compute == "" {
		return Catalog{}, errEmpty("Le provider ne répond pas.")
	}
	header := map[string]string{"X-Auth-Token": token}
	flavorsBody, status, err := fetch(ctx, c.http(), http.MethodGet, strings.TrimRight(compute, "/")+"/flavors/detail", nil, header)
	if err != nil {
		return Catalog{}, err
	}
	if status == http.StatusNotFound && project != "" && !strings.Contains(compute, project) {
		flavorsBody, status, err = fetch(ctx, c.http(), http.MethodGet, strings.TrimRight(compute, "/")+"/"+project+"/flavors/detail", nil, header)
		if err != nil {
			return Catalog{}, err
		}
	}
	if status >= 300 {
		return Catalog{}, errEmpty("Le provider ne répond pas.")
	}
	flavors, err := parseInfomaniakFlavors(flavorsBody)
	if err != nil {
		return Catalog{}, err
	}
	prices := map[string]string{}
	if rating := endpointURL(endpoints, "rating", chosen); rating != "" {
		prices = infomaniakQuote(ctx, c.http(), rating, header, flavors)
	}
	return infomaniakCatalog(locations, chosen, flavors, prices), nil
}

type infoFlavor struct {
	ID     string
	Name   string
	VCPU   int
	Memory int
	Disk   int
}

type infoEndpoint struct {
	Type   string
	Region string
	URL    string
}

func infomaniakAuth(ctx context.Context, client *http.Client, base, id, secret string) (string, string, []infoEndpoint, error) {
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
		return "", "", nil, err
	}
	body, status, header, err := fetchHeader(ctx, client, http.MethodPost, base+"/identity/v3/auth/tokens", payload, map[string]string{"Content-Type": "application/json"})
	if err != nil {
		return "", "", nil, err
	}
	if status != http.StatusOK && status != http.StatusCreated {
		return "", "", nil, errEmpty("identifiants refusés")
	}
	token := strings.TrimSpace(header.Get("X-Subject-Token"))
	if token == "" {
		return "", "", nil, errEmpty("identifiants refusés")
	}
	var parsed struct {
		Token struct {
			Project struct {
				ID string `json:"id"`
			} `json:"project"`
			Catalog []struct {
				Type      string `json:"type"`
				Endpoints []struct {
					Interface string `json:"interface"`
					Region    string `json:"region"`
					RegionID  string `json:"region_id"`
					URL       string `json:"url"`
				} `json:"endpoints"`
			} `json:"catalog"`
		} `json:"token"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", "", nil, errEmpty("Le provider ne répond pas.")
	}
	endpoints := make([]infoEndpoint, 0)
	for _, service := range parsed.Token.Catalog {
		for _, item := range service.Endpoints {
			if item.Interface != "" && item.Interface != "public" {
				continue
			}
			region := item.Region
			if region == "" {
				region = item.RegionID
			}
			if item.URL == "" || region == "" {
				continue
			}
			endpoints = append(endpoints, infoEndpoint{Type: service.Type, Region: region, URL: item.URL})
		}
	}
	return token, parsed.Token.Project.ID, endpoints, nil
}

func infomaniakLocations(endpoints []infoEndpoint) []Location {
	labels := map[string]string{"dc3-a": "Genève", "dc4-a": "Zurich"}
	seen := map[string]bool{}
	locations := make([]Location, 0)
	for _, item := range endpoints {
		if item.Type != "compute" || seen[item.Region] {
			continue
		}
		seen[item.Region] = true
		label := labels[item.Region]
		if label == "" {
			label = item.Region
		}
		locations = append(locations, Location{ID: item.Region, Label: label})
	}
	sort.Slice(locations, func(i, j int) bool { return locations[i].Label < locations[j].Label })
	return locations
}

func endpointURL(endpoints []infoEndpoint, kind, region string) string {
	for _, item := range endpoints {
		if item.Type == kind && item.Region == region {
			return item.URL
		}
	}
	return ""
}

func parseInfomaniakFlavors(body []byte) ([]infoFlavor, error) {
	var payload struct {
		Flavors []struct {
			ID       string `json:"id"`
			Name     string `json:"name"`
			RAM      int    `json:"ram"`
			Disk     int    `json:"disk"`
			VCPUs    int    `json:"vcpus"`
			Disabled bool   `json:"OS-FLV-DISABLED:disabled"`
			IsPublic *bool  `json:"os-flavor-access:is_public"`
		} `json:"flavors"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	flavors := make([]infoFlavor, 0, len(payload.Flavors))
	for _, item := range payload.Flavors {
		if item.Disabled || item.ID == "" || item.Name == "" || item.VCPUs <= 0 || item.RAM <= 0 {
			continue
		}
		if item.IsPublic != nil && !*item.IsPublic {
			continue
		}
		flavors = append(flavors, infoFlavor{ID: item.ID, Name: item.Name, VCPU: item.VCPUs, Memory: item.RAM, Disk: item.Disk})
	}
	sort.Slice(flavors, func(i, j int) bool {
		if flavors[i].VCPU != flavors[j].VCPU {
			return flavors[i].VCPU < flavors[j].VCPU
		}
		if flavors[i].Memory != flavors[j].Memory {
			return flavors[i].Memory < flavors[j].Memory
		}
		return flavors[i].Name < flavors[j].Name
	})
	if len(flavors) == 0 {
		return nil, errEmpty("Aucune offre.")
	}
	return flavors, nil
}

func infomaniakCatalog(locations []Location, chosen string, flavors []infoFlavor, prices map[string]string) Catalog {
	offers := make([]Offer, 0, len(flavors))
	for _, flavor := range flavors {
		hourly := prices[flavor.ID]
		if hourly == "" {
			hourly = prices[flavor.Name]
		}
		offer := Offer{
			ID: flavor.ID, Name: flavor.Name, Location: chosen,
			VCPU: flavor.VCPU, MemoryMB: flavor.Memory, DiskGB: flavor.Disk,
		}
		if hourly != "" && hourly != "0" {
			offer.Hourly = hourly
			offer.Monthly = monthlyFromHourly(hourly)
		}
		offers = append(offers, offer)
	}
	note := "Infomaniak facture chaque heure entamée. Le mois affiché correspond à 730 heures."
	priced := 0
	for _, offer := range offers {
		if offer.Hourly != "" {
			priced++
		}
	}
	if priced == 0 {
		note += " Le devis de prix n'a pas répondu."
	}
	return Catalog{
		Provider: "infomaniak", Currency: "EUR", Location: chosen, Locations: locations,
		Periods: []string{"hour", "month"}, Note: note, Offers: offers,
	}
}

func infomaniakQuote(ctx context.Context, client *http.Client, rating string, header map[string]string, flavors []infoFlavor) map[string]string {
	for _, service := range []string{"compute", "instance"} {
		prices := quoteService(ctx, client, rating, header, service, flavors)
		if len(prices) > 0 {
			return prices
		}
	}
	return nil
}

func quoteService(ctx context.Context, client *http.Client, rating string, header map[string]string, service string, flavors []infoFlavor) map[string]string {
	type resource struct {
		Service string            `json:"service"`
		Volume  string            `json:"volume"`
		Desc    map[string]string `json:"desc"`
	}
	resources := make([]resource, 0, len(flavors))
	for _, flavor := range flavors {
		resources = append(resources, resource{
			Service: service,
			Volume:  "1",
			Desc:    map[string]string{"flavor_id": flavor.ID, "flavor_name": flavor.Name},
		})
	}
	payload, err := json.Marshal(map[string]any{"resources": resources})
	if err != nil {
		return nil
	}
	body, status, err := fetch(ctx, client, http.MethodPost, strings.TrimRight(rating, "/")+"/v1/rating/quote", payload, header)
	if err != nil || status >= 300 {
		return nil
	}
	var parsed struct {
		Resources []struct {
			Rating json.RawMessage `json:"rating"`
			Cost   json.RawMessage `json:"cost"`
		} `json:"resources"`
	}
	if json.Unmarshal(body, &parsed) != nil || len(parsed.Resources) != len(flavors) {
		return nil
	}
	prices := map[string]string{}
	for i, flavor := range flavors {
		hourly := ratingMoney(parsed.Resources[i].Rating)
		if hourly == "" {
			hourly = ratingMoney(parsed.Resources[i].Cost)
		}
		if hourly != "" && hourly != "0" {
			prices[flavor.ID] = hourly
		}
	}
	return prices
}

func ratingMoney(raw json.RawMessage) string {
	raw = json.RawMessage(strings.TrimSpace(string(raw)))
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return money(text)
	}
	var value float64
	if json.Unmarshal(raw, &value) == nil && value > 0 {
		return formatMoney(value)
	}
	return ""
}

func fetch(ctx context.Context, client *http.Client, method, url string, payload []byte, header map[string]string) ([]byte, int, error) {
	body, status, _, err := fetchHeader(ctx, client, method, url, payload, header)
	return body, status, err
}

func fetchHeader(ctx context.Context, client *http.Client, method, url string, payload []byte, header map[string]string) ([]byte, int, http.Header, error) {
	var reader io.Reader
	if payload != nil {
		reader = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, reader)
	if err != nil {
		return nil, 0, nil, errEmpty("Le provider ne répond pas.")
	}
	if payload != nil && (header == nil || header["Content-Type"] == "") {
		req.Header.Set("Content-Type", "application/json")
	}
	for key, value := range header {
		req.Header.Set(key, value)
	}
	res, err := client.Do(req)
	if err != nil {
		return nil, 0, nil, errEmpty("Le provider ne répond pas.")
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 32<<20))
	if err != nil {
		return nil, res.StatusCode, res.Header, errEmpty("Le provider ne répond pas.")
	}
	return body, res.StatusCode, res.Header, nil
}
