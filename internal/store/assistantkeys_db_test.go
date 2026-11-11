package store

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"myinfra/internal/assistant"
)

// start builds the assistant as main does at each start: sealed entries read
// from the database, rotation, then written back.
func startAssistant(t *testing.T, db *Store, master, previous []byte) *assistant.Service {
	t.Helper()
	ctx := context.Background()
	sealed, err := db.AssistantKeys(ctx)
	if err != nil {
		t.Fatal(err)
	}
	s, err := assistant.New(master, previous, sealed)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Persist(ctx, db); err != nil {
		t.Fatal(err)
	}
	return s
}

func call(t *testing.T, s *assistant.Service, method, path, body string) int {
	t.Helper()
	mux := http.NewServeMux()
	s.Register(mux, nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(method, path, strings.NewReader(body)))
	return rec.Code
}

// A provider key entered survives a restart, and a rotation followed by a
// restart without the previous master still opens it.
func TestAssistantKeysSurviveRestartAndRotation(t *testing.T) {
	ctx := context.Background()
	admin, dsn := testAdmin(t)
	defer admin.Close(ctx)
	const name = "myinfra_assistant_keys"
	resetDB(t, admin, name)
	db, err := Open(ctx, dsn(name))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		db.Close()
		dropDB(t, admin, name)
	}()
	oldMaster := bytes.Repeat([]byte{1}, 32)
	newMaster := bytes.Repeat([]byte{2}, 32)

	// Keys entered through the routes are written to the database.
	s := startAssistant(t, db, oldMaster, nil)
	if c := call(t, s, "PUT", "/api/assistant/keys/claude", `{"key":"sk-claude"}`); c != 204 {
		t.Fatalf("PUT claude %d", c)
	}
	if c := call(t, s, "PUT", "/api/assistant/keys/codex", `{"key":"sk-codex"}`); c != 204 {
		t.Fatalf("PUT codex %d", c)
	}
	if c := call(t, s, "PUT", "/api/assistant/keys/gone", `{"key":"sk-gone"}`); c != 204 {
		t.Fatalf("PUT gone %d", c)
	}
	if c := call(t, s, "DELETE", "/api/assistant/keys/gone", ""); c != 204 {
		t.Fatalf("DELETE gone %d", c)
	}
	stored, err := db.AssistantKeys(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(stored) != 2 || stored["claude"] != s.Keys.Export()["claude"] {
		t.Fatalf("entrées en base %v", stored)
	}

	// Restart, same master: the keys open.
	s = startAssistant(t, db, oldMaster, nil)
	if v, ok := s.Keys.Get("claude"); !ok || v != "sk-claude" {
		t.Fatalf("après redémarrage : %q %v", v, ok)
	}
	if _, ok := s.Keys.Get("gone"); ok {
		t.Fatal("une clé supprimée ne revient pas au redémarrage")
	}

	// Rotation: new master, previous posed. The entries are sealed again
	// and written back.
	s = startAssistant(t, db, newMaster, oldMaster)
	if v, _ := s.Keys.Get("codex"); v != "sk-codex" {
		t.Fatal("clé perdue à la rotation")
	}
	after, _ := db.AssistantKeys(ctx)
	if after["claude"] == stored["claude"] || after["codex"] == stored["codex"] {
		t.Fatal("les entrées rechiffrées doivent être réécrites en base")
	}

	// Restart without the previous master: the keys still open.
	s = startAssistant(t, db, newMaster, nil)
	for p, want := range map[string]string{"claude": "sk-claude", "codex": "sk-codex"} {
		if v, ok := s.Keys.Get(p); !ok || v != want {
			t.Fatalf("%s après rotation et redémarrage sans PREVIOUS : %q %v", p, v, ok)
		}
	}
}
