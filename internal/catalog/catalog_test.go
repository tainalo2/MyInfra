package catalog

import "testing"

func TestBaseOffer(t *testing.T) {
	cat := sampleDigitalOcean("")
	offer, err := cat.Base()
	if err != nil {
		t.Fatal(err)
	}
	if offer.ID != "s-2vcpu-2gb" || offer.MemoryMB != 2048 {
		t.Fatalf("offer %+v", offer)
	}
}

func TestParseHetzner(t *testing.T) {
	types := []byte(`{"server_types":[{"name":"cx23","cores":2,"memory":4,"disk":40,"prices":[{"location":"fsn1","price_hourly":{"net":"0.0088"},"price_monthly":{"net":"5.4900"}},{"location":"hel1","price_hourly":{"net":"0.0088"},"price_monthly":{"net":"5.4900"}}]},{"name":"cpx11","cores":2,"memory":2,"disk":40,"locations":[{"name":"fsn1","deprecation":{"unavailable_after":"2024-01-01"}},{"name":"ash"}],"prices":[{"location":"fsn1","price_hourly":{"net":"0.0080"},"price_monthly":{"net":"4.9900"}},{"location":"ash","price_hourly":{"net":"0.0080"},"price_monthly":{"net":"4.9900"}}]},{"name":"old","deprecated":true,"cores":1,"memory":1,"disk":20,"prices":[{"location":"fsn1","price_hourly":{"net":"1"},"price_monthly":{"net":"1"}}]}]}`)
	prices := []byte(`{"pricing":{"volume":{"price_per_gb_monthly":{"net":"0.0440"}}}}`)
	cat, err := parseHetzner(types, prices, "hel1")
	if err != nil {
		t.Fatal(err)
	}
	if cat.Location != "hel1" || len(cat.Offers) != 1 {
		t.Fatalf("catalog %+v", cat)
	}
	if cat.Offers[0].Name != "CX23" || cat.Offers[0].Monthly != "5.49" || cat.Offers[0].DiskGB != 40 {
		t.Fatalf("offer %+v", cat.Offers[0])
	}
	fsn, err := parseHetzner(types, prices, "fsn1")
	if err != nil || len(fsn.Offers) != 1 || fsn.Offers[0].Name != "CX23" {
		t.Fatalf("falkenstein %+v %v", fsn.Offers, err)
	}
	ash, err := parseHetzner(types, prices, "ash")
	if err != nil || len(ash.Offers) != 1 || ash.Offers[0].Name != "CPX11" {
		t.Fatalf("ashburn %+v %v", ash.Offers, err)
	}
	if len(cat.Offers[0].Storage) != 1 || cat.Offers[0].Storage[0].MinGB != 10 || cat.Offers[0].Storage[0].MonthlyPerGB != "0.044" {
		t.Fatalf("storage %+v", cat.Offers[0].Storage)
	}
	plan, err := cat.Plan("cx23", "month", "volume", 20)
	if err != nil || plan.StorageGB != 20 || plan.Name != "CX23" {
		t.Fatalf("plan %+v %v", plan, err)
	}
	if _, err := cat.Plan("cx23", "month", "volume", 5); err == nil {
		t.Fatal("taille trop petite acceptée")
	}
}

func TestParseScaleway(t *testing.T) {
	body := []byte(`{"servers":{"DEV1-S":{"hourly_price":0.01,"ncpus":2,"ram":2147483648,"gpu":null,"volumes_constraint":{"min_size":20000000000,"max_size":20000000000},"per_volume_constraint":{"l_ssd":{"min_size":1000000000,"max_size":1000000000000}}},"L4-1-24G":{"hourly_price":0.79,"ncpus":8,"ram":51539607552,"gpu":1,"volumes_constraint":{"min_size":0,"max_size":0}},"METAL":{"hourly_price":1,"ncpus":8,"ram":1,"baremetal":true}}}`)
	cat, err := parseScaleway(body, "fr-par-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(cat.Offers) != 2 || cat.Offers[0].DiskGB != 20 || cat.Offers[0].Monthly != "7.3" || cat.Offers[0].GPUs != 0 {
		t.Fatalf("offer %+v", cat.Offers)
	}
	if cat.Offers[1].ID != "L4-1-24G" || cat.Offers[1].GPUs != 1 || cat.Offers[1].GPU != "L4" {
		t.Fatalf("gpu %+v", cat.Offers[1])
	}
	if len(cat.Offers[0].Storage) != 1 || cat.Offers[0].Storage[0].ID != "l_ssd" {
		t.Fatalf("storage %+v", cat.Offers[0].Storage)
	}
}

