package market

import (
	"strings"
	"testing"
	"testing/fstest"

	"myinfra/internal/pack"
)

func TestCatalogueIsInstallable(t *testing.T) {
	if _, err := load(files); err != nil {
		t.Fatal(err)
	}
	want := []string{Sites, Collaboration, Code, Identity, Files, Automation, Exchange, Notes, Accounts, Media, Tools}
	seen := map[string]bool{}
	for _, group := range Groups() {
		seen[group.Name] = true
		if len(group.Articles) == 0 {
			t.Fatal(group.Name)
		}
	}
	for _, name := range want {
		if !seen[name] {
			t.Fatal(name)
		}
	}
	if Logo("ghost:5-alpine") != "ghost" || Logo("lissy93/dashy:3.1.1") != "dashy" || Logo("nginx:alpine") != "" {
		t.Fatal(Logo("ghost:5-alpine"), Logo("lissy93/dashy:3.1.1"), Logo("nginx:alpine"))
	}
	if len(articles) < 80 {
		t.Fatal(len(articles))
	}
	card, ok := CardOf("wordpress")
	if !ok || card.Site != "https://wordpress.org" || card.Logo != "wordpress" || len(card.Services) != 2 || card.Description == "" {
		t.Fatalf("%+v", card)
	}
}

func TestExpandSharesTheDatabaseSecret(t *testing.T) {
	services, env, err := Expand("wordpress", "blog.exemple.fr", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(services) != 2 || !services[0].Public || services[1].Name != "mariadb" || services[1].Port != 3306 || services[0].Volumes[0].Path != "/var/www/html" {
		t.Fatalf("%+v", services)
	}
	if env["WORDPRESS_DB_PASSWORD"] == "" || env["WORDPRESS_DB_PASSWORD"] != env["MARIADB_PASSWORD"] || env["MARIADB_ROOT_PASSWORD"] == env["MARIADB_PASSWORD"] {
		t.Fatal(env)
	}
	if env["WORDPRESS_DB_HOST"] != "mariadb" {
		t.Fatal(env)
	}
}

func TestExpandAsksForTheContact(t *testing.T) {
	if _, _, err := Expand("listmonk", "listes.exemple.fr", nil); err == nil {
		t.Fatal("expected error")
	}
	_, env, err := Expand("listmonk", "listes.exemple.fr", map[string]string{"email": "ada@exemple.fr"})
	if err != nil {
		t.Fatal(err)
	}
	// The address stays a token, filled at each delivery with the name then.
	if env["LISTMONK_ADMIN_USER"] != "ada@exemple.fr" || env["LISTMONK_app__address"] != "{{url}}" {
		t.Fatal(env)
	}
	if got := pack.FillAddress(env["LISTMONK_app__address"], "listes.exemple.fr"); got != "https://listes.exemple.fr" {
		t.Fatal(got)
	}
	if got := pack.FillAddress("{{host}}:{{host}}", "a.fr"); got != "a.fr:a.fr" {
		t.Fatal(got)
	}
}

// A catalogue that does not hold together is refused with what to fix.
func TestLoadRefusesABrokenCatalogue(t *testing.T) {
	good, err := files.ReadFile("catalogue/umami.yaml")
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name  string
		fsys  fstest.MapFS
		where string
	}{
		{"unlisted", fstest.MapFS{"catalogue/order.txt": {Data: []byte("")}, "catalogue/umami.yaml": {Data: good}}, "manque dans order.txt"},
		{"missing", fstest.MapFS{"catalogue/order.txt": {Data: []byte("umami\nghost\n")}, "catalogue/umami.yaml": {Data: good}}, "ghost.yaml est absent"},
		{"invalid", fstest.MapFS{"catalogue/order.txt": {Data: []byte("umami\n")}, "catalogue/umami.yaml": {Data: []byte(strings.Replace(string(good), "port: 3000", "port: 99999", 1))}}, "ligne"},
		{"untranslated", fstest.MapFS{"catalogue/order.txt": {Data: []byte("umami\n")}, "catalogue/umami.yaml": {Data: []byte(strings.Replace(string(good), "    de:", "    xx:", 1))}}, "translations"},
		{"unread", fstest.MapFS{"catalogue/order.txt": {Data: []byte("umami\n")}, "catalogue/umami.yaml": {Data: []byte(strings.Replace(string(good), "services:", "inputs:\n  - key: unused\n    label: Inutile\nservices:", 1))}}, "unused"},
	}
	for _, c := range cases {
		_, err := load(c.fsys)
		if err == nil || !strings.Contains(err.Error(), c.where) {
			t.Fatalf("%s: %v", c.name, err)
		}
	}
}

// A copy given out does not change the catalogue.
func TestPackageOfIsACopy(t *testing.T) {
	p, ok := PackageOf("nextcloud")
	if !ok {
		t.Fatal("nextcloud")
	}
	p.Services[0].Image = "changed"
	p.About.Translations["en"] = p.About.Translations["de"]
	again, _ := PackageOf("nextcloud")
	if again.Services[0].Image == "changed" || again.About.Translations["en"].Summary == again.About.Translations["de"].Summary {
		t.Fatal("the catalogue changed")
	}
}
