package api

import (
	"testing"

	"myinfra/internal/catalog"
)

// The same rule as hourlyRate in web/src/measure.tsx, so that a quote and
// the home page agree.
func TestServerHourlyReadsAsTheHomePage(t *testing.T) {
	cases := []struct {
		view     serverView
		hourly   float64
		currency string
		ok       bool
	}{
		{serverView{Plan: &catalog.Plan{Hourly: "0.0136", Monthly: "8.49", Currency: "EUR"}}, 0.0136, "EUR", true},
		{serverView{Plan: &catalog.Plan{Monthly: "7.30", Currency: "USD"}}, 0.01, "USD", true},
		{serverView{Hosting: &hostingView{Price: "0.5", Period: "hour"}}, 0.5, "EUR", true},
		{serverView{Hosting: &hostingView{Price: "73", Period: "month", Currency: "usd"}}, 0.1, "USD", true},
		{serverView{Plan: &catalog.Plan{Hourly: "0", Monthly: "x"}}, 0, "EUR", false},
		{serverView{}, 0, "", false},
	}
	for i, c := range cases {
		hourly, currency, ok := serverHourly(c.view)
		if ok != c.ok || (ok && (currency != c.currency || hourly < c.hourly-1e-9 || hourly > c.hourly+1e-9)) {
			t.Fatalf("cas %d : %v %s %v", i, hourly, currency, ok)
		}
	}
	total := amounts{}
	total.add("EUR", 9.928)
	total.add("", 1)
	total.add("USD", 12)
	if got := total.String(); got != "10,93 €/mois + 12,00 $/mois" {
		t.Fatal(got)
	}
}
