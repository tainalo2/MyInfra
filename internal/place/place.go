package place

import (
	"regexp"
	"strings"
)

// Place is where a provider location is: its city, country and continent.
type Place struct {
	Label   string  `json:"label"`
	Country string  `json:"country"`
	Lat     float64 `json:"lat"`
	Lon     float64 `json:"lon"`
}

// named reads provider location ids, as web/src/places.ts does: keep both
// in step (a test compares them).
var named = []struct {
	test  string
	place Place
}{
	{`(?i)^(fr-par|par\d*$)`, Place{Label: "Paris", Country: "FR", Lat: 48.86, Lon: 2.35}},
	{`(?i)^nl-ams|^ams`, Place{Label: "Amsterdam", Country: "NL", Lat: 52.37, Lon: 4.9}},
	{`(?i)^pl-waw|^waw`, Place{Label: "Varsovie", Country: "PL", Lat: 52.23, Lon: 21.01}},
	{`(?i)^gra`, Place{Label: "Gravelines", Country: "FR", Lat: 50.98, Lon: 2.13}},
	{`(?i)^sbg`, Place{Label: "Strasbourg", Country: "FR", Lat: 48.58, Lon: 7.75}},
	{`(?i)^rbx`, Place{Label: "Roubaix", Country: "FR", Lat: 50.69, Lon: 3.18}},
	{`(?i)^de$|^fra`, Place{Label: "Francfort", Country: "DE", Lat: 50.11, Lon: 8.68}},
	{`(?i)^uk$|^lon`, Place{Label: "Londres", Country: "GB", Lat: 51.51, Lon: -0.12}},
	{`(?i)^bhs`, Place{Label: "Beauharnois", Country: "CA", Lat: 45.31, Lon: -73.87}},
	{`(?i)^fsn`, Place{Label: "Falkenstein", Country: "DE", Lat: 50.48, Lon: 12.37}},
	{`(?i)^nbg`, Place{Label: "Nuremberg", Country: "DE", Lat: 49.45, Lon: 11.08}},
	{`(?i)^hel`, Place{Label: "Helsinki", Country: "FI", Lat: 60.17, Lon: 24.94}},
	{`(?i)^ash`, Place{Label: "Ashburn", Country: "US", Lat: 39.04, Lon: -77.49}},
	{`(?i)^hil`, Place{Label: "Hillsboro", Country: "US", Lat: 45.52, Lon: -122.99}},
	{`(?i)^nyc`, Place{Label: "New York", Country: "US", Lat: 40.71, Lon: -74.01}},
	{`(?i)^sfo`, Place{Label: "San Francisco", Country: "US", Lat: 37.77, Lon: -122.42}},
	{`(?i)^sgp`, Place{Label: "Singapour", Country: "SG", Lat: 1.35, Lon: 103.82}},
	{`(?i)^tor`, Place{Label: "Toronto", Country: "CA", Lat: 43.65, Lon: -79.38}},
	{`(?i)^blr`, Place{Label: "Bangalore", Country: "IN", Lat: 12.97, Lon: 77.59}},
	{`(?i)^syd`, Place{Label: "Sydney", Country: "AU", Lat: -33.87, Lon: 151.21}},
	{`(?i)^dc3`, Place{Label: "Genève", Country: "CH", Lat: 46.2, Lon: 6.14}},
	{`(?i)^dc4`, Place{Label: "Zurich", Country: "CH", Lat: 47.38, Lon: 8.54}},
	{`(?i)^fi-hel`, Place{Label: "Helsinki", Country: "FI", Lat: 60.17, Lon: 24.94}},
	{`(?i)^de-fra|^eu-central`, Place{Label: "Francfort", Country: "DE", Lat: 50.11, Lon: 8.68}},
	{`(?i)^uk-lon|^lhr$|^eu-west`, Place{Label: "Londres", Country: "GB", Lat: 51.51, Lon: -0.13}},
	{`(?i)^us-chi|^ord$`, Place{Label: "Chicago", Country: "US", Lat: 41.88, Lon: -87.63}},
	{`(?i)^us-nyc`, Place{Label: "New York", Country: "US", Lat: 40.71, Lon: -74.01}},
	{`(?i)^us-sjo|^sjc$`, Place{Label: "San Jose", Country: "US", Lat: 37.34, Lon: -121.89}},
	{`(?i)^sg-sin|^ap-south`, Place{Label: "Singapour", Country: "SG", Lat: 1.35, Lon: 103.82}},
	{`(?i)^au-syd|^ap-southeast`, Place{Label: "Sydney", Country: "AU", Lat: -33.87, Lon: 151.21}},
	{`(?i)^es-mad|^mad$`, Place{Label: "Madrid", Country: "ES", Lat: 40.42, Lon: -3.7}},
	{`(?i)^se-sto|^sto$`, Place{Label: "Stockholm", Country: "SE", Lat: 59.33, Lon: 18.07}},
	{`(?i)^ewr$|^us-east`, Place{Label: "Newark", Country: "US", Lat: 40.74, Lon: -74.17}},
	{`(?i)^cdg$`, Place{Label: "Paris", Country: "FR", Lat: 49.01, Lon: 2.55}},
	{`(?i)^lax$|^us-lax`, Place{Label: "Los Angeles", Country: "US", Lat: 33.94, Lon: -118.41}},
	{`(?i)^atl$|^us-southeast`, Place{Label: "Atlanta", Country: "US", Lat: 33.75, Lon: -84.39}},
	{`(?i)^dfw$|^us-central`, Place{Label: "Dallas", Country: "US", Lat: 32.78, Lon: -96.8}},
	{`(?i)^sea$|^us-sea`, Place{Label: "Seattle", Country: "US", Lat: 47.61, Lon: -122.33}},
	{`(?i)^mia$|^us-mia`, Place{Label: "Miami", Country: "US", Lat: 25.76, Lon: -80.19}},
	{`(?i)^us-west`, Place{Label: "Fremont", Country: "US", Lat: 37.55, Lon: -121.99}},
	{`(?i)^us-iad|^iad$`, Place{Label: "Ashburn", Country: "US", Lat: 39.04, Lon: -77.49}},
	{`(?i)^ca-central|^yto$`, Place{Label: "Toronto", Country: "CA", Lat: 43.65, Lon: -79.38}},
	{`(?i)^it-mil`, Place{Label: "Milan", Country: "IT", Lat: 45.46, Lon: 9.19}},
	{`(?i)^nrt$|^ap-northeast`, Place{Label: "Tokyo", Country: "JP", Lat: 35.68, Lon: 139.69}},
	{`(?i)^jp-osa`, Place{Label: "Osaka", Country: "JP", Lat: 34.69, Lon: 135.5}},
	{`(?i)^icn$`, Place{Label: "Séoul", Country: "KR", Lat: 37.57, Lon: 126.98}},
	{`(?i)^bom$|^ap-west`, Place{Label: "Mumbai", Country: "IN", Lat: 19.08, Lon: 72.88}},
	{`(?i)^mel$|^au-mel`, Place{Label: "Melbourne", Country: "AU", Lat: -37.81, Lon: 144.96}},
	{`(?i)^sao$|^br-gru`, Place{Label: "São Paulo", Country: "BR", Lat: -23.55, Lon: -46.63}},
	{`(?i)^scl$`, Place{Label: "Santiago", Country: "CL", Lat: -33.45, Lon: -70.67}},
	{`(?i)^mex$`, Place{Label: "Mexico", Country: "MX", Lat: 19.43, Lon: -99.13}},
	{`(?i)^id-cgk`, Place{Label: "Jakarta", Country: "ID", Lat: -6.21, Lon: 106.85}},
	{`(?i)^in-maa`, Place{Label: "Chennai", Country: "IN", Lat: 13.08, Lon: 80.27}},
	{`(?i)^ch-gva`, Place{Label: "Genève", Country: "CH", Lat: 46.2, Lon: 6.14}},
	{`(?i)^ch-dk`, Place{Label: "Zurich", Country: "CH", Lat: 47.38, Lon: 8.54}},
	{`(?i)^de-muc`, Place{Label: "Munich", Country: "DE", Lat: 48.14, Lon: 11.58}},
	{`(?i)^at-vie`, Place{Label: "Vienne", Country: "AT", Lat: 48.21, Lon: 16.37}},
	{`(?i)^bg-sof`, Place{Label: "Sofia", Country: "BG", Lat: 42.7, Lon: 23.32}},
	{`(?i)^luxembourg`, Place{Label: "Luxembourg", Country: "LU", Lat: 49.61, Lon: 6.13}},
	{`(?i)^paris`, Place{Label: "Paris", Country: "FR", Lat: 48.86, Lon: 2.35}},
	{`(?i)^tokyo`, Place{Label: "Tokyo", Country: "JP", Lat: 35.68, Lon: 139.69}},
	{`(?i)^singapore`, Place{Label: "Singapour", Country: "SG", Lat: 1.35, Lon: 103.82}},
	{`(?i)^dubai`, Place{Label: "Dubaï", Country: "AE", Lat: 25.2, Lon: 55.27}},
	{`(?i)^istanbul`, Place{Label: "Istanbul", Country: "TR", Lat: 41.01, Lon: 28.98}},
	{`(?i)^manassas`, Place{Label: "Manassas", Country: "US", Lat: 38.75, Lon: -77.48}},
	{`(?i)^santa-clara|^us-sc$`, Place{Label: "Santa Clara", Country: "US", Lat: 37.35, Lon: -121.96}},
	{`(?i)^mumbai`, Place{Label: "Mumbai", Country: "IN", Lat: 19.08, Lon: 72.88}},
	{`(?i)^almaty`, Place{Label: "Almaty", Country: "KZ", Lat: 43.24, Lon: 76.89}},
	{`(?i)^de/fra`, Place{Label: "Francfort", Country: "DE", Lat: 50.11, Lon: 8.68}},
	{`(?i)^de-txl|^de/txl`, Place{Label: "Berlin", Country: "DE", Lat: 52.52, Lon: 13.4}},
	{`(?i)^es-vit|^es/vit`, Place{Label: "Vitoria", Country: "ES", Lat: 42.85, Lon: -2.67}},
	{`(?i)^fr-par|^fr/par`, Place{Label: "Paris", Country: "FR", Lat: 48.86, Lon: 2.35}},
	{`(?i)^gb-lhr|^gb/lhr`, Place{Label: "Londres", Country: "GB", Lat: 51.51, Lon: -0.13}},
	{`(?i)^gb-bhx|^gb/bhx`, Place{Label: "Worcester", Country: "GB", Lat: 52.19, Lon: -2.22}},
	{`(?i)^us-las|^us/las`, Place{Label: "Las Vegas", Country: "US", Lat: 36.17, Lon: -115.14}},
	{`(?i)^us-ewr|^us/ewr`, Place{Label: "Newark", Country: "US", Lat: 40.74, Lon: -74.17}},
	{`(?i)^us-mci|^us/mci`, Place{Label: "Kansas City", Country: "US", Lat: 39.1, Lon: -94.58}},
	{`(?i)^eu$`, Place{Label: "Amsterdam", Country: "NL", Lat: 52.37, Lon: 4.9}},
	{`(?i)^eu-fr$`, Place{Label: "Francfort", Country: "DE", Lat: 50.11, Lon: 8.68}},
	{`(?i)^eu-lo$`, Place{Label: "Londres", Country: "GB", Lat: 51.51, Lon: -0.13}},
	{`(?i)^eu-ml$`, Place{Label: "Milan", Country: "IT", Lat: 45.46, Lon: 9.19}},
	{`(?i)^eu-md$`, Place{Label: "Madrid", Country: "ES", Lat: 40.42, Lon: -3.7}},
	{`(?i)^eu-st$`, Place{Label: "Stockholm", Country: "SE", Lat: 59.33, Lon: 18.07}},
	{`(?i)^eu-bu$`, Place{Label: "Bucarest", Country: "RO", Lat: 44.43, Lon: 26.1}},
	{`(?i)^il-ta$|^il$`, Place{Label: "Tel Aviv", Country: "IL", Lat: 32.09, Lon: 34.78}},
	{`(?i)^il-pt$`, Place{Label: "Petah Tikva", Country: "IL", Lat: 32.09, Lon: 34.89}},
	{`(?i)^il-rh$`, Place{Label: "Rosh Haayin", Country: "IL", Lat: 32.09, Lon: 34.96}},
	{`(?i)^il-ha$`, Place{Label: "Haïfa", Country: "IL", Lat: 32.79, Lon: 34.99}},
	{`(?i)^us-ny2$`, Place{Label: "New York", Country: "US", Lat: 40.71, Lon: -74.01}},
	{`(?i)^us-ch$`, Place{Label: "Chicago", Country: "US", Lat: 41.88, Lon: -87.63}},
	{`(?i)^us-la$`, Place{Label: "Los Angeles", Country: "US", Lat: 34.05, Lon: -118.24}},
	{`(?i)^us-mi$`, Place{Label: "Miami", Country: "US", Lat: 25.76, Lon: -80.19}},
	{`(?i)^us-se$`, Place{Label: "Seattle", Country: "US", Lat: 47.61, Lon: -122.33}},
	{`(?i)^us-tx$`, Place{Label: "Dallas", Country: "US", Lat: 32.78, Lon: -96.8}},
	{`(?i)^us-at$`, Place{Label: "Atlanta", Country: "US", Lat: 33.75, Lon: -84.39}},
	{`(?i)^as$`, Place{Label: "Hong Kong", Country: "HK", Lat: 22.32, Lon: 114.17}},
	{`(?i)^as-sg$`, Place{Label: "Singapour", Country: "SG", Lat: 1.35, Lon: 103.82}},
	{`(?i)^as-ty$`, Place{Label: "Tokyo", Country: "JP", Lat: 35.68, Lon: 139.69}},
	{`(?i)^au-sy$`, Place{Label: "Sydney", Country: "AU", Lat: -33.87, Lon: 151.21}},
	{`(?i)^ca-tr$`, Place{Label: "Toronto", Country: "CA", Lat: 43.65, Lon: -79.38}},
}

