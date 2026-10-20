package api

import (
	"strings"
	"testing"

	"myinfra/internal/imagefile"
)

func TestLocalRefNamesAnImage(t *testing.T) {
	archive := &imagefile.Archive{Digest: "sha256:0123456789abcdef0123", Tags: []string{"ghcr.io/me/web-app:1.4.2"}}
	cases := []struct {
		name, tag, want string
	}{
		{"", "", "myinfra.local/web-app:1.4.2"},
		{"site", "", "myinfra.local/site:1.4.2"},
		{"site", "2.0", "myinfra.local/site:2.0"},
		// latest would make Kubernetes pull it: the digest names it instead.
		{"site", "latest", "myinfra.local/site:sha-0123456789ab"},
	}
	for _, c := range cases {
		got, err := localRef(c.name, c.tag, archive)
		if err != nil || got != c.want {
			t.Fatalf("%q %q : %q %v, attendu %q", c.name, c.tag, got, err, c.want)
		}
	}
	if _, err := localRef("Mon Site", "", archive); err == nil {
		t.Fatal("un nom avec espace et majuscules a été accepté")
	}
	if _, err := localRef("", "", &imagefile.Archive{Digest: "sha256:0123456789abcdef"}); err == nil {
		t.Fatal("une image sans nom a été acceptée")
	}
}

func TestRegistryHostOf(t *testing.T) {
	cases := map[string]string{
		"registry.exemple.fr":                 "registry.exemple.fr",
		"https://registry.exemple.fr/v2/":     "registry.exemple.fr",
		"Registry.Exemple.fr:5000":            "registry.exemple.fr:5000",
		"harbor.interne.lan/projet/app:1.0.0": "harbor.interne.lan",
	}
	for in, want := range cases {
		got, err := registryHostOf("registry-custom", in)
		if err != nil || got != want {
			t.Fatalf("%q : %q %v, attendu %q", in, got, err, want)
		}
	}
	if got, _ := registryHostOf("registry-ghcr", ""); got != "ghcr.io" {
		t.Fatalf("ghcr : %q", got)
	}
	for _, bad := range []string{"", "localhost", "pas une adresse", "exemple.fr'; rm"} {
		if _, err := registryHostOf("registry-custom", bad); err == nil {
			t.Fatalf("%q accepté", bad)
		}
	}
}

func TestImageHostSkipsLocalImages(t *testing.T) {
	if imageHost("myinfra.local/site:1.0") != "" {
		t.Fatal("une image importée a un registre")
	}
	if imageHost("nginx:1.27") != "docker.io" || imageHost("ghcr.io/me/app:2") != "ghcr.io" {
		t.Fatal("registre mal lu")
	}
	if !strings.HasPrefix(imagefile.LocalPrefix, "myinfra.local") {
		t.Fatal("préfixe changé")
	}
}
