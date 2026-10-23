package api

import (
	"testing"
	"time"
)

func TestManualHosting(t *testing.T) {
	if got := manualHosting([]byte(`{"offerId":"cx22","hosting":{"provider":"hetzner"}}`)); got != nil {
		t.Fatalf("bought machine: got %+v", got)
	}
	got := manualHosting([]byte(`{"location":"Paris","hosting":{"provider":"other","name":"Kimsufi","price":"5.99","period":"month","currency":"EUR"}}`))
	if got == nil || got.Name != "Kimsufi" || got.Price != "5.99" {
		t.Fatalf("got %+v", got)
	}
	if hourly, monthly := got.rates(); hourly != "" || monthly != "5.99" {
		t.Fatalf("rates %q %q", hourly, monthly)
	}
	if got := manualHosting([]byte(`{"location":"Paris"}`)); got != nil {
		t.Fatalf("no hosting: got %+v", got)
	}
}

func TestCleanHosting(t *testing.T) {
	got, problem := cleanHosting(hostingView{Provider: " OVH ", Name: "ignored", Price: "12,50", Period: "month", Currency: "eur"})
	if problem != "" || got.Provider != "ovh" || got.Name != "" || got.Price != "12.50" || got.Currency != "EUR" {
		t.Fatalf("got %+v %q", got, problem)
	}
	if _, problem := cleanHosting(hostingView{Provider: "other"}); problem == "" {
		t.Fatal("other without a name is accepted")
	}
	if _, problem := cleanHosting(hostingView{Provider: "other", Name: "<b>x</b>"}); problem == "" {
		t.Fatal("markup in the name is accepted")
	}
	for _, price := range []string{"-1", "abc", "1e9", "1/3"} {
		if _, problem := cleanHosting(hostingView{Price: price}); problem == "" {
			t.Fatalf("price %q is accepted", price)
		}
	}
	got, problem = cleanHosting(hostingView{Provider: "other", Name: "  Mon   hébergeur ", Price: "0.02", Period: "hour", Currency: "usd"})
	if problem != "" || got.Name != "Mon hébergeur" || got.Period != "hour" || got.Currency != "USD" {
		t.Fatalf("got %+v %q", got, problem)
	}
	got, _ = cleanHosting(hostingView{Provider: "hetzner", Period: "hour", Currency: "USD"})
	if got.Period != "" || got.Currency != "" {
		t.Fatalf("no price keeps a period: %+v", got)
	}
}

func TestManualServerCost(t *testing.T) {
	bill := billOf("", "73", "EUR", time.Now().Add(-10*time.Hour))
	if bill == nil || bill.Amount != "1.0000" {
		t.Fatalf("got %+v", bill)
	}
}