var compiled = func() []*regexp.Regexp {
	out := make([]*regexp.Regexp, len(named))
	for i, item := range named {
		out[i] = regexp.MustCompile(item.test)
	}
	return out
}()

// Of finds where a provider location is, by its id, then by its label (the
// city named in it), or ok false.
func Of(id, label string) (Place, bool) {
	id = strings.TrimSpace(id)
	if id != "" {
		for i, re := range compiled {
			if re.MatchString(id) {
				return named[i].place, true
			}
		}
	}
	for _, candidate := range []string{id, label} {
		lower := strings.ToLower(strings.TrimSpace(candidate))
		if lower == "" {
			continue
		}
		for _, item := range named {
			city := strings.ToLower(item.place.Label)
			if lower == city || strings.HasPrefix(lower, city+" ") || strings.Contains(lower, city) {
				return item.place, true
			}
		}
	}
	return Place{}, false
}

// Country is a country where providers have machines.
type Country struct {
	Code      string
	Name      string
	English   string
	Continent string
}

// Continents, as assistants name them.
const (
	Europe       = "europe"
	NorthAmerica = "north-america"
	SouthAmerica = "south-america"
	Asia         = "asia"
	Oceania      = "oceania"
	Africa       = "africa"
)

var Continents = []string{Europe, NorthAmerica, SouthAmerica, Asia, Oceania, Africa}

