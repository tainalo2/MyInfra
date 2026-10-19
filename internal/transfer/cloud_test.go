package transfer

import "strings"
import "testing"

func TestCloudConfig(t *testing.T) {
	got, err := CloudConfig("ghcr.io/acme/myinfra:1.2.3", "bWFzdGVy", "secret-pass", "boot-token", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"#cloud-config", "ghcr.io/acme/myinfra:1.2.3", "bWFzdGVy", "secret-pass", "boot-token", "postgres:16", "http://auto"} {
		if !strings.Contains(got, want) {
			t.Fatalf("manque %s\n%s", want, got)
		}
	}
	if strings.Contains(got, "docker/config.json") {
		t.Fatal("connexion au registre sans identifiants")
	}
	withRegistry, err := CloudConfig("ghcr.io/acme/myinfra:1.2.3", "bWFzdGVy", "secret-pass", "boot-token", "rg.fr-par.scw.cloud", "nologin", "secret-token")
	if err != nil || !strings.Contains(withRegistry, "docker/config.json") || strings.Contains(withRegistry, "secret-token") {
		t.Fatal("identifiants du registre")
	}
	if _, err := CloudConfig("mauvaise image", "a", "b", "c", "", "", ""); err == nil {
		t.Fatal("image acceptée")
	}
}
