package assistant

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type memRepo struct {
	entries map[string]string
	fail    bool
}

func (m *memRepo) PutAssistantKey(_ context.Context, provider, sealed string) error {
	if m.fail {
		return errors.New("down")
	}
	m.entries[provider] = sealed
	return nil
}

func (m *memRepo) DeleteAssistantKey(_ context.Context, provider string) error {
	if m.fail {
		return errors.New("down")
	}
	delete(m.entries, provider)
	return nil
}

func serve(s *Service, method, path, body string) int {
	mux := http.NewServeMux()
	s.Register(mux, nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(method, path, strings.NewReader(body)))
	return rec.Code
}

func TestPersistWritesSetDeleteAndRekeyed(t *testing.T) {
	ctx := context.Background()
	oldMaster := bytes.Repeat([]byte{1}, 32)
	newMaster := bytes.Repeat([]byte{2}, 32)
	repo := &memRepo{entries: map[string]string{}}

	s, _ := New(oldMaster, nil, repo.entries)
	s.Persist(ctx, repo)
	if c := serve(s, "PUT", "/api/assistant/keys/claude", `{"key":"sk"}`); c != 204 {
		t.Fatalf("PUT %d", c)
	}
	if repo.entries["claude"] != s.Keys.Export()["claude"] {
		t.Fatal("l'entrée doit être gardée telle que Export la donne")
	}
	sealedOld := repo.entries["claude"]

	// Rotation: the entry sealed again is written back by Persist.
	s, _ = New(newMaster, oldMaster, map[string]string{"claude": sealedOld})
	if err := s.Persist(ctx, repo); err != nil {
		t.Fatal(err)
	}
	if repo.entries["claude"] == sealedOld {
		t.Fatal("l'entrée rechiffrée doit être réécrite")
	}
	s, _ = New(newMaster, nil, repo.entries)
	if v, _ := s.Keys.Get("claude"); v != "sk" {
		t.Fatal("clé illisible sans l'ancienne clé maîtresse")
	}
	s.Persist(ctx, repo)
	if c := serve(s, "DELETE", "/api/assistant/keys/claude", ""); c != 204 || len(repo.entries) != 0 {
		t.Fatalf("DELETE %d %v", c, repo.entries)
	}

	// A store that fails is reported, and a delete keeps the key in memory.
	s.Keys.Set("codex", "sk2")
	repo.fail = true
	if c := serve(s, "PUT", "/api/assistant/keys/claude", `{"key":"sk"}`); c != 500 {
		t.Fatalf("PUT en échec %d", c)
	}
	if c := serve(s, "DELETE", "/api/assistant/keys/codex", ""); c != 500 {
		t.Fatalf("DELETE en échec %d", c)
	}
	if _, ok := s.Keys.Get("codex"); !ok {
		t.Fatal("une suppression non enregistrée ne doit pas retirer la clé")
	}
}