func TestParseOVH(t *testing.T) {
	body := []byte(`{"addons":[
		{"planCode":"b3-8.consumption","invoiceName":"b3-8","product":"publiccloud-instance","pricings":[{"intervalUnit":"hour","price":5120000}],"blobs":{"technical":{"cpu":{"cores":2},"memory":{"size":8},"storage":{"disks":[{"capacity":50,"number":1}]}}}},
		{"planCode":"d2-4.consumption","invoiceName":"d2-4","product":"publiccloud-instance","pricings":[{"intervalUnit":"hour","price":2800000}],"blobs":{"technical":{"cpu":{"cores":2},"memory":{"size":4},"storage":{"disks":[{"capacity":50,"number":1}]}}}},
		{"planCode":"d2-4.monthly.postpaid","invoiceName":"d2-4","product":"publiccloud-instance","pricings":[{"intervalUnit":"month","price":1240000000}]},
		{"planCode":"win-b3-8.consumption","invoiceName":"win-b3-8","product":"publiccloud-instance","pricings":[{"intervalUnit":"hour","price":1}],"blobs":{"technical":{"cpu":{"cores":2}}}},
		{"planCode":"l4-90.consumption","invoiceName":"l4-90","product":"publiccloud-instance","pricings":[{"intervalUnit":"hour","price":100000000}],"blobs":{"technical":{"cpu":{"cores":22},"memory":{"size":90},"gpu":{"number":1,"model":"L4"},"storage":{"disks":[{"capacity":400,"number":1}]}}}},
		{"planCode":"volume.classic.consumption","invoiceName":"volume.classic","product":"publiccloud-storage","pricings":[{"intervalUnit":"none","price":5900}]},
		{"planCode":"volume.classic.monthly.postpaid","invoiceName":"volume.classic","product":"publiccloud-storage","pricings":[{"intervalUnit":"month","price":4200000}]}
	]}`)
	cat, err := parseOVH(body, "SBG")
	if err != nil {
		t.Fatal(err)
	}
	if cat.Location != "SBG" || len(cat.Offers) != 3 {
		t.Fatalf("offers %d location %s", len(cat.Offers), cat.Location)
	}
	var d2 Offer
	for _, offer := range cat.Offers {
		if offer.Name == "d2-4" {
			d2 = offer
		}
	}
	if d2.MonthlyID != "d2-4.monthly.postpaid" || d2.Monthly != "12.4" || d2.DiskGB != 50 || d2.GPUs != 0 {
		t.Fatalf("d2 %+v", d2)
	}
	var l4 Offer
	for _, offer := range cat.Offers {
		if offer.Name == "l4-90" {
			l4 = offer
		}
	}
	if l4.GPUs != 1 || l4.GPU != "L4" {
		t.Fatalf("l4 %+v", l4)
	}
	if len(d2.Storage) != 1 || d2.Storage[0].Label != "Volume Classic" {
		t.Fatalf("storage %+v", d2.Storage)
	}
	plan, err := cat.Plan(d2.ID, "month", "", 0)
	if err != nil || plan.OfferID != "d2-4.monthly.postpaid" {
		t.Fatalf("plan %+v %v", plan, err)
	}
}

func TestMergeOVHKeepsLiveMachines(t *testing.T) {
	offers := []Offer{
		{ID: "s1-8.consumption", Name: "s1-8", VCPU: 2, MemoryMB: 8192, DiskGB: 40, Hourly: "0.04", Storage: []Storage{{ID: "volume.classic.consumption", Label: "Volume Classic"}}},
		{ID: "b3-8.consumption", MonthlyID: "b3-8.monthly.postpaid", Name: "b3-8", VCPU: 2, MemoryMB: 7000, DiskGB: 40, Hourly: "0.05", Monthly: "30"},
		{ID: "bm-8.consumption", Name: "bm-8", VCPU: 2, MemoryMB: 8192, DiskGB: 100, Hourly: "0.2"},
	}
	flavors := []LiveFlavor{
		{Name: "b3-8", Type: "ovh.ssd.eg", OSType: "linux", Available: true, VCPU: 2, RAM: 8, Disk: 50, Hourly: "b3-8.consumption"},
		{Name: "s1-8", Type: "ovh.ssd.eg", OSType: "linux", Available: false, VCPU: 2, RAM: 8, Disk: 40, Hourly: "s1-8.consumption"},
		{Name: "bm-8", Type: "baremetal", OSType: "linux", Available: true, VCPU: 2, RAM: 8, Disk: 100, Hourly: "bm-8.consumption"},
	}
	got := MergeOVH(offers, flavors)
	if len(got) != 1 || got[0].Name != "b3-8" || got[0].DiskGB != 50 || got[0].MemoryMB != 8192 || got[0].Hourly != "0.05" {
		t.Fatalf("%+v", got)
	}
	if len(got[0].Storage) != 1 {
		t.Fatal(got[0].Storage)
	}
}

