package catalog

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

func (c Client) upcloud(ctx context.Context, token, location string) (Catalog, error) {
	base := c.UpCloudBase
	if base == "" {
		base = "https://api.upcloud.com"
	}
	base = strings.TrimRight(base, "/")
	header := map[string]string{"Authorization": "Bearer " + strings.TrimSpace(token)}
	plans, err := get(ctx, c.http(), base+"/1.3/plan", header)
	if err != nil {
		return Catalog{}, err
	}
	zones, err := get(ctx, c.http(), base+"/1.3/zone", header)
	if err != nil {
		return Catalog{}, err
	}
	prices, err := get(ctx, c.http(), base+"/1.3/price", header)
	if err != nil {
		return Catalog{}, err
	}
	return parseUpCloud(plans, zones, prices, location)
}

func parseUpCloud(plansBody, zonesBody, pricesBody []byte, location string) (Catalog, error) {
	zones := upcloudZones(zonesBody)
	if len(zones) == 0 {
		return Catalog{}, fmt.Errorf("Aucune offre disponible.")
	}
	chosen := pickLocation(zones, location, "de-fra1")
	prices := upcloudPrices(pricesBody)
	var payload struct {
		Plans struct {
			Plan []struct {
				Name    string `json:"name"`
				Cores   int    `json:"core_number"`
				Memory  int    `json:"memory_amount"`
				Storage int    `json:"storage_size"`
				GPU     int    `json:"gpu_amount"`
			} `json:"plan"`
		} `json:"plans"`
	}
	if err := json.Unmarshal(plansBody, &payload); err != nil {
		return Catalog{}, err
	}
	offers := make([]Offer, 0)
	for _, plan := range payload.Plans.Plan {
		if plan.Name == "" || plan.Cores <= 0 || plan.GPU > 0 || plan.Storage <= 0 || strings.Contains(strings.ToUpper(plan.Name), "GPU") {
			continue
		}
		hourly := prices[chosen][plan.Name]
		if hourly == "" {
			continue
		}
		offers = append(offers, Offer{
			ID: plan.Name, Name: plan.Name, Location: chosen,
			VCPU: plan.Cores, MemoryMB: plan.Memory, DiskGB: plan.Storage,
			Hourly: hourly, Monthly: monthlyFromHourly(hourly),
		})
	}
	sort.Slice(offers, func(i, j int) bool { return offers[i].Name < offers[j].Name })
	return Catalog{
		Provider: "upcloud", Currency: "EUR", Location: chosen, Locations: zones,
		Periods: []string{"hour", "month"},
		Note:    "UpCloud facture à l'heure. Le mois affiché correspond à 730 heures.",
		Offers:  offers,
	}, nil
}

func upcloudZones(body []byte) []Location {
	var payload struct {
		Zones struct {
			Zone []struct {
				ID          string `json:"id"`
				Description string `json:"description"`
				Public      string `json:"public"`
			} `json:"zone"`
		} `json:"zones"`
	}
	if json.Unmarshal(body, &payload) != nil {
		return nil
	}
	locations := make([]Location, 0, len(payload.Zones.Zone))
	for _, zone := range payload.Zones.Zone {
		if zone.ID == "" || strings.EqualFold(zone.Public, "no") {
			continue
		}
		label := zone.Description
		if label == "" {
			label = zone.ID
		}
		locations = append(locations, Location{ID: zone.ID, Label: label})
	}
	sort.Slice(locations, func(i, j int) bool { return locations[i].Label < locations[j].Label })
	return locations
}

func upcloudPrices(body []byte) map[string]map[string]string {
	var payload struct {
		Prices struct {
			Zone []json.RawMessage `json:"zone"`
		} `json:"prices"`
	}
	out := map[string]map[string]string{}
	if json.Unmarshal(body, &payload) != nil {
		return out
	}
	for _, raw := range payload.Prices.Zone {
		var fields map[string]json.RawMessage
		if json.Unmarshal(raw, &fields) != nil {
			continue
		}
		var name string
		if json.Unmarshal(fields["name"], &name) != nil || name == "" {
			continue
		}
		plans := map[string]string{}
		for key, value := range fields {
			if !strings.HasPrefix(key, "server_plan_") {
				continue
			}
			var price struct {
				Amount float64 `json:"amount"`
				Price  float64 `json:"price"` // centimes d'euro
			}
			if json.Unmarshal(value, &price) != nil || price.Price <= 0 {
				continue
			}
			hourly := price.Price / 100
			if price.Amount > 0 {
				hourly = price.Price / price.Amount / 100
			}
			if text := formatMoney(hourly); text != "" && text != "0" {
				plans[strings.TrimPrefix(key, "server_plan_")] = text
			}
		}
		out[name] = plans
	}
	return out
}
