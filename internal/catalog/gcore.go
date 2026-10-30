package catalog

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// GcoreProject returns the project MyInfra uses: the default one, else the first.
func GcoreProject(raw []byte) string {
	var payload struct {
		Results []struct {
			ID        int64 `json:"id"`
			IsDefault bool  `json:"is_default"`
		} `json:"results"`
	}
	if json.Unmarshal(raw, &payload) != nil || len(payload.Results) == 0 {
		return ""
	}
	for _, item := range payload.Results {
		if item.IsDefault {
			return strconv.FormatInt(item.ID, 10)
		}
	}
	return strconv.FormatInt(payload.Results[0].ID, 10)
}

// GcoreRegionID extracts the numeric region id from a location id ("luxembourg-2-76").
func GcoreRegionID(location string) string {
	location = strings.TrimSpace(location)
	if i := strings.LastIndex(location, "-"); i >= 0 {
		location = location[i+1:]
	}
	if _, err := strconv.Atoi(location); err != nil {
		return ""
	}
	return location
}

func (c Client) gcore(ctx context.Context, token, location string) (Catalog, error) {
	base := c.GcoreBase
	if base == "" {
		base = "https://api.gcore.com"
	}
	base = strings.TrimRight(base, "/")
	header := map[string]string{"Authorization": "APIKey " + strings.TrimSpace(token)}
	projects, err := get(ctx, c.http(), base+"/cloud/v1/projects", header)
	if err != nil {
		return Catalog{}, err
	}
	project := GcoreProject(projects)
	if project == "" {
		return Catalog{}, fmt.Errorf("Aucun projet Gcore.")
	}
	regions, err := get(ctx, c.http(), base+"/cloud/v1/regions?limit=100", header)
	if err != nil {
		return Catalog{}, err
	}
	locations := parseGcoreRegions(regions)
	if len(locations) == 0 {
		return Catalog{}, fmt.Errorf("Aucune offre disponible.")
	}
	chosen := pickGcore(locations, location)
	region := GcoreRegionID(chosen)
	flavors, err := get(ctx, c.http(), base+"/cloud/v1/flavors/"+project+"/"+region+"?include_prices=true&disabled=false", header)
	if err != nil {
		return Catalog{}, err
	}
	return parseGcore(flavors, locations, chosen)
}

func pickGcore(locations []Location, want string) string {
	for _, item := range locations {
		if item.ID == want {
			return want
		}
	}
	for _, item := range locations {
		if strings.HasPrefix(item.ID, "luxembourg") {
			return item.ID
		}
	}
	return locations[0].ID
}

func parseGcoreRegions(raw []byte) []Location {
	var payload struct {
		Results []struct {
			ID          int64  `json:"id"`
			DisplayName string `json:"display_name"`
			Country     string `json:"country"`
			State       string `json:"state"`
			HasKvm      *bool  `json:"has_kvm"`
			HasBasicVM  *bool  `json:"has_basic_vm"`
		} `json:"results"`
	}
	if json.Unmarshal(raw, &payload) != nil {
		return nil
	}
	var locations []Location
	for _, region := range payload.Results {
		if region.ID == 0 || (region.State != "" && !strings.EqualFold(region.State, "ACTIVE")) {
			continue
		}
		if region.HasKvm != nil && !*region.HasKvm {
			continue
		}
		name := strings.TrimSpace(region.DisplayName)
		if name == "" {
			name = "region-" + strconv.FormatInt(region.ID, 10)
		}
		label := name
		if region.Country != "" {
			label += ", " + region.Country
		}
		locations = append(locations, Location{ID: slug(name) + "-" + strconv.FormatInt(region.ID, 10), Label: label})
	}
	sort.Slice(locations, func(i, j int) bool { return locations[i].Label < locations[j].Label })
	return locations
}

func slug(text string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(text) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			dash = false
		default:
			if !dash && b.Len() > 0 {
				b.WriteByte('-')
				dash = true
			}
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "region"
	}
	return out
}

func parseGcore(raw []byte, locations []Location, chosen string) (Catalog, error) {
	var payload struct {
		Results []struct {
			ID           string  `json:"flavor_id"`
			Name         string  `json:"flavor_name"`
			Disabled     bool    `json:"disabled"`
			RAM          int64   `json:"ram"`
			VCPUs        int     `json:"vcpus"`
			CurrencyCode string  `json:"currency_code"`
			PerHour      float64 `json:"price_per_hour"`
			PerMonth     float64 `json:"price_per_month"`
			GPUs         int     `json:"gpu_count"`
		} `json:"results"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return Catalog{}, fmt.Errorf("Le provider ne répond pas.")
	}
	currency := ""
	offers := make([]Offer, 0)
	storage := []Storage{{ID: "boot", Label: "Disque", MinGB: 10, MaxGB: 1000, Required: true}}
	for _, item := range payload.Results {
		id := strings.ToLower(item.ID)
		if item.ID == "" || item.Disabled || item.VCPUs <= 0 || gcoreUnsupported(id) {
			continue
		}
		hourly := formatMoney(item.PerHour)
		monthly := formatMoney(item.PerMonth)
		if item.PerHour <= 0 && item.PerMonth <= 0 {
			continue
		}
		if item.PerHour <= 0 {
			hourly = ""
		}
		if item.PerMonth <= 0 {
			monthly = monthlyFromHourly(hourly)
		}
		if currency == "" {
			currency = strings.ToUpper(item.CurrencyCode)
		}
		name := item.Name
		if name == "" {
			name = item.ID
		}
		memory := item.RAM
		if memory > 1<<20 {
			memory /= 1024 * 1024
		}
		count := item.GPUs
		if count <= 0 && strings.Contains(id, "gpu") {
			count = 1
		}
		offers = append(offers, Offer{
			ID: item.ID, Name: name, Location: chosen,
			VCPU: item.VCPUs, MemoryMB: int(memory),
			GPUs: count, GPU: gpuModel(count, item.ID, item.Name),
			Hourly: hourly, Monthly: monthly, Storage: storage,
		})
	}
	if len(offers) == 0 {
		return Catalog{}, fmt.Errorf("Aucune offre disponible.")
	}
	if currency == "" {
		currency = "EUR"
	}
	sort.Slice(offers, func(i, j int) bool {
		a, _ := strconv.ParseFloat(offers[i].Monthly, 64)
		b, _ := strconv.ParseFloat(offers[j].Monthly, 64)
		if a != b {
			return a < b
		}
		return offers[i].Name < offers[j].Name
	})
	periods := []string{"hour", "month"}
	return Catalog{
		Provider: "gcore", Currency: currency, Location: chosen, Locations: locations,
		Periods: periods,
		Note:    "Gcore facture à l'heure. Le disque est facturé en plus. Les machines partagées et bare metal sont écartées.",
		Offers:  offers,
	}, nil
}

func gcoreUnsupported(id string) bool {
	return strings.HasPrefix(id, "bm") || strings.Contains(id, "shared") ||
		strings.Contains(id, "-ai-") || strings.HasPrefix(id, "g1s") || strings.HasPrefix(id, "g2s")
}
