package catalog

import (
	"context"
	"encoding/json"
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

func (c Client) scaleway(ctx context.Context, token, location string) (Catalog, error) {
	base := c.ScalewayBase
	if base == "" {
		base = "https://api.scaleway.com"
	}
	zone := pickLocation(scalewayZones, location, "fr-par-1")
	body, err := get(ctx, c.http(), strings.TrimRight(base, "/")+"/instance/v1/zones/"+zone+"/products/servers", map[string]string{
		"X-Auth-Token": strings.TrimSpace(token),
	})
	if err != nil {
		return Catalog{}, err
	}
	return parseScaleway(body, zone)
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
	VolumesConstraint   scalewayBound            `json:"volumes_constraint"`
	PerVolumeConstraint map[string]scalewayBound `json:"per_volume_constraint"`
}

func parseScaleway(body []byte, zone string) (Catalog, error) {
	var payload struct {
		Servers map[string]scalewayProduct `json:"servers"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return Catalog{}, err
	}
	offers := make([]Offer, 0, len(payload.Servers))
	for name, item := range payload.Servers {
		if item.Baremetal || item.NCPUs == 0 || item.HourlyPrice <= 0 {
			continue
		}
		hourly := formatMoney(item.HourlyPrice)
		monthly := ""
		if item.MonthlyPrice > 0 {
			monthly = formatMoney(item.MonthlyPrice)
		} else {
			monthly = monthlyFromHourly(hourly)
		}
		disk := gbFromBytes(item.VolumesConstraint.MinSize)
		offer := Offer{
			ID: name, Name: name, Location: zone,
			VCPU: item.NCPUs, MemoryMB: int(item.RAM / (1024 * 1024)), DiskGB: disk,
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
	}, nil
}

func gbFromBytes(size int64) int {
	if size <= 0 {
		return 0
	}
	return int(size / 1000000000)
}
