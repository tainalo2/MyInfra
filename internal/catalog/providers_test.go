package catalog

import (
	"context"
	"testing"
)

func TestParseUpCloud(t *testing.T) {
	plans := []byte(`{"plans":{"plan":[{"name":"1xCPU-2GB","core_number":1,"memory_amount":2048,"storage_size":50},{"name":"GPU-8xCPU","core_number":8,"memory_amount":32768,"storage_size":0,"gpu_amount":1}]}}`)
	zones := []byte(`{"zones":{"zone":[{"id":"fi-hel1","description":"Helsinki #1","public":"yes"},{"id":"de-fra1","description":"Frankfurt #1","public":"yes"},{"id":"secret","description":"Privé","public":"no"}]}}`)
	prices := []byte(`{"prices":{"zone":[{"name":"de-fra1","server_plan_1xCPU-2GB":{"amount":1,"price":1.04},"server_plan_GPU-8xCPU":{"amount":1,"price":58},"server_core":{"amount":1,"price":0.013}}]}}`)
	cat, err := parseUpCloud(plans, zones, prices, "")
	if err != nil {
		t.Fatal(err)
	}
	if cat.Location != "de-fra1" || cat.Currency != "EUR" || len(cat.Locations) != 2 || len(cat.Offers) != 2 {
		t.Fatalf("%+v", cat)
	}
	if cat.Offers[0].ID != "1xCPU-2GB" || cat.Offers[0].Hourly != "0.0104" || cat.Offers[0].DiskGB != 50 || cat.Offers[0].Monthly == "" || cat.Offers[0].GPUs != 0 {
		t.Fatalf("offre %+v", cat.Offers[0])
	}
	if cat.Offers[1].ID != "GPU-8xCPU" || cat.Offers[1].GPUs != 1 || cat.Offers[1].GPU != "GPU" || cat.Offers[1].Hourly != "0.58" {
		t.Fatalf("gpu %+v", cat.Offers[1])
	}
}

func TestParseVultr(t *testing.T) {
	regions := [][]byte{[]byte(`{"regions":[{"id":"ams","city":"Amsterdam"},{"id":"cdg","city":"Paris"}],"meta":{"links":{"next":""}}}`)}
	plans := [][]byte{[]byte(`{"plans":[{"id":"vc2-1c-1gb","vcpu_count":1,"ram":1024,"disk":25,"monthly_cost":5,"hourly_cost":0.007,"type":"vc2","locations":["ams","cdg"],"gpu_brand":"none","location_cost":{"cdg":{"monthly_cost":6,"hourly_cost":0.009}}},{"id":"vcg-l4-1","vcpu_count":4,"ram":16384,"disk":100,"monthly_cost":100,"hourly_cost":0.2,"type":"vcg","locations":["cdg"],"gpu_brand":"NVIDIA","gpu_count":1},{"id":"vbm-1","vcpu_count":8,"ram":32768,"disk":100,"monthly_cost":200,"hourly_cost":0.4,"type":"vbm","locations":["cdg"],"gpu_brand":"NVIDIA"}]}`)}
	cat, err := parseVultr(regions, plans, "cdg")
	if err != nil {
		t.Fatal(err)
	}
	if cat.Currency != "USD" || cat.Location != "cdg" || len(cat.Offers) != 2 || cat.Offers[0].Hourly != "0.009" || cat.Offers[0].Monthly != "6" {
		t.Fatalf("%+v", cat)
	}
	if cat.Offers[1].ID != "vcg-l4-1" || cat.Offers[1].GPUs != 1 || cat.Offers[1].GPU != "L4" {
		t.Fatalf("gpu %+v", cat.Offers[1])
	}
}

func TestParseAkamai(t *testing.T) {
	regions := [][]byte{[]byte(`{"data":[{"id":"fr-par","label":"Paris, FR","capabilities":["Linodes","Metadata"]},{"id":"edge","label":"Edge","capabilities":["Object Storage"]},{"id":"nometa","label":"Sans Metadata","capabilities":["Linodes"]}],"page":1,"pages":1}`)}
	types := [][]byte{[]byte(`{"data":[{"id":"g6-nanode-1","label":"Nanode 1GB","class":"nanode","disk":25600,"memory":1024,"vcpus":1,"price":{"hourly":0.0075,"monthly":5},"region_prices":[{"id":"fr-par","hourly":0.008,"monthly":6}]},{"id":"gpu","label":"GPU","class":"gpu","disk":102400,"memory":8192,"vcpus":4,"gpus":1,"price":{"hourly":1,"monthly":700}}],"page":1,"pages":1}`)}
	cat, err := parseAkamai(regions, types, "")
	if err != nil {
		t.Fatal(err)
	}
	if cat.Location != "fr-par" || cat.Currency != "USD" || len(cat.Locations) != 1 || len(cat.Offers) != 2 {
		t.Fatalf("%+v", cat)
	}
	var offer, gpu Offer
	for _, item := range cat.Offers {
		if item.ID == "g6-nanode-1" {
			offer = item
		}
		if item.ID == "gpu" {
			gpu = item
		}
	}
	if offer.ID != "g6-nanode-1" || offer.DiskGB != 25 || offer.Hourly != "0.008" || offer.Monthly != "6" || offer.GPUs != 0 {
		t.Fatalf("offre %+v", offer)
	}
	if gpu.GPUs != 1 || gpu.GPU != "GPU" {
		t.Fatalf("gpu %+v", gpu)
	}
}