func TestParseDigitalOcean(t *testing.T) {
	sizes := []doSize{
		{Slug: "s-1vcpu-1gb", Available: true, Vcpus: 1, Memory: 1024, Disk: 25, PriceHourly: 0.00744, PriceMonthly: 5, Regions: []string{"ams3", "fra1"}},
		{Slug: "gone", Available: false, Vcpus: 1, Memory: 1024, Disk: 25, PriceHourly: 1, PriceMonthly: 1, Regions: []string{"ams3"}},
		{Slug: "nyc", Available: true, Vcpus: 2, Memory: 2048, Disk: 50, PriceHourly: 0.02, PriceMonthly: 12, Regions: []string{"nyc1"}},
		{Slug: "gpu-l4x1", Available: true, Vcpus: 8, Memory: 32768, Disk: 500, PriceHourly: 0.76, PriceMonthly: 500, Regions: []string{"ams3"}, GPU: &struct {
			Count int    `json:"count"`
			Model string `json:"model"`
		}{Count: 1, Model: "L4"}},
	}
	regions := []byte(`{"regions":[{"slug":"ams3","name":"Amsterdam 3","available":true},{"slug":"nyc1","name":"New York 1","available":true},{"slug":"old","name":"Old","available":false}]}`)
	cat, err := parseDigitalOcean(sizes, regions, "ams3")
	if err != nil {
		t.Fatal(err)
	}
	if cat.Currency != "USD" || cat.Location != "ams3" || len(cat.Offers) != 2 {
		t.Fatalf("catalog %+v", cat)
	}
	offer := cat.Offers[0]
	if offer.Name != "s-1vcpu-1gb" || offer.Monthly != "5" || offer.DiskGB != 25 || offer.GPUs != 0 {
		t.Fatalf("offer %+v", offer)
	}
	if cat.Offers[1].GPUs != 1 || cat.Offers[1].GPU != "L4" {
		t.Fatalf("gpu %+v", cat.Offers[1])
	}
	if len(offer.Storage) != 1 || offer.Storage[0].MonthlyPerGB != "0.1" || offer.Storage[0].MaxGB != 16384 {
		t.Fatalf("storage %+v", offer.Storage)
	}
	plan, err := cat.Plan(offer.ID, "month", "volume", 20)
	if err != nil || plan.Currency != "USD" || plan.StorageGB != 20 {
		t.Fatalf("plan %+v %v", plan, err)
	}
}

func TestParseInfomaniak(t *testing.T) {
	body := []byte(`{"flavors":[
		{"id":"a1","name":"a1-ram2-disk20-perf1","ram":2048,"disk":20,"vcpus":1},
		{"id":"off","name":"old","ram":2048,"disk":20,"vcpus":1,"OS-FLV-DISABLED:disabled":true},
		{"id":"priv","name":"private","ram":2048,"disk":20,"vcpus":1,"os-flavor-access:is_public":false}
	]}`)
	flavors, err := parseInfomaniakFlavors(body)
	if err != nil {
		t.Fatal(err)
	}
	if len(flavors) != 1 || flavors[0].Disk != 20 {
		t.Fatalf("flavors %+v", flavors)
	}
	locations := []Location{{ID: "dc3-a", Label: "Genève"}}
	cat := infomaniakCatalog(locations, "dc3-a", flavors, map[string]string{"a1": "0.02"})
	if cat.Offers[0].Hourly != "0.02" || cat.Offers[0].Monthly != "14.6" || cat.Currency != "EUR" {
		t.Fatalf("offer %+v", cat.Offers[0])
	}
	if ratingMoney([]byte(`"0.030000"`)) != "0.03" {
		t.Fatal(ratingMoney([]byte(`"0.030000"`)))
	}
}
