package pack

import (
	"os"
	"strings"
	"testing"
)

func compose(t *testing.T, file string) (Package, []Warning) {
	t.Helper()
	data, err := os.ReadFile("testdata/" + file)
	if err != nil {
		t.Fatal(err)
	}
	if got := Detect(data); got != "compose" {
		t.Fatalf("%s detected as %q", file, got)
	}
	p, warnings, err := Compose(data, "")
	if err != nil {
		t.Fatal(err)
	}
	// What is converted reads back the same through YAML.
	text, err := YAML(p)
	if err != nil {
		t.Fatal(err)
	}
	if _, problems, readable := Read(text); !readable {
		t.Fatalf("%v\n%s", problems, text)
	}
	return p, warnings
}

func service(p Package, name string) Service {
	for _, svc := range p.Services {
		if svc.Name == name {
			return svc
		}
	}
	return Service{}
}

func TestComposeNextcloud(t *testing.T) {
	p, warnings := compose(t, "nextcloud.compose.yaml")
	if problems := Check(p); len(problems) > 0 {
		t.Fatalf("%v\n%+v", problems, p)
	}
	app, db := service(p, "app"), service(p, "db")
	if !app.Public || app.Port != 80 || app.Volumes[0].Path != "/var/www/html" || strings.Join(app.DependsOn, ",") != "redis,db" {
		t.Fatalf("%+v", app)
	}
	if db.Public || len(db.Args) != 2 || db.Volumes[0].Path != "/var/lib/mysql" || db.Env["MYSQL_DATABASE"] != "nextcloud" {
		t.Fatalf("%+v", db)
	}
	// The same password in both services is the same secret, drawn anew.
	if p.Env["MYSQL_PASSWORD"] == "" || p.Env["MYSQL_PASSWORD"] != p.Env["MYSQL_ROOT_PASSWORD"] || strings.Contains(p.Env["MYSQL_PASSWORD"], "example") {
		t.Fatalf("%+v", p.Env)
	}
	if _, kept := app.Env["MYSQL_PASSWORD"]; kept {
		t.Fatalf("%+v", app.Env)
	}
	if len(warnings) == 0 {
		t.Fatal("no warning about disk sizes")
	}
}

func TestComposeImmich(t *testing.T) {
	p, warnings := compose(t, "immich.compose.yaml")
	server, database := service(p, "immich-server"), service(p, "database")
	if p.Name != "immich" || server.Image != "ghcr.io/immich-app/immich-server:release" || !server.Public || server.Port != 2283 {
		t.Fatalf("%+v", server)
	}
	if server.Health == nil || server.Health.HTTP != "/api/server/ping" || server.Health.Port != 0 {
		t.Fatalf("%+v", server.Health)
	}
	if len(server.Volumes) != 1 || server.Volumes[0].Path != "/usr/src/app/upload" || len(server.Files) != 1 || server.Files[0].Path != "/usr/src/app/hwaccel.yml" {
		t.Fatalf("%+v %+v", server.Volumes, server.Files)
	}
	if server.Env["PUBLIC_URL"] != "https://photos.example.com" || server.Env["TZ"] != "{{input:tz}}" {
		t.Fatalf("%+v", server.Env)
	}
	if p.Env["DB_PASSWORD"] != "{{secret:db-password}}" || p.Env["POSTGRES_PASSWORD"] != "{{secret:db-password}}" {
		t.Fatalf("%+v", p.Env)
	}
	if database.Resources == nil || database.Resources.Memory != "1024Mi" || database.Resources.CPU != "0.5" {
		t.Fatalf("%+v", database.Resources)
	}
	if len(database.Command) != 5 || database.Command[4] != `search_path="$user", public` {
		t.Fatalf("%q", database.Command)
	}
	if len(p.Inputs) != 1 || p.Inputs[0].Key != "tz" {
		t.Fatalf("%+v", p.Inputs)
	}
	// The service built from sources has no image: the package says so.
	problems := Check(p)
	if len(problems) != 1 || problems[0].Path != "services[3].image" {
		t.Fatalf("%v", problems)
	}
	joined := ""
	for _, warning := range warnings {
		joined += warning.Service + ": " + warning.Message + "\n"
	}
	for _, want := range []string{"build:", "env_file", "/etc/localtime", "hwaccel.yml", "redis-cli ping"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing %s in\n%s", want, joined)
		}
	}
}
