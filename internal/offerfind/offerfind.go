// Package offerfind picks machine offers that meet what a person asks
// (provider, place, size, price, model) among the catalogs of their provider
// accounts, and when none does, the nearest ones, each with what it misses.
// It reads no network: the catalogs are given.
package offerfind

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"myinfra/internal/catalog"
	"myinfra/internal/place"
)

// Candidate is one offer at one location of one provider account.
type Candidate struct {
	AccountID string           `json:"accountId"`
	Account   string           `json:"account"`
	Provider  string           `json:"provider"`
	Currency  string           `json:"currency"`
	Location  catalog.Location `json:"location"`
	Place     *place.Place     `json:"place,omitempty"`
	Offer     catalog.Offer    `json:"offer"`
}

// Want is what is asked; zero fields ask nothing.
type Want struct {
	Providers   []string `json:"providers,omitempty"`
	Countries   []string `json:"countries,omitempty"`
	Continents  []string `json:"continents,omitempty"`
	Locations   []string `json:"locations,omitempty"`
	MinVCPU     int      `json:"minVcpu,omitempty"`
	MinMemoryGB float64  `json:"minMemoryGb,omitempty"`
	MinDiskGB   int      `json:"minDiskGb,omitempty"`
	MinGPUs     int      `json:"minGpus,omitempty"`
	GPU         string   `json:"gpu,omitempty"`
	MaxMonthly  float64  `json:"maxMonthly,omitempty"`
	Currency    string   `json:"currency,omitempty"`
	Model       string   `json:"model,omitempty"`
	Period      string   `json:"period,omitempty"`
	Quantity    int      `json:"quantity,omitempty"`
}

// Volume is the disk added to an offer to reach the size asked.
type Volume struct {
	StorageID string  `json:"storageId"`
	Label     string  `json:"label"`
	GB        int     `json:"gb"`
	Monthly   float64 `json:"monthly"`
}

// Match is an offer with its price for the order: the price is the monthly
// one the provider shows, or 730 hours, volume included; Theoretical is what
// the home page will count, the hourly price times 730, without the volume.
type Match struct {
	Candidate
	Period      string   `json:"period"`
	Monthly     float64  `json:"monthly"`
	Theoretical float64  `json:"theoretical"`
	Volume      *Volume  `json:"volume,omitempty"`
	DiskGB      int      `json:"diskGb"`
	Gaps        []string `json:"gaps,omitempty"`
	score       float64
}

// Result is what a search finds.
type Result struct {
	Matches      []Match  `json:"matches"`
	Alternatives []Match  `json:"alternatives"`
	Impossible   []string `json:"impossible"`
	Notes        []string `json:"notes"`
}

const (
	maxMatches      = 10
	maxAlternatives = 5
)

