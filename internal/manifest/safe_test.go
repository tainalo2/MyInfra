package manifest

import (
	"strings"
	"testing"
)

func TestNamespaceKeepsExistingIdentifiers(t *testing.T) {
	if got := Namespace("26ae9a42-10dd-4dd9-9c25-8b7636a7d237"); got != "mi26ae9a4210dd4dd99c25" {
		t.Fatal(got)
	}
	if got := Namespace("abc;rm -rf /"); got != "miabcrmrf" {
		t.Fatal(got)
	}
}

func TestDeployableRejectsShellAndYAMLBreakout(t *testing.T) {
	app := App{
		ID: "26ae9a42-10dd-4dd9-9c25-8b7636a7d237",
		Services: []Service{{
			Name: "web", Image: "ghost:5", Published: true, Listen: 80,
		}},
	}
	if !Deployable(app) {
		t.Fatal("expected a normal application")
	}
	app.Env = map[string]string{"FOO\nEOF": "bar"}
	if Deployable(app) {
		t.Fatal("newline key")
	}
	app.Env = nil
	app.Services[0].Image = "ghost:5\nsecurityContext: privileged"
	if Deployable(app) {
		t.Fatal("image break")
	}
}

func TestEnvOK(t *testing.T) {
	if !EnvOK(map[string]string{"POSTGRES_PASSWORD": "secret"}) {
		t.Fatal("password")
	}
	if EnvOK(map[string]string{"bad-name": "x"}) || EnvOK(map[string]string{"OK": "a\x00b"}) {
		t.Fatal("accepted")
	}
}

func TestRefusalNamesWhatIsWrong(t *testing.T) {
	app := App{ID: "app-1", Services: []Service{{Name: "web", Image: "ghost:5", Replicas: 11}}}
	if got := Refusal(app); !strings.Contains(got, "11 copies") || !strings.Contains(got, "web") {
		t.Fatal(got)
	}
	app.Services[0].Replicas = 3
	if got := Refusal(app); got != "" {
		t.Fatal(got)
	}
}