func TestSampleProviders(t *testing.T) {
	ctx := context.Background()
	client := Client{AllowTest: true}
	up, err := client.List(ctx, "upcloud", Creds{Token: "test-token"}, "fi-hel1")
	if err != nil || up.Location != "fi-hel1" || up.Provider != "upcloud" {
		t.Fatalf("%+v %v", up, err)
	}
	vultr, err := client.List(ctx, "vultr", Creds{Token: "test-token"}, "cdg")
	if err != nil || vultr.Location != "cdg" || vultr.Currency != "USD" {
		t.Fatalf("%+v %v", vultr, err)
	}
	akamai, err := client.List(ctx, "akamai", Creds{Token: "test-token"}, "")
	if err != nil || akamai.Location != "fr-par" || len(akamai.Offers) == 0 {
		t.Fatalf("%+v %v", akamai, err)
	}
}

func TestParseExoscale(t *testing.T) {
	types := []byte(`{"instance-types":[
		{"id":"a","family":"standard","size":"tiny","cpus":1,"gpus":0,"memory":1073741824,"authorized":true},
		{"id":"b","family":"memory","size":"extra-large","cpus":2,"gpus":0,"memory":17179869184,"authorized":true},
		{"id":"c","family":"gpu3","size":"small","cpus":12,"gpus":1,"memory":1073741824,"authorized":true},
		{"id":"d","family":"standard","size":"huge","cpus":8,"gpus":0,"memory":34359738368,"authorized":false}]}`)
	prices := []byte(`{"eur":{"running_tiny":"0.01458000","running_memory_extra_large":"0.14000000","running_gpu3_small":"1.04530000","running_huge":"0.37334000","volume":"0.00014000"}}`)
	locations := []Location{{ID: "ch-gva-2", Label: "Genève, CH"}}
	cat, err := parseExoscale(types, prices, locations, "ch-gva-2")
	if err != nil || len(cat.Offers) != 3 || cat.Currency != "EUR" {
		t.Fatalf("%+v %v", cat, err)
	}
	first := cat.Offers[0]
	if first.ID != "a" || first.Hourly != "0.01458" || first.MemoryMB != 1024 || len(first.Storage) != 1 || first.Storage[0].HourlyPerGB != "0.00014" {
		t.Fatalf("offre %+v", first)
	}
	if cat.Offers[1].Name != "Mémoire Extra large" || cat.Offers[1].MemoryMB != 16384 {
		t.Fatalf("offre %+v", cat.Offers[1])
	}
	if cat.Offers[2].ID != "c" || cat.Offers[2].GPUs != 1 || cat.Offers[2].GPU != "A40" || cat.Offers[2].Name != "A40 Small" {
		t.Fatalf("gpu %+v", cat.Offers[2])
	}
}

func TestParseGcore(t *testing.T) {
	regions := []byte(`{"count":3,"results":[
		{"id":76,"display_name":"Luxembourg-2","country":"LU","state":"ACTIVE","has_kvm":true},
		{"id":99,"display_name":"Fermée","country":"XX","state":"DELETED","has_kvm":true},
		{"id":12,"display_name":"Sans KVM","country":"FR","state":"ACTIVE","has_kvm":false}]}`)
	locations := parseGcoreRegions(regions)
	if len(locations) != 1 || locations[0].ID != "luxembourg-2-76" || GcoreRegionID(locations[0].ID) != "76" {
		t.Fatalf("%+v", locations)
	}
	flavors := []byte(`{"count":4,"results":[
		{"flavor_id":"g1-standard-1-2","flavor_name":"g1-standard-1-2","vcpus":1,"ram":2048,"currency_code":"EUR","price_per_hour":0.0137,"price_per_month":10},
		{"flavor_id":"g1s-shared-1-0.5","flavor_name":"shared","vcpus":1,"ram":512,"currency_code":"EUR","price_per_hour":0.004,"price_per_month":3},
		{"flavor_id":"bm1-hf-small","flavor_name":"bm","vcpus":32,"ram":65536,"currency_code":"EUR","price_per_hour":1,"price_per_month":700},
		{"flavor_id":"g1-standard-2-4","flavor_name":"g1-standard-2-4","vcpus":2,"ram":4096,"currency_code":"EUR","price_per_hour":0,"price_per_month":0},
		{"flavor_id":"g1-gpu-l4-1","flavor_name":"L4","vcpus":8,"ram":49152,"currency_code":"EUR","price_per_hour":0.79,"price_per_month":574,"gpu_count":1}]}`)
	cat, err := parseGcore(flavors, locations, locations[0].ID)
	if err != nil || len(cat.Offers) != 2 || cat.Currency != "EUR" {
		t.Fatalf("%+v %v", cat, err)
	}
	if cat.Offers[0].ID != "g1-standard-1-2" || cat.Offers[0].Monthly != "10" || cat.Offers[1].GPUs != 1 || cat.Offers[1].GPU != "L4" {
		t.Fatalf("%+v", cat.Offers)
	}
	if GcoreProject([]byte(`{"results":[{"id":3,"is_default":false},{"id":9,"is_default":true}]}`)) != "9" {
		t.Fatal("projet par défaut")
	}
}