// Find returns the offers that meet w, cheapest first, or the nearest ones.
func Find(all []Candidate, w Want) Result {
	w = normal(w)
	out := Result{Matches: []Match{}, Alternatives: []Match{}, Impossible: []string{}, Notes: []string{}}
	if len(all) == 0 {
		out.Impossible = append(out.Impossible, "Aucun compte provider n'est enregistré, ou aucun ne répond : ajoutez-en un dans Réglages › Providers.")
		return out
	}
	for _, wanted := range w.Providers {
		found := false
		for _, c := range all {
			if sameProvider(c, wanted) {
				found = true
				break
			}
		}
		if !found {
			out.Impossible = append(out.Impossible, fmt.Sprintf("Aucun compte « %s » n'est enregistré : ajoutez-le dans Réglages › Providers, ou cherchez chez un autre.", wanted))
		}
	}
	if w.Model != "" {
		known := false
		for _, c := range all {
			if sameModel(c.Offer, w.Model) {
				known = true
				break
			}
		}
		if !known {
			out.Impossible = append(out.Impossible, fmt.Sprintf("Le modèle « %s » n'existe chez aucun des comptes : les plus proches sont proposés.", w.Model))
		}
	}
	model := modelSpec(all, w.Model)
	unplaced, uncomparable := 0, 0
	var near []Match
	for _, c := range all {
		m, ok := price(c, w)
		if !ok {
			continue
		}
		if geoAsked(w) && c.Place == nil {
			unplaced++
		}
		if w.MaxMonthly > 0 && !strings.EqualFold(c.Currency, w.Currency) {
			uncomparable++
		}
		m.Gaps, m.score = gaps(m, w, model)
		if len(m.Gaps) == 0 {
			out.Matches = append(out.Matches, m)
		} else {
			near = append(near, m)
		}
	}
	sort.SliceStable(out.Matches, func(i, j int) bool { return less(out.Matches[i], out.Matches[j]) })
	if len(out.Matches) > maxMatches {
		out.Matches = out.Matches[:maxMatches]
	}
	if len(out.Matches) == 0 {
		sort.SliceStable(near, func(i, j int) bool {
			if near[i].score != near[j].score {
				return near[i].score < near[j].score
			}
			return less(near[i], near[j])
		})
		seen := map[string]bool{}
		for _, m := range near {
			key := m.AccountID + "/" + m.Offer.ID
			if seen[key] {
				continue
			}
			seen[key] = true
			out.Alternatives = append(out.Alternatives, m)
			if len(out.Alternatives) == maxAlternatives {
				break
			}
		}
		if len(out.Alternatives) == 0 {
			out.Impossible = append(out.Impossible, "Aucune offre n'a de prix pour la période demandée.")
		}
	}
	if unplaced > 0 {
		out.Notes = append(out.Notes, fmt.Sprintf("%d offre(s) dans des lieux que MyInfra ne sait pas situer ont été écartées du filtre géographique.", unplaced))
	}
	if uncomparable > 0 {
		out.Notes = append(out.Notes, fmt.Sprintf("%d offre(s) en une autre devise que %s : leur prix n'est pas converti, elles ne comptent pas comme sous la limite.", uncomparable, w.Currency))
	}
	if w.Quantity > 1 {
		out.Notes = append(out.Notes, fmt.Sprintf("Quantité %d : les prix sont par machine. Les quotas des comptes ne sont pas connus d'avance : le provider peut refuser au-delà de son plafond.", w.Quantity))
	}
	return out
}

func normal(w Want) Want {
	w.Currency = strings.ToUpper(strings.TrimSpace(w.Currency))
	if w.Currency == "" {
		w.Currency = "EUR"
	}
	w.Model = strings.TrimSpace(w.Model)
	w.GPU = strings.TrimSpace(w.GPU)
	var countries []string
	for _, c := range w.Countries {
		if found, ok := place.CountryOf(c); ok {
			countries = append(countries, found.Code)
		} else {
			countries = append(countries, strings.ToUpper(strings.TrimSpace(c)))
		}
	}
	w.Countries = countries
	if w.Quantity < 1 {
		w.Quantity = 1
	}
	return w
}

// InPlace tells whether a location is where w asks, or w asks no place.
func InPlace(c Candidate, w Want) bool {
	w = normal(w)
	return !geoAsked(w) || placeFits(c, w)
}

func geoAsked(w Want) bool {
	return len(w.Countries)+len(w.Continents)+len(w.Locations) > 0
}

func sameProvider(c Candidate, wanted string) bool {
	return strings.EqualFold(c.Provider, wanted) || strings.EqualFold(c.Account, wanted) || c.AccountID == wanted
}

func gpuLabel(o catalog.Offer) string {
	if o.GPU != "" {
		return o.GPU
	}
	return "GPU"
}

func gpuNamed(o catalog.Offer, wanted string) bool {
	wanted = strings.ToLower(wanted)
	return strings.Contains(strings.ToLower(o.GPU), wanted) || strings.Contains(strings.ToLower(o.Name), wanted) || strings.Contains(strings.ToLower(o.ID), wanted)
}

func sameModel(o catalog.Offer, wanted string) bool {
	return strings.EqualFold(o.ID, wanted) || strings.EqualFold(o.Name, wanted) || (o.MonthlyID != "" && strings.EqualFold(o.MonthlyID, wanted))
}

// modelSpec is the size of the model asked, when some catalog has it, so
// that alternatives come near it.
func modelSpec(all []Candidate, model string) *catalog.Offer {
	if model == "" {
		return nil
	}
	for _, c := range all {
		if sameModel(c.Offer, model) {
			o := c.Offer
			return &o
		}
	}
	return nil
}

