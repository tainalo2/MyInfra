package api

import (
	"strings"
	"testing"

	"myinfra/internal/pack"
	"myinfra/internal/spec"
	"myinfra/internal/store"
)

func sharing(name string) store.Service {
	return store.Service{Name: name, Image: "img", Listen: 80, Replicas: 1, Spec: spec.Service{Volumes: []spec.Volume{{Path: "/data", SizeGi: 200, Disk: "media"}}}}
}

// A shared disk stays while a service mounts it, and goes with the last.
func TestSharedDiskGoesWithTheLastService(t *testing.T) {
	current := []store.Service{sharing("jellyfin"), sharing("sonarr")}
	next := []store.Service{sharing("jellyfin")}
	change, err := planServices(current, next, make([]pack.Service, len(next)))
	if err != nil {
		t.Fatal(err)
	}
	for _, claim := range change.claims {
		if strings.Contains(claim.name, "disk-") {
			t.Fatal("disque partagé effacé alors qu'il sert encore", change.claims)
		}
	}
	change, err = planServices(current, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, claim := range change.claims {
		if claim.name == "disk-media" {
			found = true
		}
	}
	if !found || !strings.Contains(strings.Join(change.lost, ","), "disque partagé media") {
		t.Fatal(change)
	}
	// A volume does not move from one disk to another in place.
	moved := sharing("jellyfin")
	moved.Spec.Volumes[0].Disk = "films"
	if _, err := planServices(current[:1], []store.Service{moved}, make([]pack.Service, 1)); err == nil {
		t.Fatal("changement de disque accepté")
	}
}

func TestSharedDiskServiceIsNotRedundant(t *testing.T) {
	plan := redundancyPlan([]store.Service{sharing("jellyfin"), {Name: "cache", Image: "redis:7", Listen: 6379}})
	if strings.Join(plan.Alone, ",") != "jellyfin" || strings.Join(plan.Scaled, ",") != "cache" {
		t.Fatal(plan)
	}
}

func TestExportedPackageDeclaresSharedDisks(t *testing.T) {
	p := appPackage(store.Application{Name: "media", Services: []store.Service{sharing("jellyfin"), sharing("sonarr")}})
	if len(p.Disks) != 1 || p.Disks[0] != (pack.SharedDisk{Name: "media", SizeGi: 200}) {
		t.Fatal(p.Disks)
	}
	if p.Services[0].Volumes[0].SizeGi != 0 {
		t.Fatal(p.Services[0].Volumes)
	}
	p.Services[0].Public = true
	if problems := pack.Check(p); len(problems) > 0 {
		t.Fatal(problems)
	}
}
