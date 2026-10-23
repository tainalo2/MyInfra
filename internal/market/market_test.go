package market

import "testing"

func TestCatalogueIsInstallable(t *testing.T) {
	if err := Validate(articles); err != nil {
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
	if len(services) != 2 || !services[0].Published || services[1].Name != "mariadb" || services[1].Listen != 3306 || services[0].DiskPath != "/var/www/html" {
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
	if got := FillAddress(env["LISTMONK_app__address"], "listes.exemple.fr"); got != "https://listes.exemple.fr" {
		t.Fatal(got)
	}
	if got := FillAddress("{{host}}:{{host}}", "a.fr"); got != "a.fr:a.fr" {
		t.Fatal(got)
	}
}