func parse(v string) float64 {
	n, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
	if err != nil || n <= 0 || math.IsInf(n, 0) || math.IsNaN(n) {
		return 0
	}
	return n
}

// price gives an offer its period, its monthly price and a volume when the
// disk asked is more than the offer's own; ok false when it has no price
// for the period asked.
func price(c Candidate, w Want) (Match, bool) {
	hourly, monthly := parse(c.Offer.Hourly), parse(c.Offer.Monthly)
	period := w.Period
	if period == "" {
		period = "month"
		if monthly == 0 {
			period = "hour"
		}
	}
	if (period == "month" && monthly == 0) || (period == "hour" && hourly == 0) {
		return Match{}, false
	}
	m := Match{Candidate: c, Period: period, DiskGB: c.Offer.DiskGB}
	m.Monthly = monthly
	if period == "hour" || m.Monthly == 0 {
		m.Monthly = hourly * 730
	}
	m.Theoretical = hourly * 730
	if hourly == 0 {
		m.Theoretical = monthly
	}
	need := w.MinDiskGB - c.Offer.DiskGB
	for _, st := range c.Offer.Storage {
		if !st.Required && need <= 0 {
			continue
		}
		gb := max(need, st.MinGB, 1)
		if st.MaxGB > 0 && gb > st.MaxGB {
			continue
		}
		perGB := parse(st.MonthlyPerGB)
		if perGB == 0 {
			perGB = parse(st.HourlyPerGB) * 730
		}
		m.Volume = &Volume{StorageID: st.ID, Label: st.Label, GB: gb, Monthly: perGB * float64(gb)}
		m.DiskGB = c.Offer.DiskGB + gb
		// The home page counts the machine alone: Theoretical leaves the
		// volume out, as it does.
		m.Monthly += m.Volume.Monthly
		break
	}
	return m, true
}

func less(a, b Match) bool {
	if a.Monthly != b.Monthly {
		return a.Monthly < b.Monthly
	}
	if a.Offer.VCPU != b.Offer.VCPU {
		return a.Offer.VCPU > b.Offer.VCPU
	}
	return a.Account+a.Location.ID+a.Offer.ID < b.Account+b.Location.ID+b.Offer.ID
}

func memGB(o catalog.Offer) float64 { return float64(o.MemoryMB) / 1024 }