func TestParseKamatera(t *testing.T) {
	doc := `{"cpu":[{"options":[{"value":"1A","price":0},{"value":"2A","price":4}]}],` +
		`"ramMB.A":[{"options":[{"value":"1024","price":-2},{"value":"4096","price":6}]}],` +
		`"ramMB.B":[{"options":[]}],` +
		`"diskGB":[{"options":[{"value":"20","price":6},{"value":"40","price":9}]}]}`
	raw := []byte("var prd_prices = '" + doc + "';")
	locations := []Location{{ID: "EU-FR", Label: "Frankfurt"}}
	if got, err := kamateraLocations([]byte("var p = '{\"datacenters\":[\"EU-FR\",\"ZZ\"]}';")); err != nil || len(got) != 2 || got[0].Label != "Francfort, Allemagne" {
		t.Fatalf("lieux %+v %v", got, err)
	}
	if !KamateraAccepted([]byte(`[]`)) || KamateraAccepted([]byte(`{"errors":[{"info":"Invalid clientId or secret"}]}`)) {
		t.Fatal("réponse d'erreur à 200")
	}
	cat, err := parseKamatera(raw, locations, "EU-FR")
	if err != nil {
		t.Fatal(err)
	}
	if cat.Currency != "USD" || len(cat.Offers) != 2 || cat.Offers[0].Monthly != "4" || cat.Offers[0].ID != "1A-1024-20" {
		t.Fatalf("%+v", cat)
	}
	if cat.Offers[1].ID != "2A-4096-40" || cat.Offers[1].Monthly != "19" {
		t.Fatalf("%+v", cat.Offers[1])
	}
}

func TestParseIonos(t *testing.T) {
	raw := []byte(`{"items":[
		{"id":"de/fra","properties":{"name":"Frankfurt","imageAliases":["debian:latest","ubuntu:latest"],"cpuArchitecture":[{"cpuFamily":"AMD_EPYC"}]}},
		{"id":"us/las","properties":{"name":"Las Vegas","imageAliases":["windows:latest"]}},
		{"id":"de/txl","properties":{"name":"Berlin","imageAliases":["debian:latest"],"cpuArchitecture":[]}}]}`)
	cat, err := parseIonos(raw, "")
	if err != nil || cat.Location != "de-fra" || len(cat.Locations) != 1 || cat.Currency != "EUR" {
		t.Fatalf("%+v %v", cat, err)
	}
	if IonosLocation("de-fra") != "de/fra" {
		t.Fatal("identifiant de lieu")
	}
	plan, err := cat.Plan("vcpu-2-4", "hour", "boot", 20)
	if err != nil || plan.Hourly != "0.032" || plan.StorageGB != 20 {
		t.Fatalf("%+v %v", plan, err)
	}
}

func TestSampleNewProviders(t *testing.T) {
	ctx := context.Background()
	client := Client{AllowTest: true}
	pair := Creds{ApplicationKey: "test-key", ApplicationSecret: "test-secret"}
	for _, item := range []struct {
		provider string
		creds    Creds
	}{
		{"exoscale", pair}, {"kamatera", pair},
		{"gcore", Creds{Token: "test-token"}}, {"ionos", Creds{Token: "test-token"}},
	} {
		cat, err := client.List(ctx, item.provider, item.creds, "")
		if err != nil || cat.Provider != item.provider || len(cat.Offers) == 0 || cat.Location == "" {
			t.Fatalf("%s %+v %v", item.provider, cat, err)
		}
	}
	if _, err := client.List(ctx, "clevercloud", Creds{Token: "test-token"}, ""); err == nil {
		t.Fatal("Clever Cloud ne doit plus exister")
	}
}
