// Package assistant holds the BYOK AI assistant backend: provider adapters,
// encrypted key storage, pricing table and the live token meter.
package assistant

import (
	"fmt"
	"sort"
)

// Price is in USD per million tokens.
type Price struct {
	Provider string  `json:"provider"`
	Model    string  `json:"model"`
	InPerM   float64 `json:"in_per_m"`
	OutPerM  float64 `json:"out_per_m"`
}

// Pricing maps provider/model to a price. Prices are editable settings, not hardcoded in callers.
type Pricing struct{ table map[string]Price }

func key(provider, model string) string { return provider + "/" + model }

// DefaultPricing returns the initial table; operators may override entries with Set.
func DefaultPricing() *Pricing {
	p := &Pricing{table: map[string]Price{}}
	for _, e := range []Price{
		{"claude", "claude-sonnet", 3, 15},
		{"claude", "claude-haiku", 0.8, 4},
		{"codex", "gpt-5-codex", 1.25, 10},
	} {
		p.Set(e)
	}
	return p
}

func (p *Pricing) Set(e Price) { p.table[key(e.Provider, e.Model)] = e }

func (p *Pricing) List() []Price {
	out := make([]Price, 0, len(p.table))
	for _, e := range p.table {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return key(out[i].Provider, out[i].Model) < key(out[j].Provider, out[j].Model) })
	return out
}

// Cost returns the estimated USD cost, or an error when the model has no price.
func (p *Pricing) Cost(provider, model string, in, out int) (float64, error) {
	e, ok := p.table[key(provider, model)]
	if !ok {
		return 0, fmt.Errorf("no pricing for %s/%s", provider, model)
	}
	return float64(in)/1e6*e.InPerM + float64(out)/1e6*e.OutPerM, nil
}
