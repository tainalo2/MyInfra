package catalog

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"myinfra/internal/exo"
)

var exoscaleLabels = map[string]string{
	"ch-gva-2": "Genève, CH",
	"ch-dk-2":  "Zurich, CH",
	"de-fra-1": "Francfort, DE",
	"de-muc-1": "Munich, DE",
	"at-vie-1": "Vienne, AT",
	"at-vie-2": "Vienne 2, AT",
	"bg-sof-1": "Sofia, BG",
}

func (c Client) exoscale(ctx context.Context, key, secret, location string) (Catalog, error) {
	signed := func(zone, path string) ([]byte, error) {
		url := exo.Host(c.ExoscaleBase, zone) + path
		header, err := exo.Header("GET", url, nil, key, secret)
		if err != nil {
			return nil, err
		}
		return get(ctx, c.http(), url, header)
	}
	zonesRaw, err := signed("", "/v2/zone")
	if err != nil {
		return Catalog{}, err
	}
	locations := parseExoscaleZones(zonesRaw)
	if len(locations) == 0 {
		return Catalog{}, fmt.Errorf("Aucune offre disponible.")
	}
	chosen := pickLocation(locations, location, "ch-gva-2")
	typesRaw, err := signed(chosen, "/v2/instance-type")
	if err != nil {
		return Catalog{}, err
	}
	base := c.ExoscalePriceBase
	if base == "" {
		base = "https://portal.exoscale.com"
	}
	pricesRaw, err := get(ctx, c.http(), strings.TrimRight(base, "/")+"/api/pricing/opencompute", nil)
	if err != nil {
		return Catalog{}, err
	}
	return parseExoscale(typesRaw, pricesRaw, locations, chosen)
}

func parseExoscaleZones(raw []byte) []Location {
	var payload struct {
		Zones []struct {
			Name string `json:"name"`
		} `json:"zones"`
	}
	if json.Unmarshal(raw, &payload) != nil {
		return nil
	}
	var locations []Location
	for _, zone := range payload.Zones {
		if zone.Name == "" {
			continue
		}
		label := exoscaleLabels[zone.Name]
		if label == "" {
			label = strings.ToUpper(zone.Name)
		}
		locations = append(locations, Location{ID: zone.Name, Label: label})
	}
	sort.Slice(locations, func(i, j int) bool { return locations[i].Label < locations[j].Label })
	return locations
}

func parseExoscale(typesRaw, pricesRaw []byte, locations []Location, chosen string) (Catalog, error) {
	var types struct {
		Items []struct {
			ID         string `json:"id"`
			Family     string `json:"family"`
			Size       string `json:"size"`
			CPUs       int    `json:"cpus"`
			GPUs       int    `json:"gpus"`
			Memory     int64  `json:"memory"`
			Authorized *bool  `json:"authorized"`
		} `json:"instance-types"`
	}
	if err := json.Unmarshal(typesRaw, &types); err != nil {
		return Catalog{}, fmt.Errorf("Le provider ne répond pas.")
	}
	var prices map[string]map[string]string
	if err := json.Unmarshal(pricesRaw, &prices); err != nil {
		return Catalog{}, fmt.Errorf("Le provider ne répond pas.")
	}
	eur := prices["eur"]
	volume := money(eur["volume"])
	var storage []Storage
	if volume != "" {
		storage = []Storage{{
			ID: "boot", Label: "Disque", MinGB: 10, MaxGB: 800, Required: true,
			HourlyPerGB: volume, MonthlyPerGB: monthlyFromHourly(volume),
		}}
	}
	offers := make([]Offer, 0)
	for _, item := range types.Items {
		if item.ID == "" || item.CPUs <= 0 || (item.Authorized != nil && !*item.Authorized) {
			continue
		}
		family := strings.ToLower(item.Family)
		if item.GPUs <= 0 && family != "standard" && family != "cpu" && family != "memory" {
			continue
		}
		key := "running_" + strings.ReplaceAll(strings.ToLower(item.Size), "-", "_")
		if family != "standard" {
			key = "running_" + family + "_" + strings.ReplaceAll(strings.ToLower(item.Size), "-", "_")
		}
		hourly := money(eur[key])
		if hourly == "" || hourly == "0" {
			continue
		}
		memory := item.Memory
		if memory > 1<<20 {
			memory /= 1024 * 1024
		}
		offers = append(offers, Offer{
			ID: item.ID, Name: exoscaleName(family, item.Size), Location: chosen,
			VCPU: item.CPUs, MemoryMB: int(memory), GPUs: item.GPUs, GPU: exoscaleGPU(family, item.GPUs),
			Hourly: hourly, Monthly: monthlyFromHourly(hourly), Storage: storage,
		})
	}
	if len(offers) == 0 {
		return Catalog{}, fmt.Errorf("Aucune offre disponible.")
	}
	sort.Slice(offers, func(i, j int) bool {
		a, _ := strconv.ParseFloat(offers[i].Hourly, 64)
		b, _ := strconv.ParseFloat(offers[j].Hourly, 64)
		if a != b {
			return a < b
		}
		return offers[i].Name < offers[j].Name
	})
	return Catalog{
		Provider: "exoscale", Currency: "EUR", Location: chosen, Locations: locations,
		Periods: []string{"hour", "month"},
		Note:    "Exoscale facture à l'heure. Le mois affiché correspond à 730 heures. Le disque est facturé en plus.",
		Offers:  offers,
	}, nil
}

func exoscaleName(family, size string) string {
	label := strings.ReplaceAll(size, "-", " ")
	if label != "" {
		label = strings.ToUpper(label[:1]) + label[1:]
	}
	switch family {
	case "cpu":
		return "CPU " + label
	case "memory":
		return "Mémoire " + label
	default:
		if strings.Contains(family, "gpu") {
			return exoscaleGPU(family, 1) + " " + label
		}
		return "Standard " + label
	}
}

func exoscaleGPU(family string, count int) string {
	if count <= 0 {
		return ""
	}
	switch strings.ToLower(family) {
	case "gpu3":
		return "A40"
	case "gpua30":
		return "A30"
	default:
		return gpuModel(count, family)
	}
}
