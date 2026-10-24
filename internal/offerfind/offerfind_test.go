package offerfind

import (
	"strings"
	"testing"

	"myinfra/internal/catalog"
	"myinfra/internal/place"
)

func at(id, label string) *place.Place {
	p, ok := place.Of(id, label)
	if !ok {
		return nil
	}
	return &p
}

func offer(account, provider, currency, loc, name string, vcpu, memGB, disk int, hourly, monthly string) Candidate {
	return Candidate{
		AccountID: account, Account: account, Provider: provider, Currency: currency,
		Location: catalog.Location{ID: loc}, Place: at(loc, ""),
		Offer: catalog.Offer{ID: strings.ToLower(name), Name: name, Location: loc, VCPU: vcpu, MemoryMB: memGB * 1024, DiskGB: disk, Hourly: hourly, Monthly: monthly},
	}
}

var market = []Candidate{
	offer("perso", "hetzner", "EUR", "fsn1", "CX23", 2, 4, 40, "0.0088", "5.49"),
	offer("perso", "hetzner", "EUR", "fsn1", "CX33", 4, 8, 80, "0.0136", "8.49"),
	offer("perso", "hetzner", "EUR", "hel1", "CCX23", 4, 16, 160, "0.04", "26.99"),
	offer("pro", "scaleway", "EUR", "fr-par-1", "DEV1-S", 2, 2, 20, "0.01", "7.3"),
	offer("pro", "scaleway", "EUR", "fr-par-1", "GP1-S", 8, 32, 600, "0.2", "146"),
	offer("ocean", "digitalocean", "USD", "ams3", "s-4vcpu-8gb", 4, 8, 160, "0.0714", "48"),
}

func TestCheapestThatFits(t *testing.T) {
	got := Find(market, Want{MinVCPU: 4, MinMemoryGB: 8})
	if len(got.Matches) != 4 || got.Matches[0].Offer.Name != "CX33" || got.Matches[0].Monthly != 8.49 {
		t.Fatal(got.Matches)
	}
}

func TestEachConstraint(t *testing.T) {
	cases := []struct {
		name string
		want Want
		only string
	}{
		{"provider", Want{Providers: []string{"Scaleway"}, MinVCPU: 4}, "GP1-S"},
		{"compte", Want{Providers: []string{"ocean"}}, "s-4vcpu-8gb"},
		{"pays", Want{Countries: []string{"France"}, MinVCPU: 4}, "GP1-S"},
		{"continent", Want{Continents: []string{place.Europe}, MinMemoryGB: 16, MaxMonthly: 30}, "CCX23"},
		{"lieu", Want{Locations: []string{"Helsinki"}}, "CCX23"},
		{"modèle", Want{Model: "ccx23"}, "CCX23"},
		{"disque", Want{MinDiskGB: 500}, "GP1-S"},
	}
	for _, c := range cases {
		got := Find(market, c.want)
		if len(got.Matches) == 0 || got.Matches[0].Offer.Name != c.only {
			t.Fatalf("%s : %+v", c.name, got)
		}
		for _, m := range got.Matches {
			if len(m.Gaps) > 0 {
				t.Fatalf("%s : écarts dans une correspondance %v", c.name, m.Gaps)
			}
		}
	}
}

func TestNearestWhenNothingFits(t *testing.T) {
	got := Find(market, Want{Countries: []string{"FR"}, MinMemoryGB: 8, MaxMonthly: 10})
	if len(got.Matches) != 0 || len(got.Alternatives) == 0 {
		t.Fatal(got)
	}
	first := got.Alternatives[0]
	// Hetzner in Germany at 8,49 € misses only the country: nearer than the
	// French offer at 146 €.
	if first.Offer.Name != "CX33" || len(first.Gaps) != 1 || !strings.Contains(first.Gaps[0], "Falkenstein (DE) au lieu de France") {
		t.Fatal(first)
	}
	for i := 1; i < len(got.Alternatives); i++ {
		if got.Alternatives[i].score < got.Alternatives[i-1].score {
			t.Fatal("alternatives mal classées")
		}
	}
}

func TestPriceGap(t *testing.T) {
	got := Find(market, Want{MinVCPU: 2, MaxMonthly: 5})
	if len(got.Alternatives) == 0 || !strings.Contains(got.Alternatives[0].Gaps[0], "5,49 EUR/mois au lieu de 5,00 EUR max") {
		t.Fatal(got.Alternatives)
	}
}

func TestCurrenciesAreNotConverted(t *testing.T) {
	got := Find(market, Want{Providers: []string{"digitalocean"}, MaxMonthly: 100})
	if len(got.Matches) != 0 || len(got.Notes) == 0 || !strings.Contains(got.Alternatives[0].Gaps[0], "non comparable") {
		t.Fatal(got)
	}
	got = Find(market, Want{Providers: []string{"digitalocean"}, MaxMonthly: 100, Currency: "usd"})
	if len(got.Matches) != 1 {
		t.Fatal(got)
	}
}

func TestUnknownModelAndProvider(t *testing.T) {
	got := Find(market, Want{Model: "XL-9000", Providers: []string{"ovh"}})
	if len(got.Impossible) != 2 || len(got.Alternatives) == 0 {
		t.Fatal(got)
	}
	if !strings.Contains(got.Impossible[0], "Aucun compte « ovh »") || !strings.Contains(got.Impossible[1], "XL-9000") {
		t.Fatal(got.Impossible)
	}
	if got := Find(nil, Want{}); len(got.Impossible) != 1 {
		t.Fatal(got)
	}
}

func TestVolumeReachesTheDisk(t *testing.T) {
	c := offer("perso", "hetzner", "EUR", "fsn1", "CX23", 2, 4, 40, "0.0088", "5.49")
	c.Offer.Storage = []catalog.Storage{{ID: "volume", Label: "Volume", MinGB: 10, MaxGB: 10240, MonthlyPerGB: "0.044"}}
	got := Find([]Candidate{c}, Want{MinDiskGB: 100})
	if len(got.Matches) != 1 {
		t.Fatal(got)
	}
	m := got.Matches[0]
	if m.Volume == nil || m.Volume.GB != 60 || m.DiskGB != 100 || m.Monthly < 8.12 || m.Monthly > 8.13 {
		t.Fatal(m, m.Volume)
	}
}

func TestQuantityAndPeriod(t *testing.T) {
	got := Find(market, Want{Quantity: 3, Period: "hour", Providers: []string{"hetzner"}})
	if len(got.Matches) != 3 || got.Matches[0].Period != "hour" || len(got.Notes) != 1 {
		t.Fatal(got)
	}
	if m := got.Matches[0]; m.Monthly != m.Theoretical {
		t.Fatal(m.Monthly, m.Theoretical)
	}
}
