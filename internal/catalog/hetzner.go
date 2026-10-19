package catalog

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
)

func (c Client) hetzner(ctx context.Context, token, location string) (Catalog, error) {
	base := c.HetznerBase
	if base == "" {
		base = "https://api.hetzner.cloud"
	}
	base = strings.TrimRight(base, "/")
	header := map[string]string{"Authorization": "Bearer " + strings.TrimSpace(token)}
	typesBody, err := get(ctx, c.http(), base+"/v1/server_types", header)
	if err != nil {
		return Catalog{}, err
	}
	priceBody, err := get(ctx, c.http(), base+"/v1/pricing", header)
	if err != nil {
		return Catalog{}, err
	}
	return parseHetzner(typesBody, priceBody, location)
}

type hetznerMoney struct {
	Net   string `json:"net"`
	Gross string `json:"gross"`
}

type hetznerSite struct {
	Name        string `json:"name"`
	Deprecation any    `json:"deprecation"`
}

type hetznerTypes struct {
	ServerTypes []struct {
		Name       string        `json:"name"`
		Cores      int           `json:"cores"`
		Memory     float64       `json:"memory"`
		Disk       float64       `json:"disk"`
		Deprecated bool          `json:"deprecated"`
		Locations  []hetznerSite `json:"locations"`
		Prices     []struct {
			Location     string       `json:"location"`
			PriceHourly  hetznerMoney `json:"price_hourly"`
			PriceMonthly hetznerMoney `json:"price_monthly"`
		} `json:"prices"`
	} `json:"server_types"`
}

type hetznerPricing struct {
	Pricing struct {
		Volume struct {
			PricePerGBMonthly hetznerMoney `json:"price_per_gb_monthly"`
			PricePerGBHourly  hetznerMoney `json:"price_per_gb_hourly"`
		} `json:"volume"`
	} `json:"pricing"`
}

func parseHetzner(typesBody, priceBody []byte, location string) (Catalog, error) {
	var types hetznerTypes
	if err := json.Unmarshal(typesBody, &types); err != nil {
		return Catalog{}, err
	}
	var pricing hetznerPricing
	_ = json.Unmarshal(priceBody, &pricing)
	seen := map[string]bool{}
	var locations []Location
	for _, item := range types.ServerTypes {
		if item.Deprecated {
			continue
		}
		for _, price := range item.Prices {
			if price.Location == "" || seen[price.Location] || !hetznerOpen(item.Locations, price.Location) {
				continue
			}
			seen[price.Location] = true
			locations = append(locations, Location{ID: price.Location, Label: hetznerLocation(price.Location)})
		}
	}
	chosen := pickLocation(locations, location, "fsn1")
	volume := hetznerVolume(pricing)
	offers := make([]Offer, 0)
	for _, item := range types.ServerTypes {
		if item.Deprecated {
			continue
		}
		for _, price := range item.Prices {
			if price.Location != chosen || !hetznerOpen(item.Locations, price.Location) {
				continue
			}
			hourly := money(price.PriceHourly.Net)
			monthly := money(price.PriceMonthly.Net)
			if hourly == "" && monthly == "" {
				continue
			}
			if monthly == "" {
				monthly = monthlyFromHourly(hourly)
			}
			offer := Offer{
				ID: strings.ToLower(item.Name), Name: strings.ToUpper(item.Name), Location: chosen,
				VCPU: item.Cores, MemoryMB: int(item.Memory * 1024), DiskGB: int(item.Disk),
				Hourly: hourly, Monthly: monthly,
			}
			if volume.ID != "" {
				offer.Storage = []Storage{volume}
			}
			offers = append(offers, offer)
		}
	}
	sort.Slice(offers, func(i, j int) bool { return offers[i].Name < offers[j].Name })
	return Catalog{
		Provider: "hetzner", Currency: "EUR", Location: chosen, Locations: locations,
		Periods: []string{"hour", "month"},
		Note:    "Hetzner facture à l'heure, avec un plafond au mois. Le volume est un disque en plus, au gigaoctet.",
		Offers:  offers,
	}, nil
}

func hetznerOpen(places []hetznerSite, location string) bool {
	if len(places) == 0 {
		return true
	}
	for _, place := range places {
		if place.Name == location && place.Deprecation == nil {
			return true
		}
	}
	return false
}

func hetznerVolume(pricing hetznerPricing) Storage {
	monthly := money(pricing.Pricing.Volume.PricePerGBMonthly.Net)
	hourly := money(pricing.Pricing.Volume.PricePerGBHourly.Net)
	if hourly == "" && monthly != "" {
		value, _ := parseFloat(monthly)
		hourly = formatMoney(value / 730)
	}
	if monthly == "" && hourly != "" {
		monthly = monthlyFromHourly(hourly)
	}
	if monthly == "" && hourly == "" {
		return Storage{}
	}
	return Storage{
		ID: "volume", Label: "Volume", MinGB: 10, MaxGB: 10240,
		HourlyPerGB: hourly, MonthlyPerGB: monthly,
	}
}

func hetznerLocation(id string) string {
	switch id {
	case "fsn1":
		return "Falkenstein"
	case "nbg1":
		return "Nuremberg"
	case "hel1":
		return "Helsinki"
	case "ash":
		return "Ashburn"
	case "hil":
		return "Hillsboro"
	case "sin":
		return "Singapour"
	default:
		return id
	}
}
