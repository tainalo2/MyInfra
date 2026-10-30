package catalog

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type Client struct {
	HTTP              *http.Client
	HetznerBase       string
	ScalewayBase      string
	OVHBase           string
	DigitalOceanBase  string
	InfomaniakBase    string
	UpCloudBase       string
	VultrBase         string
	AkamaiBase        string
	ExoscaleBase      string
	ExoscalePriceBase string
	GcoreBase         string
	KamateraBase      string
	KamateraPriceURL  string
	IonosBase         string
	AllowTest         bool
}

type Creds struct {
	Token             string
	ProjectID         string
	ApplicationKey    string
	ApplicationSecret string
	ConsumerKey       string
}

type Location struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

type Storage struct {
	ID           string `json:"id"`
	Label        string `json:"label"`
	MinGB        int    `json:"minGb"`
	MaxGB        int    `json:"maxGb"`
	HourlyPerGB  string `json:"hourlyPerGb,omitempty"`
	MonthlyPerGB string `json:"monthlyPerGb,omitempty"`
	Required     bool   `json:"required"`
}

type Offer struct {
	ID        string `json:"id"`
	MonthlyID string `json:"monthlyId,omitempty"`
	Name      string `json:"name"`
	Location  string `json:"location"`
	VCPU      int    `json:"vcpu"`
	MemoryMB  int    `json:"memoryMb"`
	DiskGB    int    `json:"diskGb"`
	// GPUs is how many graphics cards the offer includes. GPU names the
	// model, for example L4, when the provider says which one.
	GPUs    int       `json:"gpus,omitempty"`
	GPU     string    `json:"gpu,omitempty"`
	Hourly  string    `json:"hourly,omitempty"`
	Monthly string    `json:"monthly,omitempty"`
	Storage []Storage `json:"storage,omitempty"`
}

type Catalog struct {
	Provider  string     `json:"provider"`
	Currency  string     `json:"currency"`
	Location  string     `json:"location"`
	Locations []Location `json:"locations"`
	Periods   []string   `json:"periods"`
	Note      string     `json:"note"`
	Offers    []Offer    `json:"offers"`
}

type Plan struct {
	OfferID      string `json:"offerId"`
	Name         string `json:"name"`
	Location     string `json:"location"`
	Period       string `json:"period"`
	Hourly       string `json:"hourly,omitempty"`
	Monthly      string `json:"monthly,omitempty"`
	VCPU         int    `json:"vcpu"`
	MemoryMB     int    `json:"memoryMb"`
	DiskGB       int    `json:"diskGb"`
	GPUs         int    `json:"gpus,omitempty"`
	GPU          string `json:"gpu,omitempty"`
	StorageID    string `json:"storageId,omitempty"`
	StorageGB    int    `json:"storageGb,omitempty"`
	StorageLabel string `json:"storageLabel,omitempty"`
	ProjectID    string `json:"projectId,omitempty"`
	Currency     string `json:"currency"`
}

func (c Client) http() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 30 * time.Second}
}

func (c Client) testToken(token string) bool {
	return c.AllowTest && strings.HasPrefix(strings.TrimSpace(token), "test-")
}

func (c Client) List(ctx context.Context, provider string, creds Creds, location string) (Catalog, error) {
	switch provider {
	case "hetzner":
		if c.testToken(creds.Token) {
			return sampleHetzner(location), nil
		}
		return c.hetzner(ctx, creds.Token, location)
	case "scaleway":
		if c.testToken(creds.Token) {
			return sampleScaleway(location), nil
		}
		return c.scaleway(ctx, creds.Token, location)
	case "ovh":
		if c.testToken(creds.ApplicationKey) && c.testToken(creds.ApplicationSecret) && c.testToken(creds.ConsumerKey) {
			return sampleOVH(location), nil
		}
		return c.ovh(ctx, location)
	case "digitalocean":
		if c.testToken(creds.Token) {
			return sampleDigitalOcean(location), nil
		}
		return c.digitalOcean(ctx, creds.Token, location)
	case "infomaniak":
		if c.testToken(creds.ApplicationKey) && c.testToken(creds.ApplicationSecret) {
			return sampleInfomaniak(location), nil
		}
		return c.infomaniak(ctx, creds.ApplicationKey, creds.ApplicationSecret, location)
	case "upcloud":
		if c.testToken(creds.Token) {
			return sampleUpCloud(location), nil
		}
		return c.upcloud(ctx, creds.Token, location)
	case "vultr":
		if c.testToken(creds.Token) {
			return sampleVultr(location), nil
		}
		return c.vultr(ctx, creds.Token, location)
	case "akamai":
		if c.testToken(creds.Token) {
			return sampleAkamai(location), nil
		}
		return c.akamai(ctx, creds.Token, location)
	case "exoscale":
		if c.testToken(creds.ApplicationKey) && c.testToken(creds.ApplicationSecret) {
			return sampleExoscale(location), nil
		}
		return c.exoscale(ctx, creds.ApplicationKey, creds.ApplicationSecret, location)
	case "gcore":
		if c.testToken(creds.Token) {
			return sampleGcore(location), nil
		}
		return c.gcore(ctx, creds.Token, location)
	case "kamatera":
		if c.testToken(creds.ApplicationKey) && c.testToken(creds.ApplicationSecret) {
			return sampleKamatera(location), nil
		}
		return c.kamatera(ctx, creds.ApplicationKey, creds.ApplicationSecret, location)
	case "ionos":
		if c.testToken(creds.Token) {
			return sampleIonos(location), nil
		}
		return c.ionos(ctx, creds.Token, location)
	case "openstack":
		if c.testToken(creds.ApplicationSecret) {
			return sampleOpenStack(location), nil
		}
		return c.openStack(ctx, creds, location)
	case "proxmox":
		if c.testToken(creds.Token) {
			return sampleProxmox(location), nil
		}
		return c.proxmox(ctx, creds.ApplicationKey, creds.Token, location)
	default:
		return Catalog{}, fmt.Errorf("Provider inconnu.")
	}
}

