package manifest

import (
	"strings"
	"testing"
)

func TestSharedSecretPrecedesAppEnv(t *testing.T) {
	id := "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	app := App{
		ID:  "26ae9a42-10dd-4dd9-9c25-8b7636a7d237",
		Env: map[string]string{"TOKEN": "from-app"},
		Shared: []SharedSecret{{
			Name: SecretObject(id),
			Data: map[string]string{"TOKEN": "from-shared", "ONLY": "shared-only"},
		}},
		Services: []Service{{Name: "web", Image: "nginx:1"}},
	}
	if !Deployable(app) {
		t.Fatal("manifeste refusé")
	}
	raw := Render(app)
	name := SecretObject(id)
	want := "envFrom:\n            - secretRef:\n                name: " + name + "\n            - secretRef:\n                name: env\n"
	if !strings.Contains(raw, want) {
		t.Fatalf("envFrom\n%s", raw)
	}
	if !strings.Contains(raw, `myinfra.io/shared: "true"`) || !strings.Contains(raw, "ONLY:") {
		t.Fatal("secret partagé absent")
	}
	if strings.Contains(raw, "ABSENT") {
		t.Fatal("donnée non accordée")
	}
	bad := app
	bad.Shared = []SharedSecret{{Name: "env", Data: map[string]string{"TOKEN": "x"}}}
	if Deployable(bad) {
		t.Fatal("nom de secret accepté")
	}
}
