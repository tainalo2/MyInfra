package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"myinfra/internal/store"
)

func TestNightDueOncePerNightInsideTheWindow(t *testing.T) {
	at := func(day, hour, minute int) time.Time { return time.Date(2026, 10, day, hour, minute, 0, 0, time.UTC) }
	row := store.AutoUpdate{Enabled: true, Hour: 3}
	if nightDue(row, at(30, 2, 59)) {
		t.Fatal("avant l'heure")
	}
	if !nightDue(row, at(30, 3, 5)) {
		t.Fatal("à l'heure")
	}
	if !nightDue(row, at(30, 5, 59)) {
		t.Fatal("dans la fenêtre, MyInfra éteint à l'heure")
	}
	if nightDue(row, at(30, 6, 0)) {
		t.Fatal("après la fenêtre : la nuit suivante")
	}
	ran := at(30, 3, 5)
	row.LastRun = &ran
	if nightDue(row, at(30, 4, 0)) {
		t.Fatal("deux fois la même nuit")
	}
	if !nightDue(row, at(31, 3, 1)) {
		t.Fatal("la nuit suivante")
	}
	// An hour late in the evening: the window runs past midnight.
	late := store.AutoUpdate{Enabled: true, Hour: 23}
	if !nightDue(late, at(31, 0, 30)) {
		t.Fatal("fenêtre après minuit")
	}
	if nightDue(store.AutoUpdate{Enabled: false, Hour: 3}, at(30, 3, 5)) {
		t.Fatal("désactivé")
	}
}

func TestImpactQuietLeavesASingleDriverToThePerson(t *testing.T) {
	if !impactQuiet(impactOf([]impactApp{{Effect: effectNone}}, "reboot")) {
		t.Fatal("redémarrage sans coupure")
	}
	if impactQuiet(impactOf([]impactApp{{Effect: effectShort}})) {
		t.Fatal("courte coupure")
	}
	if impactQuiet(impactOf(nil, "single-plane")) {
		t.Fatal("un seul pilote")
	}
}

