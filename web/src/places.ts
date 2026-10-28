export type Place = { label: string; lat: number; lon: number };

const named: { test: RegExp; place: Place }[] = [
  { test: /^(fr-par|par\d*$)/i, place: { label: "Paris", lat: 48.86, lon: 2.35 } },
  { test: /^nl-ams|^ams/i, place: { label: "Amsterdam", lat: 52.37, lon: 4.9 } },
  { test: /^pl-waw|^waw/i, place: { label: "Varsovie", lat: 52.23, lon: 21.01 } },
  { test: /^gra/i, place: { label: "Gravelines", lat: 50.98, lon: 2.13 } },
  { test: /^sbg/i, place: { label: "Strasbourg", lat: 48.58, lon: 7.75 } },
  { test: /^rbx/i, place: { label: "Roubaix", lat: 50.69, lon: 3.18 } },
  { test: /^de$|^fra/i, place: { label: "Francfort", lat: 50.11, lon: 8.68 } },
  { test: /^uk$|^lon/i, place: { label: "Londres", lat: 51.51, lon: -0.12 } },
  { test: /^bhs/i, place: { label: "Beauharnois", lat: 45.31, lon: -73.87 } },
  { test: /^fsn/i, place: { label: "Falkenstein", lat: 50.48, lon: 12.37 } },
  { test: /^nbg/i, place: { label: "Nuremberg", lat: 49.45, lon: 11.08 } },
  { test: /^hel/i, place: { label: "Helsinki", lat: 60.17, lon: 24.94 } },
  { test: /^ash/i, place: { label: "Ashburn", lat: 39.04, lon: -77.49 } },
  { test: /^hil/i, place: { label: "Hillsboro", lat: 45.52, lon: -122.99 } },
  { test: /^nyc/i, place: { label: "New York", lat: 40.71, lon: -74.01 } },
  { test: /^sfo/i, place: { label: "San Francisco", lat: 37.77, lon: -122.42 } },
  { test: /^sgp/i, place: { label: "Singapour", lat: 1.35, lon: 103.82 } },
  { test: /^tor/i, place: { label: "Toronto", lat: 43.65, lon: -79.38 } },
  { test: /^blr/i, place: { label: "Bangalore", lat: 12.97, lon: 77.59 } },
  { test: /^syd/i, place: { label: "Sydney", lat: -33.87, lon: 151.21 } },
  { test: /^dc3/i, place: { label: "Genève", lat: 46.2, lon: 6.14 } },
  { test: /^dc4/i, place: { label: "Plan-les-Ouates", lat: 46.17, lon: 6.12 } },
  { test: /^fi-hel/i, place: { label: "Helsinki", lat: 60.17, lon: 24.94 } },
  { test: /^de-fra|^eu-central/i, place: { label: "Francfort", lat: 50.11, lon: 8.68 } },
  { test: /^uk-lon|^lhr$|^eu-west/i, place: { label: "Londres", lat: 51.51, lon: -0.13 } },
  { test: /^us-chi|^ord$/i, place: { label: "Chicago", lat: 41.88, lon: -87.63 } },
  { test: /^us-nyc/i, place: { label: "New York", lat: 40.71, lon: -74.01 } },
  { test: /^us-sjo|^sjc$/i, place: { label: "San Jose", lat: 37.34, lon: -121.89 } },
  { test: /^sg-sin|^ap-south/i, place: { label: "Singapour", lat: 1.35, lon: 103.82 } },
  { test: /^au-syd|^ap-southeast/i, place: { label: "Sydney", lat: -33.87, lon: 151.21 } },
  { test: /^es-mad|^mad$/i, place: { label: "Madrid", lat: 40.42, lon: -3.7 } },
  { test: /^se-sto|^sto$/i, place: { label: "Stockholm", lat: 59.33, lon: 18.07 } },
  { test: /^ewr$|^us-east/i, place: { label: "Newark", lat: 40.74, lon: -74.17 } },
  { test: /^cdg$/i, place: { label: "Paris", lat: 49.01, lon: 2.55 } },
  { test: /^lax$|^us-lax/i, place: { label: "Los Angeles", lat: 33.94, lon: -118.41 } },
  { test: /^atl$|^us-southeast/i, place: { label: "Atlanta", lat: 33.75, lon: -84.39 } },
  { test: /^dfw$|^us-central/i, place: { label: "Dallas", lat: 32.78, lon: -96.8 } },
  { test: /^sea$|^us-sea/i, place: { label: "Seattle", lat: 47.61, lon: -122.33 } },
  { test: /^mia$|^us-mia/i, place: { label: "Miami", lat: 25.76, lon: -80.19 } },
  { test: /^us-west/i, place: { label: "Fremont", lat: 37.55, lon: -121.99 } },
  { test: /^us-iad|^iad$/i, place: { label: "Ashburn", lat: 39.04, lon: -77.49 } },
  { test: /^ca-central|^yto$/i, place: { label: "Toronto", lat: 43.65, lon: -79.38 } },
  { test: /^it-mil/i, place: { label: "Milan", lat: 45.46, lon: 9.19 } },
  { test: /^nrt$|^ap-northeast/i, place: { label: "Tokyo", lat: 35.68, lon: 139.69 } },
  { test: /^jp-osa/i, place: { label: "Osaka", lat: 34.69, lon: 135.5 } },
  { test: /^icn$/i, place: { label: "Séoul", lat: 37.57, lon: 126.98 } },
  { test: /^bom$|^ap-west/i, place: { label: "Mumbai", lat: 19.08, lon: 72.88 } },
  { test: /^mel$|^au-mel/i, place: { label: "Melbourne", lat: -37.81, lon: 144.96 } },
  { test: /^sao$|^br-gru/i, place: { label: "São Paulo", lat: -23.55, lon: -46.63 } },
  { test: /^scl$/i, place: { label: "Santiago", lat: -33.45, lon: -70.67 } },
  { test: /^mex$/i, place: { label: "Mexico", lat: 19.43, lon: -99.13 } },
  { test: /^id-cgk/i, place: { label: "Jakarta", lat: -6.21, lon: 106.85 } },
  { test: /^in-maa/i, place: { label: "Chennai", lat: 13.08, lon: 80.27 } },
  { test: /^ch-gva/i, place: { label: "Genève", lat: 46.2, lon: 6.14 } },
  { test: /^ch-dk/i, place: { label: "Zurich", lat: 47.38, lon: 8.54 } },
  { test: /^de-muc/i, place: { label: "Munich", lat: 48.14, lon: 11.58 } },
  { test: /^at-vie/i, place: { label: "Vienne", lat: 48.21, lon: 16.37 } },
  { test: /^bg-sof/i, place: { label: "Sofia", lat: 42.7, lon: 23.32 } },
  { test: /^luxembourg/i, place: { label: "Luxembourg", lat: 49.61, lon: 6.13 } },
  { test: /^paris/i, place: { label: "Paris", lat: 48.86, lon: 2.35 } },
  { test: /^tokyo/i, place: { label: "Tokyo", lat: 35.68, lon: 139.69 } },
  { test: /^singapore/i, place: { label: "Singapour", lat: 1.35, lon: 103.82 } },
  { test: /^dubai/i, place: { label: "Dubaï", lat: 25.2, lon: 55.27 } },
  { test: /^istanbul/i, place: { label: "Istanbul", lat: 41.01, lon: 28.98 } },
  { test: /^manassas/i, place: { label: "Manassas", lat: 38.75, lon: -77.48 } },
  { test: /^santa-clara|^us-sc$/i, place: { label: "Santa Clara", lat: 37.35, lon: -121.96 } },
  { test: /^mumbai/i, place: { label: "Mumbai", lat: 19.08, lon: 72.88 } },
  { test: /^almaty/i, place: { label: "Almaty", lat: 43.24, lon: 76.89 } },
  { test: /^de\/fra/i, place: { label: "Francfort", lat: 50.11, lon: 8.68 } },
  { test: /^de-txl|^de\/txl/i, place: { label: "Berlin", lat: 52.52, lon: 13.4 } },
  { test: /^es-vit|^es\/vit/i, place: { label: "Vitoria", lat: 42.85, lon: -2.67 } },
  { test: /^fr-par|^fr\/par/i, place: { label: "Paris", lat: 48.86, lon: 2.35 } },
  { test: /^gb-lhr|^gb\/lhr/i, place: { label: "Londres", lat: 51.51, lon: -0.13 } },
  { test: /^gb-bhx|^gb\/bhx/i, place: { label: "Worcester", lat: 52.19, lon: -2.22 } },
  { test: /^us-las|^us\/las/i, place: { label: "Las Vegas", lat: 36.17, lon: -115.14 } },
  { test: /^us-ewr|^us\/ewr/i, place: { label: "Newark", lat: 40.74, lon: -74.17 } },
  { test: /^us-mci|^us\/mci/i, place: { label: "Kansas City", lat: 39.1, lon: -94.58 } },
  { test: /^eu$/i, place: { label: "Amsterdam", lat: 52.37, lon: 4.9 } },
  { test: /^eu-fr$/i, place: { label: "Francfort", lat: 50.11, lon: 8.68 } },
  { test: /^eu-lo$/i, place: { label: "Londres", lat: 51.51, lon: -0.13 } },
  { test: /^eu-ml$/i, place: { label: "Milan", lat: 45.46, lon: 9.19 } },
  { test: /^eu-md$/i, place: { label: "Madrid", lat: 40.42, lon: -3.7 } },
  { test: /^eu-st$/i, place: { label: "Stockholm", lat: 59.33, lon: 18.07 } },
  { test: /^eu-bu$/i, place: { label: "Bucarest", lat: 44.43, lon: 26.1 } },
  { test: /^il-ta$|^il$/i, place: { label: "Tel Aviv", lat: 32.09, lon: 34.78 } },
  { test: /^il-pt$/i, place: { label: "Petah Tikva", lat: 32.09, lon: 34.89 } },
  { test: /^il-rh$/i, place: { label: "Rosh Haayin", lat: 32.09, lon: 34.96 } },
  { test: /^il-ha$/i, place: { label: "Haïfa", lat: 32.79, lon: 34.99 } },
  { test: /^us-ny2$/i, place: { label: "New York", lat: 40.71, lon: -74.01 } },
  { test: /^us-ch$/i, place: { label: "Chicago", lat: 41.88, lon: -87.63 } },
  { test: /^us-la$/i, place: { label: "Los Angeles", lat: 34.05, lon: -118.24 } },
  { test: /^us-mi$/i, place: { label: "Miami", lat: 25.76, lon: -80.19 } },
  { test: /^us-se$/i, place: { label: "Seattle", lat: 47.61, lon: -122.33 } },
  { test: /^us-tx$/i, place: { label: "Dallas", lat: 32.78, lon: -96.8 } },
  { test: /^us-at$/i, place: { label: "Atlanta", lat: 33.75, lon: -84.39 } },
  { test: /^as$/i, place: { label: "Hong Kong", lat: 22.32, lon: 114.17 } },
  { test: /^as-sg$/i, place: { label: "Singapour", lat: 1.35, lon: 103.82 } },
  { test: /^as-ty$/i, place: { label: "Tokyo", lat: 35.68, lon: 139.69 } },
  { test: /^au-sy$/i, place: { label: "Sydney", lat: -33.87, lon: 151.21 } },
  { test: /^ca-tr$/i, place: { label: "Toronto", lat: 43.65, lon: -79.38 } },
  { test: /^sin$/i, place: { label: "Singapour", lat: 1.35, lon: 103.82 } },
  { test: /^lim$|^eu-west-lim/i, place: { label: "Limburg", lat: 50.38, lon: 8.06 } },
  { test: /^jnb$|^johannesburg/i, place: { label: "Johannesburg", lat: -26.2, lon: 28.05 } },
  { test: /^hnl$|^honolulu/i, place: { label: "Honolulu", lat: 21.31, lon: -157.86 } },
  { test: /^del$|^delhi/i, place: { label: "Delhi", lat: 28.61, lon: 77.21 } },
  { test: /^manchester|^mnc$/i, place: { label: "Manchester", lat: 53.48, lon: -2.24 } },
  { test: /^marseille|^mrs$/i, place: { label: "Marseille", lat: 43.3, lon: 5.37 } },
  { test: /^bruxelles|^brussels|^bru$/i, place: { label: "Bruxelles", lat: 50.85, lon: 4.35 } },
  { test: /^prague|^praha|^prg$/i, place: { label: "Prague", lat: 50.08, lon: 14.44 } },
  { test: /^copenhagen|^copenhague|^cph$/i, place: { label: "Copenhague", lat: 55.68, lon: 12.57 } },
  { test: /^dublin|^dln$/i, place: { label: "Dublin", lat: 53.35, lon: -6.26 } },
  { test: /^oslo|^osl$/i, place: { label: "Oslo", lat: 59.91, lon: 10.75 } },
  { test: /^lisbon|^lisboa|^lis$/i, place: { label: "Lisbonne", lat: 38.72, lon: -9.14 } },
];

export function placeOf(code?: string): Place | null {
  const value = (code ?? "").trim();
  if (!value) return null;
  const found = named.find((item) => item.test.test(value));
  if (found) return found.place;
  const lower = value.toLowerCase();
  return named.find((item) => item.place.label.toLowerCase() === lower)?.place ?? null;
}

export function knownPlaces(): Place[] {
  const seen = new Set<string>();
  const out: Place[] = [];
  for (const item of named) {
    if (seen.has(item.place.label)) continue;
    seen.add(item.place.label);
    out.push(item.place);
  }
  out.sort((a, b) => a.label.localeCompare(b.label, "fr"));
  return out;
}

export function placeLabel(code?: string) {
  return placeOf(code)?.label || (code ?? "").trim();
}
