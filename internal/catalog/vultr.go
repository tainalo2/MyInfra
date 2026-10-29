package catalog

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

func (c Client) vultr(ctx context.Context, token, location string) (Catalog, error) {
	base := c.VultrBase
	if base == "" {
		base = "https://api.vultr.com"
	}
	base = strings.TrimRight(base, "/")
	header := map[string]string{"Authorization": "Bearer " + strings.TrimSpace(token)}
	regions, err := collectPages(ctx, c, base+"/v2/regions?per_page=500", header)
	if err != nil {
		return Catalog{}, err
	}
	plans, err := collectPages(ctx, c, base+"/v2/plans?per_page=500", header)
	if err != nil {
		return Catalog{}, err
	}
	return parseVultr(regions, plans, location)
}

func collectPages(ctx context.Context, c Client, first string, header map[string]string) ([][]byte, error) {
	var pages [][]byte
	next := first
	for i := 0; i < 15 && next != ""; i++ {
		body, err := get(ctx, c.http(), next, header)
		if err != nil {
			return nil, err
		}
		pages = append(pages, body)
		next = pageNext(body)
	}
	return pages, nil
}

func pageNext(body []byte) string {
	var payload struct {
		Meta struct {
			Links struct {
				Next string `json:"next"`
			} `json:"links"`
		} `json:"meta"`
	}
	if json.Unmarshal(body, &payload) != nil {
		return ""
	}
	next := strings.TrimSpace(payload.Meta.Links.Next)
	if strings.HasPrefix(next, "https://") || strings.HasPrefix(next, "http://") {
		return next
	}
	return ""
}

type vultrPlan struct {
	ID        string   `json:"id"`
	VCPU      int      `json:"vcpu_count"`
	RAM       int      `json:"ram"`
	Disk      int      `json:"disk"`
	Monthly   float64  `json:"monthly_cost"`
	Hourly    float64  `json:"hourly_cost"`
	Type      string   `json:"type"`
	Locations []string `json:"locations"`
	GPU       string   `json:"gpu_brand"`
	GPUCount  int      `json:"gpu_count"`
	Costs     map[string]struct {
		Monthly float64 `json:"monthly_cost"`
		Hourly  float64 `json:"hourly_cost"`
	} `json:"location_cost"`
}

func parseVultr(regionPages, planPages [][]byte, location string) (Catalog, error) {
	var locations []Location
	seen := map[string]bool{}
	for _, page := range regionPages {
		var payload struct {
			Regions []struct {
				ID   string `json:"id"`
				City string `json:"city"`
			} `json:"regions"`
		}
		if json.Unmarshal(page, &payload) != nil {
			continue
		}
		for _, region := range payload.Regions {
			if region.ID == "" || seen[region.ID] {
				continue
			}
			seen[region.ID] = true
			label := region.City
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
	chosen := pickLocation(locations, location, "ams")
	offers := make([]Offer, 0)
	for _, page := range planPages {
		var payload struct {
			Plans []vultrPlan `json:"plans"`
		}
		if json.Unmarshal(page, &payload) != nil {
			continue
		}
		for _, plan := range payload.Plans {
			if !vultrUsable(plan, chosen) {
				continue
			}
			hourly := plan.Hourly
			monthly := plan.Monthly
			if cost, ok := plan.Costs[chosen]; ok && (cost.Hourly > 0 || cost.Monthly > 0) {
				hourly = cost.Hourly
				monthly = cost.Monthly
			}
			hourlyText := formatMoney(hourly)
			monthlyText := formatMoney(monthly)
			if hourlyText == "" && monthlyText == "" {
				continue
			}
			if monthlyText == "" {
				monthlyText = monthlyFromHourly(hourlyText)
			}
			count := vultrGPUs(plan)
			offers = append(offers, Offer{
				ID: plan.ID, Name: plan.ID, Location: chosen,
				VCPU: plan.VCPU, MemoryMB: plan.RAM, DiskGB: plan.Disk,
				GPUs: count, GPU: vultrModel(plan, count),
				Hourly: hourlyText, Monthly: monthlyText,
			})
		}
	}
	sort.Slice(offers, func(i, j int) bool { return offers[i].Name < offers[j].Name })
	return Catalog{
		Provider: "vultr", Currency: "USD", Location: chosen, Locations: locations,
		Periods: []string{"hour", "month"},
		Note:    "Vultr facture à l'heure, avec un plafond au mois. Prix en dollars.",
		Offers:  offers,
	}, nil
}

func vultrUsable(plan vultrPlan, location string) bool {
	if plan.ID == "" || plan.VCPU <= 0 || plan.Disk <= 0 {
		return false
	}
	kind := strings.ToLower(plan.Type)
	if kind == "vbm" {
		return false
	}
	if len(plan.Locations) == 0 {
		return true
	}
	for _, item := range plan.Locations {
		if item == location {
			return true
		}
	}
	return false
}

func vultrGPUs(plan vultrPlan) int {
	if plan.GPUCount > 0 {
		return plan.GPUCount
	}
	kind := strings.ToLower(plan.Type)
	if strings.Contains(kind, "gpu") || kind == "vcg" || (plan.GPU != "" && !strings.EqualFold(plan.GPU, "none")) {
		return 1
	}
	return 0
}

func vultrModel(plan vultrPlan, count int) string {
	model := gpuModel(count, plan.ID, plan.GPU)
	if model == "GPU" && plan.GPU != "" && !strings.EqualFold(plan.GPU, "none") && !strings.EqualFold(plan.GPU, "nvidia") {
		return plan.GPU
	}
	return model
}
