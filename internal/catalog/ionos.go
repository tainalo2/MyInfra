package catalog

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// IONOS publishes no price API. These are the public vCPU server rates in euros
// (cloud.ionos.es/precios, checked on 17 October 2026).
const (
	ionosVCPUHour = 0.012
	ionosGBHour   = 0.002
	ionosDiskGB30 = 0.04
)

type ionosPreset struct {
	Cores int
	RAMGB int
}

var ionosPresets = []ionosPreset{{1, 1}, {1, 2}, {2, 4}, {2, 8}, {4, 8}, {4, 16}, {8, 16}, {8, 32}}

func (c Client) ionos(ctx context.Context, token, location string) (Catalog, error) {
	base := c.IonosBase
	if base == "" {
		base = "https://api.ionos.com"
	}
	header := map[string]string{"Authorization": "Bearer " + strings.TrimSpace(token)}
	raw, err := get(ctx, c.http(), strings.TrimRight(base, "/")+"/cloudapi/v6/locations?depth=1", header)
	if err != nil {
		return Catalog{}, err
	}
	return parseIonos(raw, location)
}

// IonosLocation turns an application id ("de-fra") into the API id ("de/fra").
func IonosLocation(id string) string {
	return strings.Replace(strings.TrimSpace(id), "-", "/", 1)
}

func parseIonos(raw []byte, location string) (Catalog, error) {
	var payload struct {
		Items []struct {
			ID         string `json:"id"`
			Properties struct {
				Name         string          `json:"name"`
				ImageAliases []string        `json:"imageAliases"`
				CPU          json.RawMessage `json:"cpuArchitecture"`
			} `json:"properties"`
		} `json:"items"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return Catalog{}, fmt.Errorf("Le provider ne répond pas.")
	}
	var locations []Location
	for _, item := range payload.Items {
		if item.ID == "" || !strings.Contains(item.ID, "/") {
			continue
		}
		if len(item.Properties.CPU) > 0 && strings.TrimSpace(string(item.Properties.CPU)) == "[]" {
			continue
		}
		if len(item.Properties.ImageAliases) > 0 && !hasPrefixAny(item.Properties.ImageAliases, "debian") {
			continue
		}
		label := item.Properties.Name
		if label == "" {
			label = item.ID
		}
		locations = append(locations, Location{ID: strings.Replace(item.ID, "/", "-", 1), Label: label})
	}
	sort.Slice(locations, func(i, j int) bool { return locations[i].Label < locations[j].Label })
	if len(locations) == 0 {
		return Catalog{}, fmt.Errorf("Aucune offre disponible.")
	}
	chosen := pickLocation(locations, location, "de-fra")
	diskHour := ionosDiskGB30 / 720
	storage := []Storage{{
		ID: "boot", Label: "Disque", MinGB: 10, MaxGB: 4000, Required: true,
		HourlyPerGB: formatMoney(diskHour), MonthlyPerGB: formatMoney(ionosDiskGB30),
	}}
	offers := make([]Offer, 0, len(ionosPresets))
	for _, preset := range ionosPresets {
		hourly := float64(preset.Cores)*ionosVCPUHour + float64(preset.RAMGB)*ionosGBHour
		offers = append(offers, Offer{
			ID:       fmt.Sprintf("vcpu-%d-%d", preset.Cores, preset.RAMGB),
			Name:     fmt.Sprintf("vCPU %d x %d Go", preset.Cores, preset.RAMGB),
			Location: chosen, VCPU: preset.Cores, MemoryMB: preset.RAMGB * 1024,
			Hourly: formatMoney(hourly), Monthly: formatMoney(hourly * 730), Storage: storage,
		})
	}
	return Catalog{
		Provider: "ionos", Currency: "EUR", Location: chosen, Locations: locations,
		Periods: []string{"hour", "month"},
		Note:    "IONOS facture à la minute, sans prix publié par l'API : tarifs publics des serveurs vCPU. Le mois affiché correspond à 730 heures. Le disque est facturé en plus.",
		Offers:  offers,
	}, nil
}

func hasPrefixAny(values []string, prefix string) bool {
	for _, value := range values {
		if strings.HasPrefix(strings.ToLower(value), prefix) {
			return true
		}
	}
	return false
}
