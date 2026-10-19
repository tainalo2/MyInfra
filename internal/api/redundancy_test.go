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
