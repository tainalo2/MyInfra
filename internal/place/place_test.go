package place

import (
	"context"
	"os"
	"regexp"
	"testing"

	"myinfra/internal/catalog"
)

// The Go table and the one of the screens read locations alike.
func TestSameAsTheScreens(t *testing.T) {
	src, err := os.ReadFile("../../web/src/places.ts")
	if err != nil {
		t.Skip("interface absente")
	}
	rows := regexp.MustCompile(`\{ test: /(.+?)/i, place: \{ label: "(.+?)"`).FindAllStringSubmatch(string(src), -1)
	if len(rows) != len(named) {
		t.Fatalf("%d lieux dans places.ts, %d ici", len(rows), len(named))
	}
	for i, row := range rows {
		want := "(?i)" + regexp.MustCompile(`\\/`).ReplaceAllString(row[1], "/")
		if named[i].test != want || named[i].place.Label != row[2] {
			t.Fatalf("lieu %d : %q %q, places.ts %q %q", i, named[i].test, named[i].place.Label, want, row[2])
		}
	}
}

func TestEveryPlaceHasACountry(t *testing.T) {
	for _, item := range named {
		c, ok := CountryOf(item.place.Country)
		if !ok || c.Continent == "" {
			t.Fatalf("%s : pays %q inconnu", item.place.Label, item.place.Country)
		}
	}
}

// Every location the sample catalogs offer is placed.
func TestSampleLocationsArePlaced(t *testing.T) {
	client := catalog.Client{AllowTest: true}
	creds := catalog.Creds{Token: "test-x", ApplicationKey: "test-x", ApplicationSecret: "test-x", ConsumerKey: "test-x", ProjectID: "test-x"}
	for _, provider := range []string{"hetzner", "scaleway", "ovh", "digitalocean", "infomaniak", "upcloud", "vultr", "akamai", "exoscale", "gcore", "kamatera", "ionos"} {
		cat, err := client.List(context.Background(), provider, creds, "")
		if err != nil {
			t.Fatal(provider, err)
		}
		for _, loc := range cat.Locations {
			if _, ok := Of(loc.ID, loc.Label); !ok {
				t.Errorf("%s : lieu %s (%s) non situé", provider, loc.ID, loc.Label)
			}
		}
	}
}

func TestInfomaniakAndMissingSites(t *testing.T) {
	got, ok := Of("dc4-a", "")
	if !ok || got.Label != "Plan-les-Ouates" || got.Country != "CH" {
		t.Fatal(got, ok)
	}
	if got, ok = Of("sin", ""); !ok || got.Label != "Singapour" {
		t.Fatal(got, ok)
	}
	if got, ok = Of("", "Plan-les-Ouates"); !ok || got.Lat == 0 {
		t.Fatal(got, ok)
	}
}

func TestCountryNames(t *testing.T) {
	for _, name := range []string{"FR", "france", "Allemagne", "germany", "uk", "USA"} {
		if _, ok := CountryOf(name); !ok {
			t.Fatal(name)
		}
	}
}