var countries = []Country{
	{"FR", "France", "France", Europe},
	{"NL", "Pays-Bas", "Netherlands", Europe},
	{"PL", "Pologne", "Poland", Europe},
	{"DE", "Allemagne", "Germany", Europe},
	{"GB", "Royaume-Uni", "United Kingdom", Europe},
	{"FI", "Finlande", "Finland", Europe},
	{"CH", "Suisse", "Switzerland", Europe},
	{"ES", "Espagne", "Spain", Europe},
	{"SE", "Suède", "Sweden", Europe},
	{"IT", "Italie", "Italy", Europe},
	{"AT", "Autriche", "Austria", Europe},
	{"BG", "Bulgarie", "Bulgaria", Europe},
	{"LU", "Luxembourg", "Luxembourg", Europe},
	{"RO", "Roumanie", "Romania", Europe},
	{"TR", "Turquie", "Turkey", Europe},
	{"US", "États-Unis", "United States", NorthAmerica},
	{"CA", "Canada", "Canada", NorthAmerica},
	{"MX", "Mexique", "Mexico", NorthAmerica},
	{"BR", "Brésil", "Brazil", SouthAmerica},
	{"CL", "Chili", "Chile", SouthAmerica},
	{"SG", "Singapour", "Singapore", Asia},
	{"IN", "Inde", "India", Asia},
	{"JP", "Japon", "Japan", Asia},
	{"KR", "Corée du Sud", "South Korea", Asia},
	{"ID", "Indonésie", "Indonesia", Asia},
	{"AE", "Émirats arabes unis", "United Arab Emirates", Asia},
	{"KZ", "Kazakhstan", "Kazakhstan", Asia},
	{"IL", "Israël", "Israel", Asia},
	{"HK", "Hong Kong", "Hong Kong", Asia},
	{"AU", "Australie", "Australia", Oceania},
}

// CountryOf finds a country by its ISO code, or its French or English name.
func CountryOf(wanted string) (Country, bool) {
	wanted = strings.TrimSpace(wanted)
	for _, c := range countries {
		if strings.EqualFold(c.Code, wanted) || strings.EqualFold(c.Name, wanted) || strings.EqualFold(c.English, wanted) {
			return c, true
		}
	}
	switch strings.ToLower(wanted) {
	case "uk", "angleterre", "england":
		return CountryOf("GB")
	case "usa", "us", "etats-unis", "états unis":
		return CountryOf("US")
	}
	return Country{}, false
}

// Countries lists the known countries.
func Countries() []Country { return append([]Country(nil), countries...) }
