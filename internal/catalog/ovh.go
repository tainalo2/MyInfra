package catalog

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
)

var ovhRegions = []Location{
	{ID: "GRA", Label: "Gravelines"},
	{ID: "SBG", Label: "Strasbourg"},
	{ID: "RBX", Label: "Roubaix"},
	{ID: "WAW", Label: "Varsovie"},
	{ID: "DE", Label: "Francfort"},
	{ID: "UK", Label: "Londres"},
	{ID: "BHS", Label: "Beauharnois"},
}

func (c Client) ovh(ctx context.Context, location string) (Catalog, error) {
	base := c.OVHBase
	if base == "" {
		base = "https://eu.api.ovh.com/1.0"
	}
	body, err := get(ctx, c.http(), strings.TrimRight(base, "/")+"/order/catalog/public/cloud?ovhSubsidiary=FR", nil)
	if err != nil {
		return Catalog{}, err
	}
	return parseOVH(body, location)
}

type ovhAddon struct {
	PlanCode    string `json:"planCode"`
	InvoiceName string `json:"invoiceName"`
	Product     string `json:"product"`
	Pricings    []struct {
		Interval     int    `json:"interval"`
		IntervalUnit string `json:"intervalUnit"`
		Price        int64  `json:"price"`
		Quantity     struct {
			Min int  `json:"min"`
			Max *int `json:"max"`
		} `json:"quantity"`
	} `json:"pricings"`
	Blobs struct {
		Technical struct {
			CPU struct {
				Cores int `json:"cores"`
			} `json:"cpu"`
			Memory struct {
				Size float64 `json:"size"`
			} `json:"memory"`
			GPU struct {
				Number int    `json:"number"`
				Model  string `json:"model"`
			} `json:"gpu"`
			Storage struct {
				Disks []struct {
					Capacity float64 `json:"capacity"`
					Number   int     `json:"number"`
				} `json:"disks"`
			} `json:"storage"`
		} `json:"technical"`
	} `json:"blobs"`
}

