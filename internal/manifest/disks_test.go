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