// Only what stops no application is applied together: a major version, an
// application that keeps its files on one disk, a changed library copy
// wait for the person.
func TestQuietUpdatesKeepWhatInterrupts(t *testing.T) {
	a, h := tokenFixture(t)
	ctx := t.Context()
	if err := a.Store.CreateServer(ctx, "owner", "s1", "solo", "tok-solo-123", "", "", "linked", nil, nil, nil, false); err != nil {
		t.Fatal(err)
	}
	apps := []struct {
		id   string
		disk bool
	}{{"web", false}, {"major", false}, {"files", true}}
	for _, app := range apps {
		if err := a.Store.CreateApplication(ctx, store.NewApplication{
			ID: app.id, OwnerID: "owner", Name: app.id, SourceKind: "image", Source: "nginx:1",
			DestinationKind: "server", ServerID: "s1", Replicas: 1, DefaultID: "d-" + app.id, DefaultHost: app.id + ".apps.test",
			Services: []store.Service{{ID: "svc-" + app.id, Name: "web", Image: "nginx:1", Published: true, Disk: app.disk, Replicas: 1}},
		}); err != nil {
			t.Fatal(err)
		}
	}
	found := []store.Update{
		{Kind: "app", TargetID: "web", OwnerID: "owner", Current: "1", Available: "1.1", Level: "patch"},
		{Kind: "app", TargetID: "major", OwnerID: "owner", Current: "1", Available: "2", Level: "major"},
		{Kind: "app", TargetID: "files", OwnerID: "owner", Current: "1", Available: "1.1", Level: "patch"},
	}
	if err := a.Store.SaveUpdates(ctx, "app", found, nil); err != nil {
		t.Fatal(err)
	}
	rec := call(t, h, http.MethodGet, "/api/updates/quiet", "", "sess-dep", nil)
	if rec.Code != http.StatusOK {
		t.Fatal(rec.Code, rec.Body.String())
	}
	var view struct {
		Updates []quietItem `json:"updates"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	if len(view.Updates) != 1 || view.Updates[0].ID != "web" || view.Updates[0].Kind != "app" {
		t.Fatalf("%+v", view.Updates)
	}
}

func TestNightlySettingIsCheckedAndKept(t *testing.T) {
	_, h := tokenFixture(t)
	rec := call(t, h, http.MethodGet, "/api/updates/auto", "", "sess-dep", nil)
	if rec.Code != http.StatusOK || !jsonHas(t, rec.Body.Bytes(), "enabled", false) || !jsonHas(t, rec.Body.Bytes(), "hour", float64(3)) {
		t.Fatal("par défaut", rec.Code, rec.Body.String())
	}
	if rec := call(t, h, http.MethodPut, "/api/updates/auto", "", "sess-dep", map[string]any{"enabled": true, "hour": 24}); rec.Code != http.StatusBadRequest {
		t.Fatal("heure hors bornes", rec.Code)
	}
	if rec := call(t, h, http.MethodPut, "/api/updates/auto", "", "sess-dep", map[string]any{"enabled": true, "hour": 4}); rec.Code != http.StatusOK {
		t.Fatal(rec.Code, rec.Body.String())
	}
	rec = call(t, h, http.MethodGet, "/api/updates/auto", "", "sess-owner", nil)
	if !jsonHas(t, rec.Body.Bytes(), "enabled", true) || !jsonHas(t, rec.Body.Bytes(), "hour", float64(4)) {
		t.Fatal("non gardé", rec.Body.String())
	}
	// Without an hour, the one chosen stays.
	call(t, h, http.MethodPut, "/api/updates/auto", "", "sess-dep", map[string]any{"enabled": false})
	rec = call(t, h, http.MethodGet, "/api/updates/auto", "", "sess-owner", nil)
	if !jsonHas(t, rec.Body.Bytes(), "enabled", false) || !jsonHas(t, rec.Body.Bytes(), "hour", float64(4)) {
		t.Fatal("heure perdue", rec.Body.String())
	}
}

func TestApplyQuietWithNothingToApply(t *testing.T) {
	_, h := tokenFixture(t)
	if rec := call(t, h, http.MethodPost, "/api/updates/quiet", "", "sess-dep", map[string]any{}); rec.Code != http.StatusConflict {
		t.Fatal(rec.Code, rec.Body.String())
	}
}

func TestRestartRefusesWhatCannotRestart(t *testing.T) {
	a, h := tokenFixture(t)
	ctx := t.Context()
	if err := a.Store.CreateServer(ctx, "owner", "s1", "solo", "tok-solo-123", "", "", "linked", nil, nil, nil, false); err != nil {
		t.Fatal(err)
	}
	if err := a.Store.CreateApplication(ctx, store.NewApplication{
		ID: "app-demo", OwnerID: "owner", Name: "demo", SourceKind: "image", Source: "nginx:1",
		DestinationKind: "server", ServerID: "s1", Replicas: 1, DefaultID: "d-demo", DefaultHost: "demo.apps.test",
	}); err != nil {
		t.Fatal(err)
	}
	if err := a.Store.SetPaused(ctx, "owner", "app-demo", true); err != nil {
		t.Fatal(err)
	}
	if rec := call(t, h, http.MethodPost, "/api/applications/app-demo/restart", "", "sess-dep", map[string]any{}); rec.Code != http.StatusConflict {
		t.Fatal("en pause", rec.Code, rec.Body.String())
	}
	if rec := call(t, h, http.MethodPost, "/api/applications/myinfra/restart", "", "sess-dep", map[string]any{}); rec.Code != http.StatusConflict {
		t.Fatal("MyInfra lui-même", rec.Code)
	}
	if rec := call(t, h, http.MethodPost, "/api/applications/absente/restart", "", "sess-dep", map[string]any{}); rec.Code != http.StatusNotFound {
		t.Fatal("absente", rec.Code)
	}
}

func jsonHas(t *testing.T, raw []byte, key string, want any) bool {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out[key] == want
}

// Through MCP, applying the quiet updates lists them first and changes
// nothing until the code comes back.
func TestMCPApplyQuietAsksFirst(t *testing.T) {
	a, h := tokenFixture(t)
	ctx := t.Context()
	if err := a.Store.CreateServer(ctx, "owner", "s1", "solo", "tok-solo-123", "", "", "linked", nil, nil, nil, false); err != nil {
		t.Fatal(err)
	}
	if err := a.Store.CreateApplication(ctx, store.NewApplication{
		ID: "web", OwnerID: "owner", Name: "web", SourceKind: "image", Source: "nginx:1",
		DestinationKind: "server", ServerID: "s1", Replicas: 1, DefaultID: "d-web", DefaultHost: "web.apps.test",
		Services: []store.Service{{ID: "svc-web", Name: "web", Image: "nginx:1", Published: true, Replicas: 1}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := a.Store.SaveUpdates(ctx, "app", []store.Update{{Kind: "app", TargetID: "web", OwnerID: "owner", Current: "1", Available: "1.1", Level: "patch"}}, nil); err != nil {
		t.Fatal(err)
	}
	token := makeToken(t, h, "sess-owner", store.RoleDeploy, 0).Token
	text, failed := mcpCallTool(t, h, token, "apply_quiet_updates", map[string]any{})
	if failed || confirmCode.FindStringSubmatch(text) == nil || !strings.Contains(text, "web") {
		t.Fatal(text)
	}
	if app, err := a.Store.Application(ctx, "owner", "web"); err != nil || app.Rollout == "updating" {
		t.Fatal("lancée avant l'accord", err)
	}
	reader := makeToken(t, h, "sess-owner", store.RoleRead, 0).Token
	if text, failed := mcpCallTool(t, h, reader, "list_quiet_updates", map[string]any{}); failed || !strings.Contains(text, "nightly") {
		t.Fatal(text)
	}
}
