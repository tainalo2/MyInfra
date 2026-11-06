package catalog

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

var scalewayZones = []Location{
	{ID: "fr-par-1", Label: "Paris 1"},
	{ID: "fr-par-2", Label: "Paris 2"},
	{ID: "fr-par-3", Label: "Paris 3"},
	{ID: "nl-ams-1", Label: "Amsterdam 1"},
	{ID: "nl-ams-2", Label: "Amsterdam 2"},
	{ID: "nl-ams-3", Label: "Amsterdam 3"},
	{ID: "pl-waw-1", Label: "Varsovie 1"},
	{ID: "pl-waw-2", Label: "Varsovie 2"},
	{ID: "pl-waw-3", Label: "Varsovie 3"},
}

// scalewayPage is the most either list gives at a time; its default is less
// than a zone's offers.
const scalewayPage = 100

func (c Client) scaleway(ctx context.Context, token, location string) (Catalog, error) {
	base := c.ScalewayBase
	if base == "" {
		base = "https://api.scaleway.com"
	}
	base = strings.TrimRight(base, "/")
	zone := pickLocation(scalewayZones, location, "fr-par-1")
	servers, err := c.scalewayServers(ctx, base, token, zone)
	if err != nil {
		return Catalog{}, err
	}
	// The Instance API answers 0 for its prices: the public catalogue has them.
	var prices map[string]float64
	for _, item := range servers {
		if item.HourlyPrice <= 0 {
			if prices, err = c.scalewayPrices(ctx, base, zone); err != nil {
				return Catalog{}, err
			}
			break
		}
	}
	cat := scalewayCatalog(servers, prices, zone)
	if len(servers) > 0 && len(cat.Offers) == 0 {
		return Catalog{}, fmt.Errorf("Scaleway ne donne aucun prix pour cette zone.")
	}
	return cat, nil
}

func (c Client) scalewayServers(ctx context.Context, base, token, zone string) (map[string]scalewayProduct, error) {
	servers := map[string]scalewayProduct{}
	for page := 1; ; page++ {
		url := fmt.Sprintf("%s/instance/v1/zones/%s/products/servers?per_page=%d&page=%d", base, zone, scalewayPage, page)
		body, err := get(ctx, c.http(), url, map[string]string{"X-Auth-Token": strings.TrimSpace(token)})
		if err != nil {
			return nil, err
		}
		found, err := parseScalewayServers(body)
		if err != nil {
			return nil, err
		}
		for name, item := range found {
			servers[name] = item
		}
		if len(found) < scalewayPage {
			return servers, nil
		}
	}
}

