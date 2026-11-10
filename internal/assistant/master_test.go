package assistant

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRekeyReopensWithPreviousMaster(t *testing.T) {
	oldMaster := bytes.Repeat([]byte{1}, 32)
	newMaster := bytes.Repeat([]byte{2}, 32)
	before, _ := NewKeyStore(oldMaster)
	before.Set("claude", "sk-claude")
	before.Set("codex", "sk-codex")
	sealed := before.Export()

	after, _ := NewKeyStore(newMaster)
	after.Import(sealed)
	if _, ok := after.Get("claude"); ok {
		t.Fatal("la nouvelle clé ne doit pas ouvrir l'ancien scellé")
	}
	rekeyed, lost, err := after.Rekey(oldMaster)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(rekeyed, ",") != "claude,codex" || len(lost) != 0 {
		t.Fatalf("rekeyed %v lost %v", rekeyed, lost)
	}
	if v, _ := after.Get("claude"); v != "sk-claude" {
		t.Fatal("clé perdue au rechiffrement")
	}
	// The sealed entries now open with the new master alone.
	fresh, _ := NewKeyStore(newMaster)
	fresh.Import(after.Export())
	if v, _ := fresh.Get("codex"); v != "sk-codex" {
		t.Fatal("le nouveau scellé doit s'ouvrir sans l'ancienne clé")
	}
	// A second pass is a no-op.
	if rekeyed, _, _ := after.Rekey(oldMaster); len(rekeyed) != 0 {
		t.Fatal("rien à rechiffrer au second passage")
	}
}

func TestRekeyReportsLostEntries(t *testing.T) {
	stranger, _ := NewKeyStore(bytes.Repeat([]byte{3}, 32))
	stranger.Set("claude", "sk")
	ks, _ := NewKeyStore(bytes.Repeat([]byte{2}, 32))
	ks.Import(stranger.Export())
	rekeyed, lost, err := ks.Rekey(bytes.Repeat([]byte{1}, 32))
	if err != nil || len(rekeyed) != 0 || strings.Join(lost, ",") != "claude" {
		t.Fatalf("rekeyed %v lost %v err %v", rekeyed, lost, err)
	}
	if _, _, err := ks.Rekey([]byte("court")); err == nil {
		t.Fatal("une clé précédente invalide doit être refusée")
	}
}

func TestNewRotatesAtStart(t *testing.T) {
	oldMaster := bytes.Repeat([]byte{1}, 32)
	newMaster := bytes.Repeat([]byte{2}, 32)
	before, _ := NewKeyStore(oldMaster)
	before.Set("claude", "sk-claude")
	svc, err := New(newMaster, oldMaster, before.Export())
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := svc.Keys.Get("claude"); v != "sk-claude" {
		t.Fatal("la rotation au démarrage doit rouvrir les clés")
	}
}

// Without a BYOK master key the routes answer, as unavailable: nothing
// panics, nothing is kept.
func TestServiceWithoutMasterKeyIsUnavailable(t *testing.T) {
	svc, err := New(nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if svc.Available() {
		t.Fatal("sans clé, indisponible")
	}
	mux := http.NewServeMux()
	svc.Register(mux, nil)
	call := func(method, path, body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(method, path, strings.NewReader(body)))
		return rec
	}
	if rec := call("GET", "/api/assistant/status", ""); rec.Code != 200 || !strings.Contains(rec.Body.String(), `"assistant_unavailable":true`) || !strings.Contains(rec.Body.String(), `"enabled":false`) {
		t.Fatalf("status %d %s", rec.Code, rec.Body.String())
	}
	if rec := call("PUT", "/api/assistant/keys/claude", `{"key":"sk"}`); rec.Code != 503 {
		t.Fatalf("set key %d %s", rec.Code, rec.Body.String())
	}
	if rec := call("DELETE", "/api/assistant/keys/claude", ""); rec.Code != 503 {
		t.Fatalf("delete key %d", rec.Code)
	}
	if rec := call("POST", "/api/assistant/chat", `{"session":"s","provider":"claude","context":"template_edit"}`); rec.Code != 200 || !strings.Contains(rec.Body.String(), `"assistant_unavailable":true`) {
		t.Fatalf("chat %d %s", rec.Code, rec.Body.String())
	}
	if rec := call("GET", "/api/assistant/pricing", ""); rec.Code != 200 {
		t.Fatalf("pricing %d", rec.Code)
	}
}

func TestNewWithMasterKeyIsAvailable(t *testing.T) {
	svc, err := New(bytes.Repeat([]byte{7}, 32), nil, nil)
	if err != nil || !svc.Available() {
		t.Fatal(err)
	}
	if _, err := New([]byte("court"), nil, nil); err == nil {
		t.Fatal("une clé de mauvaise taille doit être refusée")
	}
}
