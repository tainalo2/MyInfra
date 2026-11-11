package api

import (
	"encoding/json"
	"net/http"
	"testing"

	"myinfra/internal/deploy"
	"myinfra/internal/store"
)

// A machine added by hand that stopped answering goes back to waiting for its
// command, every step to do again, and no longer shows as lost. The others
// stay as they are.
func TestReinstallTakesBackALostMachine(t *testing.T) {
	a, h := tokenFixture(t)
	ctx := t.Context()
	create := func(id, token, provider, status string) {
		t.Helper()
		if err := a.Store.CreateServer(ctx, "owner", id, id, token, provider, "", status, nil, nil, nil, false); err != nil {
			t.Fatal(err)
		}
		if err := a.Store.SetJoinAddress(ctx, token, "203.0.113.10"); err != nil {
			t.Fatal(err)
		}
	}
	create("lost", "tok-lost-123", "", "linked")
	create("fine", "tok-fine-123", "", "linked")
	create("waiting", "tok-waiting-123", "", "pending")
	a.reach.health = map[string]store.ServerHealth{"lost": {Fails: lostAfter}, "waiting": {Fails: lostAfter}}

	for _, id := range []string{"fine", "waiting"} {
		rec := call(t, h, http.MethodPost, "/api/servers/"+id+"/reinstall", "", "sess-owner", nil)
		if rec.Code != http.StatusConflict {
			t.Fatal(id, rec.Code, rec.Body.String())
		}
	}

	rec := call(t, h, http.MethodPost, "/api/servers/lost/reinstall", "", "sess-owner", nil)
	if rec.Code != http.StatusOK {
		t.Fatal(rec.Code, rec.Body.String())
	}
	var view serverView
	if err := json.Unmarshal(rec.Body.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	if view.Status != "pending" || view.Reach != "" {
		t.Fatalf("status %q, reach %q", view.Status, view.Reach)
	}
	if view.JoinCommand == "" {
		t.Fatal("no command to paste")
	}
	row, err := a.Store.Server(ctx, "owner", "lost")
	if err != nil {
		t.Fatal(err)
	}
	steps := stepsOf(row.Deploy)
	if len(steps) == 0 {
		t.Fatal("no steps to do again")
	}
	for _, step := range steps {
		if deploy.Done(steps, step.ID) {
			t.Fatalf("step %s still done", step.ID)
		}
	}
	healths, err := a.Store.ServerHealths(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := healths["lost"]; ok {
		t.Fatal("still marked as lost")
	}
	if rec := call(t, h, http.MethodPost, "/api/servers/lost/reinstall", "", "sess-owner", nil); rec.Code != http.StatusConflict {
		t.Fatal("twice", rec.Code)
	}
}
