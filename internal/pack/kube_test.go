package pack

import (
	"os"
	"strings"
	"testing"
)

func kubeOf(t *testing.T, file, name string) (Package, []Warning) {
	t.Helper()
	data, err := os.ReadFile("testdata/" + file)
	if err != nil {
		t.Fatal(err)
	}
	if got := Detect(data); got != "kubernetes" {
		t.Fatalf("%s detected as %q", file, got)
	}
	p, warnings, err := Kube(data, name, "mipkg")
	if err != nil {
		t.Fatal(err)
	}
	if problems := Check(p); len(problems) > 0 {
		t.Fatalf("%v\n%+v", problems, p)
	}
	return p, warnings
}

// wiki.helm.yaml is helm template of Wiki.js, with its PostgreSQL.
func TestKubeWikiJS(t *testing.T) {
	p, warnings := kubeOf(t, "wiki.helm.yaml", "Wiki.js")
	wiki, db := service(p, "wiki"), service(p, "wiki-postgresql")
	if !wiki.Public || wiki.Port != 3000 || wiki.Image != "requarks/wiki:2" || wiki.Health == nil || wiki.Health.HTTP != "/healthz" {
		t.Fatalf("%+v", wiki)
	}
	if wiki.Env["DB_HOST"] != "wiki-postgresql" || wiki.Env["DB_PORT"] != "5432" {
		t.Fatalf("%+v", wiki.Env)
	}
	if db.Public || db.Port != 5432 || len(db.Volumes) != 1 || db.Volumes[0].Path != "/var/lib/postgresql/data" || db.Volumes[0].SizeGi != 8 {
		t.Fatalf("%+v", db)
	}
	// The password both read from one Secret is one secret, drawn anew.
	if p.Env["DB_PASS"] == "" || p.Env["DB_PASS"] != p.Env["POSTGRES_PASSWORD"] || !strings.HasPrefix(p.Env["DB_PASS"], "{{secret:") {
		t.Fatalf("%+v", p.Env)
	}
	if p.Env["POSTGRES_USER"] != "postgres" {
		t.Fatalf("%+v", p.Env)
	}
	joined := ""
	for _, warning := range warnings {
		joined += warning.Message + "\n"
	}
	if !strings.Contains(joined, "ServiceAccount") || !strings.Contains(joined, "joignaient sur le port 80") {
		t.Fatal(joined)
	}
}

func TestKubePodinfo(t *testing.T) {
	p, _ := kubeOf(t, "podinfo.helm.yaml", "")
	if len(p.Services) != 1 || p.Name != "Import Helm" {
		t.Fatalf("%+v", p)
	}
	svc := p.Services[0]
	if !svc.Public || svc.Name != "podinfo" || svc.Port == 0 || len(svc.Command) == 0 {
		t.Fatalf("%+v", svc)
	}
}