func (cat Catalog) Plan(offerID, period, storageID string, storageGB int) (Plan, error) {
	if period != "hour" && period != "month" {
		return Plan{}, fmt.Errorf("Choisissez l'heure ou le mois.")
	}
	var offer Offer
	found := false
	for _, item := range cat.Offers {
		if item.ID == offerID || (period == "month" && item.MonthlyID == offerID) {
			offer = item
			found = true
			break
		}
	}
	if !found {
		return Plan{}, fmt.Errorf("Type de machine introuvable.")
	}
	if period == "hour" && offer.Hourly == "" {
		return Plan{}, fmt.Errorf("Cette offre n'a pas de prix à l'heure.")
	}
	if period == "month" && offer.Monthly == "" {
		return Plan{}, fmt.Errorf("Cette offre n'a pas de prix au mois.")
	}
	id := offer.ID
	if period == "month" && offer.MonthlyID != "" {
		id = offer.MonthlyID
	}
	plan := Plan{
		OfferID: id, Name: offer.Name, Location: cat.Location, Period: period,
		Hourly: offer.Hourly, Monthly: offer.Monthly,
		VCPU: offer.VCPU, MemoryMB: offer.MemoryMB, DiskGB: offer.DiskGB,
		GPUs: offer.GPUs, GPU: offer.GPU, Currency: cat.Currency,
	}
	if storageID == "" {
		for _, item := range offer.Storage {
			if item.Required {
				return Plan{}, fmt.Errorf("Indiquez la taille du disque.")
			}
		}
		return plan, nil
	}
	var storage Storage
	ok := false
	for _, item := range offer.Storage {
		if item.ID == storageID {
			storage = item
			ok = true
			break
		}
	}
	if !ok {
		return Plan{}, fmt.Errorf("Stockage introuvable.")
	}
	if storageGB < storage.MinGB || (storage.MaxGB > 0 && storageGB > storage.MaxGB) {
		return Plan{}, fmt.Errorf("La taille du disque doit rester entre %d et %d Go.", storage.MinGB, storage.MaxGB)
	}
	plan.StorageID = storage.ID
	plan.StorageGB = storageGB
	plan.StorageLabel = storage.Label
	return plan, nil
}

func get(ctx context.Context, client *http.Client, url string, header map[string]string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	for key, value := range header {
		req.Header.Set(key, value)
	}
	res, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Le provider ne répond pas.")
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 32<<20))
	if err != nil {
		return nil, fmt.Errorf("Le provider ne répond pas.")
	}
	if res.StatusCode >= 300 {
		return nil, fmt.Errorf("Le provider ne répond pas.")
	}
	return body, nil
}

func money(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return ""
	}
	return formatMoney(value)
}

func formatMoney(value float64) string {
	if value < 0 {
		return ""
	}
	text := strconv.FormatFloat(value, 'f', 6, 64)
	text = strings.TrimRight(text, "0")
	text = strings.TrimRight(text, ".")
	if text == "" {
		return "0"
	}
	return text
}

func monthlyFromHourly(hourly string) string {
	value, err := strconv.ParseFloat(hourly, 64)
	if err != nil || value <= 0 {
		return ""
	}
	return formatMoney(value * 730)
}

func pickLocation(locations []Location, want, fallback string) string {
	if want != "" {
		for _, item := range locations {
			if item.ID == want {
				return want
			}
		}
	}
	for _, item := range locations {
		if item.ID == fallback {
			return fallback
		}
	}
	if len(locations) > 0 {
		return locations[0].ID
	}
	return fallback
}
