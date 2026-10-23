package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"myinfra/internal/store"
)

const wikiPackage = `type: myinfra-package
version: 1
name: Wiki
inputs:
  - key: admin
    label: E-mail
env:
  ADMIN: "{{input:admin}}"
  DB_PASSWORD: "{{secret:db}}"
services:
  - name: web
    image: ghcr.io/acme/wiki:2
    public: true
    port: 3000
    volumes:
      - path: /data
        sizeGi: 4
      - path: /cache
    files:
      - path: /etc/wiki.ini
        content: "url = {{url}}\n"
    dependsOn: [db]
  - name: db
    image: postgres:16
    port: 5432
`

func packageCall(t *testing.T, a *API, handler http.HandlerFunc, method, path, id string, body any) *httptest.ResponseRecorder {
	t.Helper()
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(method, path, bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(withOwner(withUser(req.Context(), "u1"), "u1"))
	if id != "" {
		req.SetPathValue("id", id)
	}
	rec := httptest.NewRecorder()
	handler(rec, req)
	return rec
}

func TestPackageIsKeptAndInstalled(t *testing.T) {
	a, _ := leadAPI(t, false, store.CopyRole{})
	ctx := t.Context()
	if err := a.Store.CreateFirstUser(ctx, "u1", "hash"); err != nil {
		t.Fatal(err)
	}
	if err := a.Store.CreateServer(ctx, "u1", "s1", "solo", "tok-solo-123", "", "", "linked", nil, nil, nil, false); err != nil {
		t.Fatal(err)
	}
	bad := packageCall(t, a, a.createPackage, http.MethodPost, "/api/packages", "", map[string]string{"text": strings.Replace(wikiPackage, "public: true\n", "", 1)})
	if bad.Code != http.StatusBadRequest || !strings.Contains(bad.Body.String(), `"problems"`) {
		t.Fatal(bad.Code, bad.Body.String())
	}
	rec := packageCall(t, a, a.createPackage, http.MethodPost, "/api/packages", "", map[string]string{"text": wikiPackage})
	if rec.Code != http.StatusCreated {
		t.Fatal(rec.Code, rec.Body.String())
	}
	var view packageView
	_ = json.Unmarshal(rec.Body.Bytes(), &view)
	if view.Name != "Wiki" || !strings.Contains(view.YAML, "dependsOn:") || len(view.Services) != 2 {
		t.Fatalf("%+v", view)
	}
	again := packageCall(t, a, a.importPackage, http.MethodPost, "/api/packages/import", "", map[string]string{"text": wikiPackage})
	if again.Code != http.StatusCreated || !strings.Contains(again.Body.String(), `"name":"Wiki (2)"`) {
		t.Fatal(again.Code, again.Body.String())
	}
	missing := packageCall(t, a, a.installPackage, http.MethodPost, "/api/packages/x/install", view.ID, map[string]any{
		"name": "wiki", "destinationKind": "server", "serverId": "s1",
	})
	if missing.Code != http.StatusBadRequest {
		t.Fatal(missing.Code, missing.Body.String())
	}
	installed := packageCall(t, a, a.installPackage, http.MethodPost, "/api/packages/x/install", view.ID, map[string]any{
		"name": "wiki", "destinationKind": "server", "serverId": "s1", "inputs": map[string]string{"admin": "moi@exemple.fr"},
	})
	if installed.Code != http.StatusCreated {
		t.Fatal(installed.Code, installed.Body.String())
	}
	work, err := a.workloads(ctx, "tok-solo-123")
	if err != nil || len(work) != 2 {
		t.Fatalf("%+v %v", work, err)
	}
	web := work[0]
	if web.Service != "web" {
		web = work[1]
	}
	if !web.Disk || web.DiskPath != "/data" || web.DiskGi != 4 || web.Listen != 3000 || len(web.Spec.Volumes) != 1 || web.Spec.Volumes[0].Path != "/cache" {
		t.Fatalf("%+v", web)
	}
	if web.Spec.Files[0].Content != "url = https://wiki.apps.localhost\n" || web.Spec.DependsOn[0] != "db" {
		t.Fatalf("%+v", web.Spec)
	}
	if web.Env["ADMIN"] != "moi@exemple.fr" || len(web.Env["DB_PASSWORD"]) != 64 {
		t.Fatalf("%+v", web.Env)
	}
}

func TestApplicationServicesChangeInPlace(t *testing.T) {
	a, _ := leadAPI(t, false, store.CopyRole{})
	ctx := t.Context()
	if err := a.Store.CreateFirstUser(ctx, "u1", "hash"); err != nil {
		t.Fatal(err)
	}
	if err := a.Store.CreateServer(ctx, "u1", "s1", "solo", "tok-solo-123", "", "", "linked", nil, nil, nil, false); err != nil {
		t.Fatal(err)
	}
	rec := packageCall(t, a, a.createPackage, http.MethodPost, "/api/packages", "", map[string]string{"text": wikiPackage})
	var view packageView
	_ = json.Unmarshal(rec.Body.Bytes(), &view)
	installed := packageCall(t, a, a.installPackage, http.MethodPost, "/x", view.ID, map[string]any{
		"name": "wiki", "destinationKind": "server", "serverId": "s1", "inputs": map[string]string{"admin": "moi@exemple.fr"},
	})
	var app struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(installed.Body.Bytes(), &app)

	exported := packageCall(t, a, a.applicationPackage, http.MethodGet, "/x", app.ID, nil)
	text := exported.Body.String()
	for _, want := range []string{`{{secret:DB_PASSWORD}}`, `url = {{url}}`, `"ADMIN":"moi@exemple.fr"`} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %s in %s", want, text)
		}
	}
	kept := packageCall(t, a, a.keepApplicationPackage, http.MethodPost, "/x", app.ID, nil)
	if kept.Code != http.StatusCreated || !strings.Contains(kept.Body.String(), `"name":"wiki (2)"`) {
		t.Fatal(kept.Code, kept.Body.String())
	}

	web := map[string]any{"name": "web", "image": "ghcr.io/acme/wiki:3", "public": true, "port": 3000,
		"volumes": []map[string]any{{"path": "/data", "sizeGi": 4}, {"path": "/cache"}}}
	cache := map[string]any{"name": "cache", "image": "redis:7", "port": 6379}
	changed := packageCall(t, a, a.setServices, http.MethodPut, "/x", app.ID, map[string]any{"services": []any{web, cache}})
	if changed.Code != http.StatusAccepted {
		t.Fatal(changed.Code, changed.Body.String())
	}
	work, _ := a.workloads(ctx, "tok-solo-123")
	names := map[string]string{}
	for _, item := range work {
		names[item.Service] = item.Image
	}
	if len(work) != 2 || names["web"] != "ghcr.io/acme/wiki:3" || names["cache"] != "redis:7" {
		t.Fatalf("%+v", work)
	}

	web["volumes"] = []map[string]any{{"path": "/data", "sizeGi": 4}}
	refused := packageCall(t, a, a.setServices, http.MethodPut, "/x", app.ID, map[string]any{"services": []any{web, cache}})
	if refused.Code != http.StatusConflict || !strings.Contains(refused.Body.String(), "web (/cache)") {
		t.Fatal(refused.Code, refused.Body.String())
	}
	confirmed := packageCall(t, a, a.setServices, http.MethodPut, "/x", app.ID, map[string]any{"services": []any{web, cache}, "confirm": true})
	if confirmed.Code != http.StatusAccepted {
		t.Fatal(confirmed.Code, confirmed.Body.String())
	}
	web["volumes"] = []map[string]any{{"path": "/data", "sizeGi": 8}}
	resized := packageCall(t, a, a.setServices, http.MethodPut, "/x", app.ID, map[string]any{"services": []any{web, cache}})
	if resized.Code != http.StatusBadRequest || !strings.Contains(resized.Body.String(), "taille") {
		t.Fatal(resized.Code, resized.Body.String())
	}
}
