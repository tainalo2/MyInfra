package api

import (
	"testing"

	"myinfra/internal/store"
)

func TestRedundancySortsServices(t *testing.T) {
	plan := redundancyPlan([]store.Service{
		{Name: "web", Image: "nginx:1.27"},
		{Name: "uptime-kuma", Image: "louislam/uptime-kuma:2.5.6", Disk: true},
		{Name: "vikunja", Image: "vikunja/vikunja:latest", Disk: true},
		{Name: "data", Image: "acme/thing:1", Disk: true},
	})
	if len(plan.Scaled) != 1 || plan.Failover[0] != "uptime-kuma" || plan.Database[0] != "vikunja" || plan.Alone[0] != "data" {
		t.Fatalf("%+v", plan)
	}
	if distinctLines("node-a\nnode-a\n\n") != 1 || distinctLines("a\nb") != 2 {
		t.Fatal("placement")
	}
}

func TestShapeServicesAtCreation(t *testing.T) {
	services := []store.Service{
		{Name: "web", Image: "nginx:1.27"},
		{Name: "uptime-kuma", Image: "louislam/uptime-kuma:2.5.6", Disk: true},
		{Name: "vikunja", Image: "vikunja/vikunja:latest", Disk: true},
		{Name: "data", Image: "acme/thing:1", Disk: true, Replicas: 3},
	}
	if err := shapeServices(services, 3, 6); err != nil {
		t.Fatal(err)
	}
	want := []struct {
		replicas         int
		backup, database string
	}{{3, "", ""}, {1, "ready", ""}, {3, "", "ready"}, {1, "", ""}}
	for i, w := range want {
		got := services[i]
		if got.Replicas != w.replicas || got.Backup != w.backup || got.Database != w.database {
			t.Fatalf("%s : %+v", got.Name, got)
		}
	}
	if shapeServices([]store.Service{{Name: "web", Image: "nginx"}}, 4, 3) == nil {
		t.Fatal("plus de copies que de serveurs")
	}
	if shapeServices([]store.Service{{Name: "k", Image: "louislam/uptime-kuma:2", Disk: true}}, 2, 2) == nil {
		t.Fatal("la sauvegarde demande trois serveurs")
	}
	one := []store.Service{{Name: "data", Image: "acme/thing:1", Disk: true, Replicas: 3}}
	_ = shapeServices(one, 1, 0)
	if one[0].Replicas != 1 {
		t.Fatal("sans redondance, un disque garde une copie")
	}
}
