package api

import (
	"encoding/json"
	"net/http"
	"testing"

	"myinfra/internal/sshk"
)

// An imported key keeps its private half when one is given: MyInfra can
// then open SSH on a machine that already trusts it.
func TestImportSSHKeyWithPrivate(t *testing.T) {
	a, h := buyingFixture(t)
	pair, err := sshk.Generate()
	if err != nil {
		t.Fatal(err)
	}
	rec := call(t, h, http.MethodPost, "/api/secrets/ssh-keys/import", "", "sess-owner", map[string]any{"name": "portable", "privateKey": pair.Private})
	if rec.Code != http.StatusCreated {
		t.Fatal(rec.Code, rec.Body.String())
	}
	var view secretView
	_ = json.Unmarshal(rec.Body.Bytes(), &view)
	if !view.HasPrivate || view.PublicKey != pair.Public || view.Fingerprint != pair.Fingerprint {
		t.Fatalf("got %+v", view)
	}
	if got := a.privateKey(t.Context(), "owner", []string{view.ID}); got != pair.Private {
		t.Fatalf("private key %q", got)
	}

	rec = call(t, h, http.MethodPost, "/api/secrets/ssh-keys/import", "", "sess-owner", map[string]any{"name": "pub", "publicKey": pair.Public})
	_ = json.Unmarshal(rec.Body.Bytes(), &view)
	if rec.Code != http.StatusCreated || view.HasPrivate {
		t.Fatal(rec.Code, rec.Body.String())
	}

	other, _ := sshk.Generate()
	for label, body := range map[string]map[string]any{
		"mélange": {"name": "x", "publicKey": other.Public, "privateKey": pair.Private},
		"privée":  {"name": "x", "privateKey": "pas une clé"},
		"vide":    {"name": "x"},
	} {
		if rec := call(t, h, http.MethodPost, "/api/secrets/ssh-keys/import", "", "sess-owner", body); rec.Code != http.StatusBadRequest {
			t.Fatalf("%s : %d %s", label, rec.Code, rec.Body.String())
		}
	}
}

// The SSH user of a machine added by hand is kept beside its hosting, and
// can change after the machine is created.
func TestServerSSHUserEdit(t *testing.T) {
	_, h := buyingFixture(t)
	rec := call(t, h, http.MethodGet, "/api/secrets", "", "sess-owner", nil)
	var secrets []secretView
	_ = json.Unmarshal(rec.Body.Bytes(), &secrets)
	keyID := ""
	for _, s := range secrets {
		if s.Kind == "ssh_key" {
			keyID = s.ID
		}
	}
	rec = call(t, h, http.MethodPost, "/api/servers", "", "sess-owner", map[string]any{
		"name": "maison", "providerId": "manual", "keyIds": []string{keyID},
		"hosting": map[string]any{"provider": "infomaniak", "price": "5", "period": "month"},
	})
	if rec.Code != http.StatusCreated {
		t.Fatal(rec.Code, rec.Body.String())
	}
	var server serverView
	_ = json.Unmarshal(rec.Body.Bytes(), &server)
	path := "/api/servers/" + server.ID + "/ssh-user"

	rec = call(t, h, http.MethodPut, path, "", "sess-owner", map[string]any{"sshUser": "debian"})
	_ = json.Unmarshal(rec.Body.Bytes(), &server)
	if rec.Code != http.StatusOK || server.SSHUser != "debian" || server.Hosting == nil || server.Hosting.Provider != "infomaniak" {
		t.Fatal(rec.Code, rec.Body.String())
	}
	rec = call(t, h, http.MethodPut, path, "", "sess-owner", map[string]any{"sshUser": "root"})
	server = serverView{}
	_ = json.Unmarshal(rec.Body.Bytes(), &server)
	if rec.Code != http.StatusOK || server.SSHUser != "" || server.Hosting == nil {
		t.Fatal(rec.Code, rec.Body.String())
	}
	if rec := call(t, h, http.MethodPut, path, "", "sess-owner", map[string]any{"sshUser": "a;b"}); rec.Code != http.StatusBadRequest {
		t.Fatal(rec.Code, rec.Body.String())
	}
}
