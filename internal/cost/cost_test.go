package cost

import (
	"testing"
	"time"
)

func TestSinceUsesTheHourlyPrice(t *testing.T) {
	from := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	got, ok := Since("0.0200", "14.60", from, from.Add(10*time.Hour))
	if !ok || got != "0.2000" {
		t.Fatalf("hourly %s ok %v", got, ok)
	}
}

func TestSinceSpreadsTheMonthlyPrice(t *testing.T) {
	from := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	got, ok := Since("", "730", from, from.Add(730*time.Hour))
	if !ok || got != "730.0000" {
		t.Fatalf("monthly %s ok %v", got, ok)
	}
}

func TestSinceWithoutAPrice(t *testing.T) {
	if _, ok := Since("", "", time.Now(), time.Now()); ok {
		t.Fatal("empty price")
	}
}

func TestSinceBeforeOpeningIsZero(t *testing.T) {
	from := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	got, ok := Since("1", "", from, from.Add(-time.Hour))
	if !ok || got != "0.0000" {
		t.Fatalf("early %s ok %v", got, ok)
	}
}