func parseOVH(body []byte, location string) (Catalog, error) {
	var payload struct {
		Addons []ovhAddon `json:"addons"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return Catalog{}, err
	}
	monthly := map[string]string{}
	for _, addon := range payload.Addons {
		if addon.Product != "publiccloud-instance" || !strings.HasSuffix(addon.PlanCode, ".monthly.postpaid") {
			continue
		}
		if price := ovhPrice(addon, "month"); price != "" {
			monthly[addon.InvoiceName] = price
		}
	}
	storage := ovhVolumes(payload.Addons)
	chosen := pickLocation(ovhRegions, location, "GRA")
	offers := make([]Offer, 0)
	for _, addon := range payload.Addons {
		if !ovhInstance(addon.PlanCode, addon.Product) {
			continue
		}
		hourly := ovhPrice(addon, "hour")
		if hourly == "" {
			continue
		}
		cores := addon.Blobs.Technical.CPU.Cores
		if cores == 0 {
			continue
		}
		month := monthly[addon.InvoiceName]
		monthlyID := ""
		if month != "" {
			monthlyID = addon.InvoiceName + ".monthly.postpaid"
		} else {
			month = monthlyFromHourly(hourly)
		}
		gpus, model := ovhGPU(addon)
		offers = append(offers, Offer{
			ID: addon.PlanCode, MonthlyID: monthlyID, Name: addon.InvoiceName, Location: chosen,
			VCPU: cores, MemoryMB: int(addon.Blobs.Technical.Memory.Size * 1024),
			DiskGB: ovhDisk(addon), GPUs: gpus, GPU: model,
			Hourly: hourly, Monthly: month, Storage: storage,
		})
	}
	sort.Slice(offers, func(i, j int) bool { return offers[i].Name < offers[j].Name })
	return Catalog{
		Provider: "ovh", Currency: "EUR", Location: chosen, Locations: ovhRegions,
		Periods: []string{"hour", "month"},
		Note:    "OVH Public Cloud se facture à l'heure ou au mois. Les volumes sont en plus, au gigaoctet. Prix France, hors taxe.",
		Offers:  offers,
	}, nil
}

func ovhInstance(code, product string) bool {
	if product != "publiccloud-instance" || !strings.HasSuffix(code, ".consumption") {
		return false
	}
	if strings.Count(code, ".") != 1 || strings.HasPrefix(code, "win-") || strings.HasPrefix(code, "metal.") {
		return false
	}
	return true
}

func ovhPrice(addon ovhAddon, unit string) string {
	for _, price := range addon.Pricings {
		if price.IntervalUnit == unit && price.Price > 0 {
			return formatMoney(float64(price.Price) / 100000000)
		}
	}
	return ""
}

func ovhGPU(addon ovhAddon) (int, string) {
	card := addon.Blobs.Technical.GPU
	if card.Number <= 0 {
		return 0, ""
	}
	model := card.Model
	if model == "" {
		model = gpuModel(card.Number, addon.InvoiceName, addon.PlanCode)
	}
	return card.Number, model
}

func ovhDisk(addon ovhAddon) int {
	total := 0
	for _, disk := range addon.Blobs.Technical.Storage.Disks {
		count := disk.Number
		if count < 1 {
			count = 1
		}
		total += int(disk.Capacity) * count
	}
	return total
}

func ovhVolumes(addons []ovhAddon) []Storage {
	wanted := []struct {
		hour, month, label string
	}{
		{"volume.classic.consumption", "volume.classic.monthly.postpaid", "Volume Classic"},
		{"volume.high-speed.consumption", "volume.high-speed.monthly.postpaid", "Volume High Speed"},
		{"volume.high-speed-gen2.consumption", "volume.high-speed-gen2.monthly.postpaid", "Volume High Speed Gen2"},
	}
	byCode := map[string]ovhAddon{}
	for _, addon := range addons {
		byCode[addon.PlanCode] = addon
	}
	out := make([]Storage, 0, len(wanted))
	for _, item := range wanted {
		hour := byCode[item.hour]
		hourly := ovhPrice(hour, "hour")
		if hourly == "" {
			hourly = ovhPrice(hour, "none")
		}
		monthly := ovhPrice(byCode[item.month], "month")
		if hourly == "" && monthly == "" {
			continue
		}
		if monthly == "" {
			monthly = monthlyFromHourly(hourly)
		}
		if hourly == "" {
			value, _ := parseFloat(monthly)
			hourly = formatMoney(value / 730)
		}
		out = append(out, Storage{
			ID: item.hour, Label: item.label, MinGB: 1, MaxGB: 4000,
			HourlyPerGB: hourly, MonthlyPerGB: monthly,
		})
	}
	return out
}

type LiveFlavor struct {
	Name      string
	Type      string
	OSType    string
	Available bool
	VCPU      int
	RAM       int
	Disk      int
	Hourly    string
	Monthly   string
}

func MergeOVH(offers []Offer, flavors []LiveFlavor) []Offer {
	var storage []Storage
	byCode := map[string]Offer{}
	for _, offer := range offers {
		if storage == nil && len(offer.Storage) > 0 {
			storage = offer.Storage
		}
		for _, key := range []string{offer.ID, offer.MonthlyID, offer.Name} {
			key = strings.ToLower(strings.TrimSpace(key))
			if key != "" {
				byCode[key] = offer
			}
		}
	}
	out := make([]Offer, 0, len(flavors))
	seen := map[string]bool{}
	for _, flavor := range flavors {
		if !usableOVH(flavor) {
			continue
		}
		price, ok := byCode[strings.ToLower(flavor.Hourly)]
		if !ok {
			price, ok = byCode[strings.ToLower(flavor.Name)]
		}
		if !ok || price.Hourly == "" {
			continue
		}
		id := flavor.Hourly
		if id == "" {
			id = price.ID
		}
		if seen[strings.ToLower(id)] {
			continue
		}
		seen[strings.ToLower(id)] = true
		disk := price.DiskGB
		if flavor.Disk >= 10 {
			disk = flavor.Disk
		}
		memory := price.MemoryMB
		if flavor.RAM > 0 {
			memory = flavor.RAM * 1024
		}
		vcpu := flavor.VCPU
		if vcpu == 0 {
			vcpu = price.VCPU
		}
		name := flavor.Name
		if name == "" {
			name = price.Name
		}
		out = append(out, Offer{
			ID: id, MonthlyID: price.MonthlyID, Name: name, Location: price.Location,
			VCPU: vcpu, MemoryMB: memory, DiskGB: disk,
			Hourly: price.Hourly, Monthly: price.Monthly, Storage: storage,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func usableOVH(flavor LiveFlavor) bool {
	if !flavor.Available {
		return false
	}
	kind := strings.ToLower(flavor.Type + " " + flavor.OSType + " " + flavor.Name)
	if strings.Contains(kind, "baremetal") || strings.Contains(kind, "windows") || strings.Contains(kind, "metal") {
		return false
	}
	return flavor.Hourly != "" || flavor.Name != ""
}
