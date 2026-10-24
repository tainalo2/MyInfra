package agent

import (
	"strings"
	"testing"

	"myinfra/internal/spec"
)

// On a machine, the services that share a disk mount one Docker volume.
func TestSharedDiskIsOneVolume(t *testing.T) {
	shared := spec.Volume{Path: "/data", SizeGi: 200, Disk: "media"}
	script, err := Sync("http://127.0.0.1:8080", "tok_en", []Workload{
		{ID: specApp, Service: "jellyfin", Image: "jellyfin/jellyfin:10.10.7", Published: true, Listen: 8096, Spec: spec.Service{Volumes: []spec.Volume{shared}}},
		{ID: specApp, Service: "sonarr", Image: "linuxserver/sonarr:4.0.20", Listen: 8989, Spec: spec.Service{Volumes: []spec.Volume{shared}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	mount := " -v 'myinfra-data-7d49736b58e74386b947720823531792-disk-media:/data'"
	if strings.Count(script, mount) < 2 {
		t.Fatalf("volume commun absent\n%s", script)
	}
	if strings.Contains(script, "-jellyfin-v1") || strings.Contains(script, "-sonarr-v1") {
		t.Fatal("volume propre pour un disque partagé")
	}
}

// Two public services of one application have routers of their own.
func TestPublicServicesHaveTheirOwnRouters(t *testing.T) {
	script, err := Sync("http://127.0.0.1:8080", "tok_en", []Workload{
		{ID: specApp, Service: "jellyseerr", Image: "fallenbagel/jellyseerr:2.7.3", Published: true, Listen: 5055, Hosts: []string{"media.example.org"}},
		{ID: specApp, Service: "sonarr", Image: "linuxserver/sonarr:4.0.20", Published: true, Listen: 8989, Hosts: []string{"series.example.org"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	first := routerName(specApp + "jellyseerr" + "le")
	routers := map[string]bool{}
	for _, line := range strings.Split(script, "traefik.http.routers.") {
		if name, _, ok := strings.Cut(line, "."); ok && strings.HasPrefix(name, "app") && !strings.HasSuffix(name, "-http") {
			routers[name] = true
		}
	}
	if len(routers) != 2 {
		t.Fatalf("%d routeurs : %v (%s)", len(routers), routers, first)
	}
}
