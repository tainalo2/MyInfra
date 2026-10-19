package provision

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"myinfra/internal/catalog"
	"myinfra/internal/transfer"
)

func TestJSONStacksParse(t *testing.T) {
	if _, err := exec.LookPath("tofu"); err != nil {
		t.Skip("tofu absent")
	}
	user, err := transfer.CloudConfig("ghcr.io/acme/myinfra:1", "master-key", "db-pass", "boot-token", "ghcr.io", "user", "secret \"pass\"")
	if err != nil {
		t.Fatal(err)
	}
	user += "\n# ${not interpolation}\nline \"quoted\" \\ end"
	specs := []struct {
		provider string
		creds    catalog.Creds
		spec     Spec
	}{
		{"hetzner", catalog.Creds{Token: "secret-token"}, Spec{Name: "web", OfferID: "cx22", Location: "fsn1", UserData: user}},
		{"scaleway", catalog.Creds{Token: "secret-token", ProjectID: "proj"}, Spec{Name: "web", OfferID: "DEV1-S", Location: "fr-par-1", UserData: user}},
		{"digitalocean", catalog.Creds{Token: "secret-token"}, Spec{Name: "web", OfferID: "s-1vcpu-1gb", Location: "ams3", UserData: user}},
		{"ovh", catalog.Creds{ApplicationKey: "app-key-ovh", ApplicationSecret: "app-secret-ovh", ConsumerKey: "consumer-ovh", ProjectID: "p"}, Spec{Name: "web", OfferID: "b2-7", Location: "GRA11", UserData: user}},
		{"upcloud", catalog.Creds{Token: "secret-token"}, Spec{Name: "web", OfferID: "1xCPU-1GB", Location: "fi-hel1", UserData: user}},
		{"vultr", catalog.Creds{Token: "secret-token"}, Spec{Name: "web", OfferID: "vc2-1c-1gb", Location: "ams", UserData: user}},
		{"akamai", catalog.Creds{Token: "secret-token"}, Spec{Name: "web", OfferID: "g6-nanode-1", Location: "fr-par", UserData: user}},
		{"exoscale", catalog.Creds{ApplicationKey: "exo-key", ApplicationSecret: "exo-secret"}, Spec{Name: "web", OfferID: "standard.small", Location: "ch-gva-2", UserData: user}},
		{"gcore", catalog.Creds{Token: "secret-token"}, Spec{Name: "web", OfferID: "g1-standard-1-2", Location: "1"}},
		{"kamatera", catalog.Creds{ApplicationKey: "kama-key", ApplicationSecret: "kama-secret"}, Spec{Name: "web", OfferID: "ubuntu", Location: "EU"}},
		{"ionos", catalog.Creds{Token: "secret-token"}, Spec{Name: "web", OfferID: "vcpu-1-2", Location: "de/fra"}},
		{"openstack", catalog.Creds{Token: "https://cloud.example/identity", ApplicationKey: "os-id", ApplicationSecret: "os-secret"}, Spec{Name: "web", OfferID: "m1.small", Location: "RegionOne", UserData: user}},
		{"infomaniak", catalog.Creds{ApplicationKey: "ik-id", ApplicationSecret: "ik-secret"}, Spec{Name: "web", OfferID: "a1-ram2-disk20-perf1", Location: "dc3-a", UserData: user}},
		{"proxmox", catalog.Creds{Token: "secret-token", ApplicationKey: "https://pve.example:8006"}, Spec{Name: "web", OfferID: "100", Location: "pve"}},
	}
	for _, item := range specs {
		body, _, err := renderStack(item.provider, item.creds, item.spec, Client{})
		if err != nil {
			t.Fatalf("%s: %v", item.provider, err)
		}
		if strings.Contains(body, "secret-token") || strings.Contains(body, "app-secret-ovh") || strings.Contains(body, "exo-secret") || strings.Contains(body, "kama-secret") || strings.Contains(body, "os-secret") || strings.Contains(body, "ik-secret") {
			t.Fatalf("%s contient un secret", item.provider)
		}
		dir := t.TempDir()
		path := filepath.Join(dir, "main.tf.json")
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command("tofu", "validate")
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err == nil || !strings.Contains(string(out), "Missing required provider") || strings.Contains(string(out), "Invalid") {
			t.Fatalf("%s: %v\n%s", item.provider, err, out)
		}
	}
}
