package market

import (
	"testing"

	"myinfra/internal/pack"
)

// The library built into MyInfra opens and presents every category.
func TestCatalogueIsInstallable(t *testing.T) {
	groups := Groups()
	if len(groups) < 11 {
		t.Fatal(len(groups))
	}
	for _, group := range groups {
		if len(group.Articles) == 0 || group.Translations["en"] == "" {
			t.Fatalf("%+v", group)
		}
	}
	if Logo("ghost:5-alpine") != "ghost" || Logo("lissy93/dashy:3.1.1") != "dashy" || Logo("nginx:alpine") != "" {
		t.Fatal(Logo("ghost:5-alpine"), Logo("lissy93/dashy:3.1.1"), Logo("nginx:alpine"))
	}
	if len(IDs()) < 80 {
		t.Fatal(len(IDs()))
	}
	card, ok := CardOf("wordpress")
	if !ok || card.Site != "https://wordpress.org" || card.Logo != "wordpress" || len(card.Services) != 2 || card.Description == "" || card.Version == "" {
		t.Fatalf("%+v", card)
	}
	if Release() == "" {
		t.Fatal("release")
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
