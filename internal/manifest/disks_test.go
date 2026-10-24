package manifest

import (
	"strings"
	"testing"

	"myinfra/internal/spec"
)

// One claim for a shared disk, mounted by every service that names it, with
// their pods held together on the node of that claim.
func TestSharedDiskIsOneClaim(t *testing.T) {
	shared := spec.Volume{Path: "/data", SizeGi: 200, Disk: "media"}
	app := App{ID: specApp, Hosts: []string{"media.example.org"}, Services: []Service{
		{Name: "jellyfin", Image: "jellyfin/jellyfin:10.10.7", Published: true, Listen: 8096, Disk: true, DiskPath: "/config", DiskGi: 8, Spec: spec.Service{Volumes: []spec.Volume{shared}}},
		{Name: "sonarr", Image: "linuxserver/sonarr:4.0.20", Listen: 8989, Replicas: 3, Disk: true, DiskPath: "/config", DiskGi: 4, Spec: spec.Service{Volumes: []spec.Volume{shared}}},
	}}
	if reason := Refusal(app); reason != "" {
		t.Fatal(reason)
	}
	raw := Render(app)
	docs := documents(t, raw)
	claim := docs["PersistentVolumeClaim/disk-media"]
	if claim == nil || dig(claim, "spec", "resources", "requests", "storage") != "200Gi" {
		t.Fatal(raw)
	}
	if strings.Count(raw, "name: disk-media\n") != 1 {
		t.Fatal("claim écrite plusieurs fois")
	}
	for _, name := range []string{"jellyfin", "sonarr"} {
		deploy := docs["Deployment/"+name]
		if deploy == nil {
			t.Fatalf("%s n'est pas un Deployment\n%s", name, raw)
		}
		if dig(deploy, "spec", "replicas") != 1 {
			t.Fatal(name, dig(deploy, "spec", "replicas"))
		}
		volumes := dig(deploy, "spec", "template", "spec", "volumes").([]any)
		found := false
		for _, v := range volumes {
			if dig(v, "persistentVolumeClaim", "claimName") == "disk-media" {
				found = true
			}
		}
		if !found {
			t.Fatal(name, volumes)
		}
		if dig(deploy, "spec", "template", "metadata", "labels", "myinfra.io/disk-media") != "true" {
			t.Fatal(name, "étiquette")
		}
		term := dig(deploy, "spec", "template", "spec", "affinity", "podAffinity", "requiredDuringSchedulingIgnoredDuringExecution", 0)
		if dig(term, "topologyKey") != "kubernetes.io/hostname" || dig(term, "labelSelector", "matchLabels", "myinfra.io/disk-media") != "true" {
			t.Fatal(name, term)
		}
	}
	if docs["StatefulSet/sonarr"] != nil || docs["PersistentVolumeClaim/sonarr.v1"] != nil {
		t.Fatal("copies ou disque propre pour un disque partagé")
	}
}

// Each public service is routed on its own names; the redirection is
// written once for the application.
func TestEachPublicServiceIsRoutedOnItsNames(t *testing.T) {
	app := App{ID: specApp, Hosts: []string{"media.example.org", "series.example.org"}, Zones: map[string]string{"media.example.org": "example.org", "series.example.org": "example.org"}, Services: []Service{
		{Name: "jellyseerr", Image: "fallenbagel/jellyseerr:2.7.3", Published: true, Listen: 5055, Hosts: []string{"media.example.org"}},
		{Name: "sonarr", Image: "linuxserver/sonarr:4.0.20", Published: true, Listen: 8989, Hosts: []string{"series.example.org"}},
		{Name: "prowlarr", Image: "linuxserver/prowlarr:1.32.2", Published: true, Listen: 9696, Hosts: []string{}},
	}}
	raw := Render(app)
	docs := documents(t, raw)
	if strings.Count(raw, "kind: Middleware") != 1 {
		t.Fatal("redirection écrite plusieurs fois")
	}
	routes := map[string]string{}
	for key, doc := range docs {
		if !strings.HasPrefix(key, "IngressRoute/") || strings.Contains(key, "-http") {
			continue
		}
		route := dig(doc, "spec", "routes", 0)
		routes[dig(route, "services", 0, "name").(string)] = dig(route, "match").(string)
	}
	if routes["jellyseerr"] != "Host(`media.example.org`)" || routes["sonarr"] != "Host(`series.example.org`)" {
		t.Fatal(routes)
	}
	if _, ok := routes["prowlarr"]; ok {
		t.Fatal("un service public sans nom reçoit une route")
	}
}

func TestSharedDiskStaysOnItsNode(t *testing.T) {
	app := App{ID: specApp, Services: []Service{{Name: "jellyfin", Image: "jellyfin/jellyfin:10.10.7", Listen: 8096, Node: "sw4", Spec: spec.Service{Volumes: []spec.Volume{{Path: "/data", SizeGi: 20, Disk: "media", Node: "id"}}}}}}
	docs := documents(t, Render(app))
	if dig(docs["Deployment/jellyfin"], "spec", "template", "spec", "nodeSelector", "kubernetes.io/hostname") != "sw4" {
		t.Fatal(Render(app))
	}
}
