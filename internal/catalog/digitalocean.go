package catalog

import (
	"context"
	"encoding/json"
	"net/url"
	"sort"
	"strings"
)

const digitalOceanVolumeMonthly = 0.10

func (c Client) digitalOcean(ctx context.Context, token, location string) (Catalog, error) {
	base := c.DigitalOceanBase
	if base == "" {
		base = "https://api.digitalocean.com"
	}
	base = strings.TrimRight(base, "/")
	header := map[string]string{"Authorization": "Bearer " + strings.TrimSpace(token)}
	var sizes []doSize
	next := base + "/v2/sizes?per_page=200"
	for page := 0; next != "" && page < 10; page++ {
		if page > 0 && !sameHost(base, next) {
			break
		}
		body, err := get(ctx, c.http(), next, header)
		if err != nil {
			return Catalog{}, err
		}
		var payload struct {
			Sizes []doSize `json:"sizes"`
			Links struct {
				Pages struct {
					Next string `json:"next"`
				} `json:"pages"`
			} `json:"links"`
		}
		if err := json.Unmarshal(body, &payload); err != nil {
			return Catalog{}, err
		}
		sizes = append(sizes, payload.Sizes...)
		next = payload.Links.Pages.Next
	}
	regionsBody, err := get(ctx, c.http(), base+"/v2/regions?per_page=200", header)
	if err != nil {
		return Catalog{}, err
	}
	return parseDigitalOcean(sizes, regionsBody, location)
}

type doSize struct {
	Slug         string   `json:"slug"`
	Available    bool     `json:"available"`
	Vcpus        int      `json:"vcpus"`
	Memory       int      `json:"memory"`
	Disk         int      `json:"disk"`
	PriceHourly  float64  `json:"price_hourly"`
	PriceMonthly float64  `json:"price_monthly"`
	Regions      []string `json:"regions"`
	GPU          *struct {
		Count int    `json:"count"`
		Model string `json:"model"`
	} `json:"gpu_info"`
}

func parseDigitalOcean(sizes []doSize, regionsBody []byte, location string) (Catalog, error) {
	var regions struct {
		Regions []struct {
			Slug      string `json:"slug"`
			Name      string `json:"name"`
			Available bool   `json:"available"`
		} `json:"regions"`
	}
	if err := json.Unmarshal(regionsBody, &regions); err != nil {
		return Catalog{}, err
	}
	locations := make([]Location, 0, len(regions.Regions))
	for _, region := range regions.Regions {
		if !region.Available || region.Slug == "" {
			continue
		}
		label := region.Name
		if label == "" {
			label = region.Slug
		}
		locations = append(locations, Location{ID: region.Slug, Label: label})
	}
	sort.Slice(locations, func(i, j int) bool { return locations[i].Label < locations[j].Label })
	if len(locations) == 0 {
		return Catalog{}, errEmpty("Aucune région.")
	}
	chosen := pickLocation(locations, location, "ams3")
	volume := digitalOceanVolume()
	offers := make([]Offer, 0, len(sizes))
	for _, size := range sizes {
		if !size.Available || size.Slug == "" || size.Vcpus <= 0 || size.Memory <= 0 {
			continue
		}
		if size.PriceHourly <= 0 && size.PriceMonthly <= 0 {
			continue
		}
		if !contains(size.Regions, chosen) {
			continue
		}
		hourly := formatMoney(size.PriceHourly)
		monthly := formatMoney(size.PriceMonthly)
		if hourly == "" || hourly == "0" {
			hourly = ""
		}
		if monthly == "" || monthly == "0" {
			monthly = monthlyFromHourly(hourly)
		}
		if hourly == "" && monthly != "" {
			hourly = hourlyFromMonthly(monthly, 672)
		}
		gpus, model := digitalOceanGPU(size)
		offers = append(offers, Offer{
			ID: size.Slug, Name: size.Slug, Location: chosen,
			VCPU: size.Vcpus, MemoryMB: size.Memory, DiskGB: size.Disk,
			GPUs: gpus, GPU: model,
			Hourly: hourly, Monthly: monthly, Storage: []Storage{volume},
		})
	}
	sort.Slice(offers, func(i, j int) bool {
		if offers[i].VCPU != offers[j].VCPU {
			return offers[i].VCPU < offers[j].VCPU
		}
		if offers[i].MemoryMB != offers[j].MemoryMB {
			return offers[i].MemoryMB < offers[j].MemoryMB
		}
		return offers[i].Name < offers[j].Name
	})
	if len(offers) == 0 {
		return Catalog{}, errEmpty("Aucune offre pour ce lieu.")
	}
	return Catalog{
		Provider: "digitalocean", Currency: "USD", Location: chosen, Locations: locations,
		Periods: []string{"hour", "month"},
		Note:    "DigitalOcean facture à la seconde, avec un plafond au mois pour les offres groupées. Le volume coûte 0,10 $ par Gio et par mois. Prix en dollars.",
		Offers:  offers,
	}, nil
}

func digitalOceanGPU(size doSize) (int, string) {
	if size.GPU != nil && size.GPU.Count > 0 {
		model := size.GPU.Model
		if model == "" {
			model = gpuModel(size.GPU.Count, size.Slug)
		}
		return size.GPU.Count, model
	}
	if strings.Contains(strings.ToLower(size.Slug), "gpu") {
		return 1, gpuModel(1, size.Slug)
	}
	return 0, ""
}

func digitalOceanVolume() Storage {
	return Storage{
		ID: "volume", Label: "Volume", MinGB: 1, MaxGB: 16384,
		MonthlyPerGB: formatMoney(digitalOceanVolumeMonthly),
		HourlyPerGB:  hourlyFromMonthly(formatMoney(digitalOceanVolumeMonthly), 672),
	}
}

func hourlyFromMonthly(monthly string, hours float64) string {
	value, err := parseFloat(monthly)
	if err != nil || value <= 0 || hours <= 0 {
		return ""
	}
	return formatMoney(value / hours)
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func sameHost(base, next string) bool {
	left, err := url.Parse(base)
	if err != nil {
		return false
	}
	right, err := url.Parse(next)
	if err != nil {
		return false
	}
	return left.Host != "" && left.Host == right.Host
}

type errEmpty string

func (e errEmpty) Error() string { return string(e) }
