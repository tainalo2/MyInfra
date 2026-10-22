package api

import "testing"

func TestManualPlaceIgnoresBoughtPlan(t *testing.T) {
	if got := manualPlace([]byte(`{"offerId":"DEV1-S","name":"DEV1-S","location":"fr-par-1"}`)); got != "" {
		t.Fatal(got)
	}
	if got := manualPlace([]byte(`{"location":"Paris"}`)); got != "Paris" {
		t.Fatal(got)
	}
	if got := manualPlace([]byte(`{}`)); got != "" {
		t.Fatal(got)
	}
}

func TestCleanPlace(t *testing.T) {
	if got, ok := cleanPlace("  São Paulo "); !ok || got != "São Paulo" {
		t.Fatalf("%q %v", got, ok)
	}
	if _, ok := cleanPlace("Paris<script>"); ok {
		t.Fatal("accepted")
	}
	if got, ok := cleanPlace("  "); !ok || got != "" {
		t.Fatalf("%q %v", got, ok)
	}
}
