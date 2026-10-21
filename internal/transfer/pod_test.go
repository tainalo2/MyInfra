package transfer

import "strings"
import "testing"

func TestPodManifest(t *testing.T) {
	got, err := PodManifest(PodSpec{
		Image: "ghcr.io/acme/myinfra:1.2.3", MasterKey: "bWFzdGVy",
		BootstrapToken: "boot-token", PublicURL: "https://myinfra.exemple.fr", Host: "myinfra.exemple.fr", Instances: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"name: myinfra", "ghcr.io/acme/myinfra:1.2.3", "bWFzdGVy", "boot-token",
		"kind: Cluster", "instances: 2", "podAntiAffinityType: required", "name: database-app", "key: uri", "pg_isready -q -h database-rw -p 5432",
		"https://myinfra.exemple.fr", "Host(`myinfra.exemple.fr`)", "certResolver: exemple-fr",
		"node-role.kubernetes.io/control-plane", "DoesNotExist", "imagePullPolicy: Always",
		"replicas: 2", "podAntiAffinity", "topologySpreadConstraints", "kind: PodDisruptionBudget", "minAvailable: 1",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("manque %s", want)
		}
	}
	// The database lives on no node's disk alone.
	if strings.Contains(got, "PersistentVolumeClaim") || strings.Contains(got, "postgres:16") {
		t.Fatal(got)
	}
	if strings.Contains(got, "MISSING") || strings.Contains(got, "%s") || strings.Contains(got, "%!") {
		t.Fatal("argument de manifeste manquant")
	}
	if strings.Contains(got, "myinfra.io/managed") {
		t.Fatal("le namespace serait repris par la synchronisation des applications")
	}
	if _, err := PodManifest(PodSpec{Image: "mauvaise image", MasterKey: "a", BootstrapToken: "c", PublicURL: "https://myinfra.exemple.fr", Host: "myinfra.exemple.fr"}); err == nil {
		t.Fatal("image acceptée")
	}
}

func TestPodHost(t *testing.T) {
	if PodHost("exemple.fr") != "myinfra.exemple.fr" {
		t.Fatal(PodHost("exemple.fr"))
	}
	if PodHost("") != "" || PodHost("apps.localhost") != "" || PodHost("localhost") != "" {
		t.Fatal("domaine local accepté")
	}
}