func parseScalewayServers(body []byte) (map[string]scalewayProduct, error) {
	var payload struct {
		Servers map[string]scalewayProduct `json:"servers"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	return payload.Servers, nil
}

type scalewayPrice struct {
	Product string `json:"product"`
	Price   struct {
		Retail struct {
			Units int64 `json:"units"`
			Nanos int64 `json:"nanos"`
		} `json:"retail_price"`
	} `json:"price"`
	Measure struct {
		Unit string  `json:"unit"`
		Size float64 `json:"size"`
	} `json:"unit_of_measure"`
}

// hourly is the price of an hour: some offers are priced by the minute.
func (p scalewayPrice) hourly() float64 {
	amount := float64(p.Price.Retail.Units) + float64(p.Price.Retail.Nanos)/1e9
	if p.Measure.Size > 0 {
		amount /= p.Measure.Size
	}
	switch p.Measure.Unit {
	case "hour":
		return amount
	case "minute":
		return amount * 60
	}
	return 0
}

// scalewayPrices reads the hourly price of a zone's offers, by name. The
// catalogue is public: the account's token stays out of it.
func (c Client) scalewayPrices(ctx context.Context, base, zone string) (map[string]float64, error) {
	prices := map[string]float64{}
	for page := 1; ; page++ {
		url := fmt.Sprintf("%s/product-catalog/v2alpha1/public-catalog/products?product_types=instance&zone=%s&page_size=%d&page=%d", base, zone, scalewayPage, page)
		body, err := get(ctx, c.http(), url, nil)
		if err != nil {
			return nil, err
		}
		var payload struct {
			Products []scalewayPrice `json:"products"`
		}
		if err := json.Unmarshal(body, &payload); err != nil {
			return nil, err
		}
		for _, item := range payload.Products {
			prices[item.Product] = item.hourly()
		}
		if len(payload.Products) < scalewayPage {
			return prices, nil
		}
	}
}

type scalewayBound struct {
	MinSize int64 `json:"min_size"`
	MaxSize int64 `json:"max_size"`
}

type scalewayProduct struct {
	HourlyPrice         float64                  `json:"hourly_price"`
	MonthlyPrice        float64                  `json:"monthly_price"`
	NCPUs               int                      `json:"ncpus"`
	RAM                 int64                    `json:"ram"`
	Arch                string                   `json:"arch"`
	Baremetal           bool                     `json:"baremetal"`
	GPU                 json.RawMessage          `json:"gpu"`
	VolumesConstraint   scalewayBound            `json:"volumes_constraint"`
	PerVolumeConstraint map[string]scalewayBound `json:"per_volume_constraint"`
}

func scalewayCatalog(servers map[string]scalewayProduct, prices map[string]float64, zone string) Catalog {
	offers := make([]Offer, 0, len(servers))
	for name, item := range servers {
		price := item.HourlyPrice
		if price <= 0 {
			price = prices[name]
		}
		if item.Baremetal || item.NCPUs == 0 || price <= 0 {
			continue
		}
		hourly := formatMoney(price)
		monthly := ""
		if item.MonthlyPrice > 0 {
			monthly = formatMoney(item.MonthlyPrice)
		} else {
			monthly = monthlyFromHourly(hourly)
		}
		disk := gbFromBytes(item.VolumesConstraint.MinSize)
		gpus := scalewayGPUs(item.GPU)
		offer := Offer{
			ID: name, Name: name, Location: zone,
			VCPU: item.NCPUs, MemoryMB: int(item.RAM / (1024 * 1024)), DiskGB: disk,
			GPUs: gpus, GPU: gpuModel(gpus, name),
			Hourly: hourly, Monthly: monthly,
		}
		if item.VolumesConstraint.MaxSize > item.VolumesConstraint.MinSize && item.VolumesConstraint.MaxSize > 0 {
			offer.Storage = append(offer.Storage, Storage{
				ID: "boot", Label: "Disque", Required: true,
				MinGB: gbFromBytes(item.VolumesConstraint.MinSize),
				MaxGB: gbFromBytes(item.VolumesConstraint.MaxSize),
			})
		}
		if extra, ok := item.PerVolumeConstraint["l_ssd"]; ok && extra.MaxSize > extra.MinSize && extra.MaxSize > 0 {
			offer.Storage = append(offer.Storage, Storage{
				ID: "l_ssd", Label: "SSD local",
				MinGB: gbFromBytes(extra.MinSize), MaxGB: gbFromBytes(extra.MaxSize),
			})
		}
		offers = append(offers, offer)
	}
	sort.Slice(offers, func(i, j int) bool { return offers[i].Name < offers[j].Name })
	return Catalog{
		Provider: "scaleway", Currency: "EUR", Location: zone, Locations: scalewayZones,
		Periods: []string{"hour", "month"},
		Note:    "Scaleway facture à l'heure. Le mois affiché correspond à 730 heures.",
		Offers:  offers,
	}
}

func scalewayGPUs(raw json.RawMessage) int {
	if len(raw) == 0 || string(raw) == "null" {
		return 0
	}
	var count int
	if json.Unmarshal(raw, &count) == nil {
		return count
	}
	var object struct {
		Count int `json:"count"`
	}
	if json.Unmarshal(raw, &object) == nil {
		return object.Count
	}
	return 0
}

func gbFromBytes(size int64) int {
	if size <= 0 {
		return 0
	}
	return int(size / 1000000000)
}
