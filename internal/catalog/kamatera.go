package catalog

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

const kamateraAPI = "https://cloudcli.cloudwm.com"

func kamateraHeader(id, secret string) map[string]string {
	return map[string]string{
		"AuthClientId": strings.TrimSpace(id),
		"AuthSecret":   strings.TrimSpace(secret),
		"Accept":       "application/json",
	}
}

type kamateraPreset struct {
	Type   string
	Cores  int
	RAM    int
	DiskGB int
}

var kamateraPresets = []kamateraPreset{
	{"A", 1, 1024, 20}, {"A", 1, 2048, 30}, {"A", 2, 4096, 40}, {"A", 2, 8192, 60},
	{"A", 4, 8192, 80}, {"A", 4, 16384, 100}, {"A", 8, 16384, 150}, {"A", 8, 32768, 200},
	{"B", 1, 2048, 30}, {"B", 2, 4096, 40}, {"B", 4, 8192, 80}, {"B", 8, 16384, 150},
}

var kamateraLabels = map[string]string{
	"AS": "Hong Kong, Chine", "AS-SG": "Singapour", "AS-TY": "Tokyo, Japon", "AU-SY": "Sydney, Australie",
	"CA-TR": "Toronto, Canada", "EU": "Amsterdam, Pays-Bas", "EU-BU": "Bucarest, Roumanie",
	"EU-FR": "Francfort, Allemagne", "EU-LO": "Londres, Royaume-Uni", "EU-MD": "Madrid, Espagne",
	"EU-ML": "Milan, Italie", "EU-ST": "Stockholm, Suède", "IL": "Rosh Haayin, Israël", "IL-HA": "Haïfa, Israël",
	"IL-PT": "Petah Tikva, Israël", "IL-RH": "Rosh Haayin 2, Israël", "IL-TA": "Tel Aviv, Israël",
	"US-AT": "Atlanta, États-Unis", "US-CH": "Chicago, États-Unis", "US-LA": "Los Angeles, États-Unis",
	"US-MI": "Miami, États-Unis", "US-MI2": "Miami 2, États-Unis", "US-NY2": "New York, États-Unis",
	"US-SC": "Santa Clara, États-Unis", "US-SE": "Seattle, États-Unis", "US-TX": "Texas, États-Unis",
}

func (c Client) kamatera(ctx context.Context, id, secret, location string) (Catalog, error) {
	base := c.KamateraBase
	if base == "" {
		base = kamateraAPI
	}
	base = strings.TrimRight(base, "/")
	// The datacenter list of the API takes more than ten seconds, so this cheap call only checks the keys.
	raw, err := get(ctx, c.http(), base+"/service/servers", kamateraHeader(id, secret))
	if err != nil {
		return Catalog{}, err
	}
	if !KamateraAccepted(raw) {
		return Catalog{}, fmt.Errorf("Les identifiants ont été refusés.")
	}
	priceURL := c.KamateraPriceURL
	if priceURL == "" {
		priceURL = "https://console.kamatera.com/info/calculator.js.php"
	}
	prices, err := get(ctx, c.http(), priceURL, nil)
	if err != nil {
		return Catalog{}, err
	}
	locations, err := kamateraLocations(prices)
	if err != nil {
		return Catalog{}, err
	}
	return parseKamatera(prices, locations, pickLocation(locations, location, "EU-FR"))
}

// KamateraAccepted tells a server list (a JSON array) from an error object returned with status 200.
func KamateraAccepted(body []byte) bool {
	var list []json.RawMessage
	return json.Unmarshal(body, &list) == nil
}

func kamateraLocations(prices []byte) ([]Location, error) {
	doc, err := KamateraCalculator(prices)
	if err != nil {
		return nil, err
	}
	var ids []string
	if json.Unmarshal(doc["datacenters"], &ids) != nil {
		return nil, fmt.Errorf("Le provider ne répond pas.")
	}
	var out []Location
	for _, id := range ids {
		label := kamateraLabels[id]
		if label == "" {
			label = id
		}
		out = append(out, Location{ID: id, Label: label})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Label < out[j].Label })
	if len(out) == 0 {
		return nil, fmt.Errorf("Aucune offre disponible.")
	}
	return out, nil
}

// KamateraPrices is the public calculator, read as a JSON document.
func kamateraTable(prices map[string]json.RawMessage, key string) map[string]float64 {
	var groups []struct {
		Options []struct {
			Value string  `json:"value"`
			Price float64 `json:"price"`
		} `json:"options"`
	}
	out := map[string]float64{}
	if json.Unmarshal(prices[key], &groups) != nil {
		return out
	}
	for _, group := range groups {
		for _, option := range group.Options {
			out[option.Value] = option.Price
		}
	}
	return out
}

// KamateraCalculator extracts the JSON document embedded in calculator.js.php.
func KamateraCalculator(raw []byte) (map[string]json.RawMessage, error) {
	parts := strings.Split(string(raw), "'")
	if len(parts) < 3 {
		return nil, fmt.Errorf("Le provider ne répond pas.")
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal([]byte(strings.Join(parts[1:len(parts)-1], "'")), &doc); err != nil {
		return nil, fmt.Errorf("Le provider ne répond pas.")
	}
	return doc, nil
}

func parseKamatera(raw []byte, locations []Location, chosen string) (Catalog, error) {
	doc, err := KamateraCalculator(raw)
	if err != nil {
		return Catalog{}, err
	}
	cpu := kamateraTable(doc, "cpu")
	disk := kamateraTable(doc, "diskGB")
	ram := map[string]map[string]float64{}
	for _, kind := range []string{"A", "B"} {
		ram[kind] = kamateraTable(doc, "ramMB."+kind)
	}
	offers := make([]Offer, 0)
	for _, preset := range kamateraPresets {
		cpuKey := strconv.Itoa(preset.Cores) + preset.Type
		cpuPrice, okCPU := cpu[cpuKey]
		ramPrice, okRAM := ram[preset.Type][strconv.Itoa(preset.RAM)]
		diskPrice, okDisk := disk[strconv.Itoa(preset.DiskGB)]
		if !okCPU || !okRAM || !okDisk {
			continue
		}
		total := cpuPrice + ramPrice + diskPrice
		if total <= 0 {
			continue
		}
		family := "Disponibilité"
		if preset.Type == "B" {
			family = "Standard"
		}
		offers = append(offers, Offer{
			ID:       KamateraOfferID(preset.Cores, preset.Type, preset.RAM, preset.DiskGB),
			Name:     fmt.Sprintf("%s %d vCPU %d Go", family, preset.Cores, preset.RAM/1024),
			Location: chosen, VCPU: preset.Cores, MemoryMB: preset.RAM, DiskGB: preset.DiskGB,
			Monthly: formatMoney(total),
		})
	}
	if len(offers) == 0 {
		return Catalog{}, fmt.Errorf("Aucune offre disponible.")
	}
	sort.Slice(offers, func(i, j int) bool {
		a, _ := strconv.ParseFloat(offers[i].Monthly, 64)
		b, _ := strconv.ParseFloat(offers[j].Monthly, 64)
		return a < b
	})
	return Catalog{
		Provider: "kamatera", Currency: "USD", Location: chosen, Locations: locations,
		Periods: []string{"month"},
		Note:    "Kamatera facture ici au mois, trafic inclus. Prix en dollars.",
		Offers:  offers,
	}, nil
}

// KamateraOfferID encodes a preset as "2A-4096-40" (cores+type, RAM in MB, disk in GB).
func KamateraOfferID(cores int, kind string, ram, disk int) string {
	return fmt.Sprintf("%d%s-%d-%d", cores, kind, ram, disk)
}
