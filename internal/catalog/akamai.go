package catalog

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

func (c Client) akamai(ctx context.Context, token, location string) (Catalog, error) {
	base := c.AkamaiBase
	if base == "" {
		base = "https://api.linode.com"
	}
	base = strings.TrimRight(base, "/")
	header := map[string]string{"Authorization": "Bearer " + strings.TrimSpace(token)}
	regions, err := linodePages(ctx, c, base+"/v4/regions", header)
	if err != nil {
		return Catalog{}, err
	}
	types, err := linodePages(ctx, c, base+"/v4/linode/types", header)
	if err != nil {
		return Catalog{}, err
	}
	return parseAkamai(regions, types, location)
}

func linodePages(ctx context.Context, c Client, path string, header map[string]string) ([][]byte, error) {
	var pages [][]byte
	for page := 1; page <= 8; page++ {
		body, err := get(ctx, c.http(), fmt.Sprintf("%s?page_size=100&page=%d", path, page), header)
		if err != nil {
			return nil, err
		}
		pages = append(pages, body)
		var meta struct {
			Page  int `json:"page"`
			Pages int `json:"pages"`
		}
		if json.Unmarshal(body, &meta) != nil || meta.Pages <= page || meta.Pages == 0 {
			break
		}
	}
	return pages, nil
}

func parseAkamai(regionPages, typePages [][]byte, location string) (Catalog, error) {
	var locations []Location
	seen := map[string]bool{}
	for _, page := range regionPages {
		var payload struct {
			Data []struct {
				ID           string   `json:"id"`
				Label        string   `json:"label"`
				Capabilities []string `json:"capabilities"`
			} `json:"data"`
		}
		if json.Unmarshal(page, &payload) != nil {
			continue
		}
		for _, region := range payload.Data {
			if region.ID == "" || seen[region.ID] || !linodeCapable(region.Capabilities) {
				continue
			}
			seen[region.ID] = true
			label := region.Label
			if label == "" {
				label = region.ID
			}
			locations = append(locations, Location{ID: region.ID, Label: label})
		}
	}
	sort.Slice(locations, func(i, j int) bool { return locations[i].Label < locations[j].Label })
	if len(locations) == 0 {
		return Catalog{}, fmt.Errorf("Aucune offre disponible.")
	}
	chosen := pickLocation(locations, location, "fr-par")
	offers := make([]Offer, 0)
	for _, page := range typePages {
		var payload struct {
			Data []struct {
				ID           string      `json:"id"`
				Label        string      `json:"label"`
				Class        string      `json:"class"`
				Disk         int         `json:"disk"`
				Memory       int         `json:"memory"`
				VCPUs        int         `json:"vcpus"`
				GPUs         int         `json:"gpus"`
				Price        akamaiPrice `json:"price"`
				RegionPrices []struct {
					ID      string  `json:"id"`
					Hourly  float64 `json:"hourly"`
					Monthly float64 `json:"monthly"`
				} `json:"region_prices"`
			} `json:"data"`
		}
		if json.Unmarshal(page, &payload) != nil {
			continue
		}
		for _, item := range payload.Data {
			class := strings.ToLower(item.Class)
			if item.ID == "" || item.VCPUs <= 0 || item.GPUs > 0 || class == "gpu" || class == "metal" {
				continue
			}
			hourly := item.Price.Hourly
			monthly := item.Price.Monthly
			for _, region := range item.RegionPrices {
				if region.ID == chosen && (region.Hourly > 0 || region.Monthly > 0) {
					hourly = region.Hourly
					monthly = region.Monthly
				}
			}
			hourlyText := formatMoney(hourly)
			monthlyText := formatMoney(monthly)
			if hourlyText == "" && monthlyText == "" {
				continue
			}
			if monthlyText == "" {
				monthlyText = monthlyFromHourly(hourlyText)
			}
			name := item.Label
			if name == "" {
				name = item.ID
			}
			disk := item.Disk / 1024
			if item.Disk > 0 && disk == 0 {
				disk = 1
			}
			offers = append(offers, Offer{
				ID: item.ID, Name: name, Location: chosen,
				VCPU: item.VCPUs, MemoryMB: item.Memory, DiskGB: disk,
				Hourly: hourlyText, Monthly: monthlyText,
			})
		}
	}
	sort.Slice(offers, func(i, j int) bool { return offers[i].Name < offers[j].Name })
	return Catalog{
		Provider: "akamai", Currency: "USD", Location: chosen, Locations: locations,
		Periods: []string{"hour", "month"},
		Note:    "Akamai Connected Cloud facture à l'heure, avec un plafond au mois. Prix en dollars.",
		Offers:  offers,
	}, nil
}

type akamaiPrice struct {
	Hourly  float64 `json:"hourly"`
	Monthly float64 `json:"monthly"`
}

func linodeCapable(values []string) bool {
	if len(values) == 0 {
		return true
	}
	linodes, metadata := false, false
	for _, value := range values {
		switch strings.ToLower(value) {
		case "linodes":
			linodes = true
		case "metadata":
			metadata = true
		}
	}
	return linodes && metadata
}