// gaps lists what an offer misses of w, in words, and how far it is.
func gaps(m Match, w Want, model *catalog.Offer) ([]string, float64) {
	var out []string
	score := 0.0
	o := m.Offer
	if len(w.Providers) > 0 {
		ok := false
		for _, p := range w.Providers {
			if sameProvider(m.Candidate, p) {
				ok = true
			}
		}
		if !ok {
			out = append(out, fmt.Sprintf("chez %s (%s), hors des providers demandés", m.Provider, m.Account))
			score += 3
		}
	}
	if geoAsked(w) {
		if !placeFits(m.Candidate, w) {
			where := m.Location.Label
			if where == "" {
				where = m.Location.ID
			}
			if m.Place != nil {
				where = m.Place.Label + " (" + m.Place.Country + ")"
			}
			out = append(out, "à "+where+" au lieu de "+askedPlace(w))
			score += 2
			if !continentFits(m.Candidate, w) {
				score += 1.5
			}
		}
	}
	if w.Model != "" && !sameModel(o, w.Model) {
		out = append(out, fmt.Sprintf("modèle %s au lieu de %s", o.Name, w.Model))
		score += 3
		if model != nil {
			score += 2 * (relGap(float64(o.VCPU), float64(model.VCPU)) + relGap(memGB(o), memGB(*model)))
		}
	}
	if w.MinVCPU > 0 && o.VCPU < w.MinVCPU {
		out = append(out, fmt.Sprintf("%d vCPU au lieu de %d", o.VCPU, w.MinVCPU))
		score += 4 * float64(w.MinVCPU-o.VCPU) / float64(w.MinVCPU)
	}
	if w.MinMemoryGB > 0 && memGB(o)+1e-9 < w.MinMemoryGB {
		out = append(out, fmt.Sprintf("%s Go de mémoire au lieu de %s", trim(memGB(o)), trim(w.MinMemoryGB)))
		score += 4 * (w.MinMemoryGB - memGB(o)) / w.MinMemoryGB
	}
	if w.MinGPUs > 0 || w.GPU != "" {
		need := w.MinGPUs
		if need < 1 {
			need = 1
		}
		switch {
		case o.GPUs < need:
			got := "pas de GPU"
			if o.GPUs > 0 {
				got = fmt.Sprintf("%d %s", o.GPUs, gpuLabel(o))
			}
			out = append(out, fmt.Sprintf("%s au lieu de %d GPU", got, need))
			score += 4 * float64(need-o.GPUs) / float64(need)
		case w.GPU != "" && !gpuNamed(o, w.GPU):
			out = append(out, fmt.Sprintf("GPU %s au lieu de %s", gpuLabel(o), w.GPU))
			score += 3
		}
	}
	if w.MinDiskGB > 0 && m.DiskGB < w.MinDiskGB {
		out = append(out, fmt.Sprintf("%d Go de disque au lieu de %d (pas de volume possible)", m.DiskGB, w.MinDiskGB))
		score += 2 * float64(w.MinDiskGB-m.DiskGB) / float64(w.MinDiskGB)
	}
	if w.MaxMonthly > 0 {
		if !strings.EqualFold(m.Currency, w.Currency) {
			out = append(out, fmt.Sprintf("prix en %s, non comparable à une limite en %s", m.Currency, w.Currency))
			score += 1
		} else if m.Monthly > w.MaxMonthly+1e-9 {
			out = append(out, fmt.Sprintf("%s %s/mois au lieu de %s %s max", money(m.Monthly), m.Currency, money(w.MaxMonthly), w.Currency))
			score += 5 * (m.Monthly - w.MaxMonthly) / w.MaxMonthly
		}
	}
	return out, score
}

func relGap(got, want float64) float64 {
	if want <= 0 {
		return 0
	}
	return math.Abs(got-want) / want
}

func placeFits(c Candidate, w Want) bool {
	for _, loc := range w.Locations {
		if strings.EqualFold(loc, c.Location.ID) || (c.Place != nil && strings.EqualFold(loc, c.Place.Label)) ||
			(c.Location.Label != "" && strings.Contains(strings.ToLower(c.Location.Label), strings.ToLower(loc))) {
			return true
		}
	}
	if c.Place == nil {
		return false
	}
	for _, country := range w.Countries {
		if strings.EqualFold(country, c.Place.Country) {
			return true
		}
	}
	return continentListed(c, w)
}

func continentListed(c Candidate, w Want) bool {
	if c.Place == nil {
		return false
	}
	country, ok := place.CountryOf(c.Place.Country)
	if !ok {
		return false
	}
	for _, continent := range w.Continents {
		if strings.EqualFold(continent, country.Continent) {
			return true
		}
	}
	return false
}

// continentFits tells whether an offer at least is on the continent of a
// place asked, to rank a neighbour country before another continent.
func continentFits(c Candidate, w Want) bool {
	if c.Place == nil {
		return false
	}
	here, ok := place.CountryOf(c.Place.Country)
	if !ok {
		return false
	}
	if continentListed(c, w) {
		return true
	}
	for _, code := range w.Countries {
		if asked, ok := place.CountryOf(code); ok && asked.Continent == here.Continent {
			return true
		}
	}
	return false
}

func askedPlace(w Want) string {
	var parts []string
	for _, code := range w.Countries {
		if c, ok := place.CountryOf(code); ok {
			parts = append(parts, c.Name)
		} else {
			parts = append(parts, code)
		}
	}
	parts = append(parts, w.Continents...)
	parts = append(parts, w.Locations...)
	return strings.Join(parts, " ou ")
}

func trim(v float64) string {
	return strings.TrimSuffix(strings.TrimRight(strconv.FormatFloat(v, 'f', 2, 64), "0"), ".")
}

func money(v float64) string {
	return strings.Replace(strconv.FormatFloat(math.Round(v*100)/100, 'f', 2, 64), ".", ",", 1)
}
