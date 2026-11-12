package assistant

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	harness "myinfra/internal/harness-router"
)

func newSvc(t *testing.T, claudeURL, openaiURL string) *Service {
	ks, err := NewKeyStore(bytes.Repeat([]byte{7}, 32))
	if err != nil {
		t.Fatal(err)
	}
	cfg, _ := harness.ParseConfig([]byte("contexts:\n  general:\n    system_prompt: sys\n    allowed_tools: []\n  template_editor:\n    system_prompt: sys\n    allowed_tools: [read_template]\npages:\n  - page: template_editor\n    context: template_editor\n"))
	p := DefaultPricing()
	s := &Service{Keys: ks, Pricing: p, Meter: NewMeter(p), Router: harness.NewRouter(cfg),
		Providers: map[string]Provider{"claude": Claude{claudeURL}, "codex": OpenAI{openaiURL}}}
	// Set a default AccessVerifier that always allows access for tests
	s.AccessVerifier = func(ctx context.Context, userID, templateID string) bool {
		return true
	}
	return s
}

func TestKeyStoreEncryptsAndRoundtrips(t *testing.T) {
	ks, _ := NewKeyStore(bytes.Repeat([]byte{1}, 32))
	ks.Set("claude", "sk-secret")
	if strings.Contains(ks.enc["claude"], "sk-secret") {
		t.Fatal("stored in clear")
	}
	if v, _ := ks.Get("claude"); v != "sk-secret" {
		t.Fatal("roundtrip")
	}
}

func TestMeterSumsAndSwitchesPricing(t *testing.T) {
	m := NewMeter(DefaultPricing())
	m.Record("s", "claude", "claude-sonnet", Usage{1_000_000, 0}) // $3
	snap := m.Record("s", "codex", "gpt-5-codex", Usage{0, 1_000_000})
	if snap.InputTokens != 1_000_000 || snap.OutputTokens != 1_000_000 {
		t.Fatalf("%+v", snap)
	}
	if snap.CostUSD != 13 { // 3 + 10
		t.Fatalf("cost %v", snap.CostUSD)
	}
}

func TestChatUpdatesMeterAndNoKey(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != "k" {
			w.WriteHeader(401)
			return
		}
		w.Write([]byte(`{"content":[{"text":"hi"}],"usage":{"input_tokens":100,"output_tokens":50}}`))
	}))
	defer up.Close()
	s := newSvc(t, up.URL, "")
	mux := http.NewServeMux()
	s.Register(mux, AsRole(harness.RoleDeploy))
	post := func(path, body string) *httptest.ResponseRecorder {
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, httptest.NewRequest("POST", path, strings.NewReader(body)))
		return rr
	}
	chat := `{"session":"a","provider":"claude","model":"claude-sonnet","page":"template_editor","messages":[{"role":"user","content":"x"}]}`
	if !strings.Contains(post("/api/assistant/chat", chat).Body.String(), "assistant_unavailable") {
		t.Fatal("expected unavailable without key")
	}
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest("PUT", "/api/assistant/keys/claude", strings.NewReader(`{"key":"k"}`)))
	if rr.Code != 204 {
		t.Fatal(rr.Code)
	}
	var out struct{ Usage Snapshot }
	json.Unmarshal(post("/api/assistant/chat", chat).Body.Bytes(), &out)
	post("/api/assistant/chat", chat)
	got := s.Meter.Get("a")
	if got.InputTokens != 200 || got.OutputTokens != 100 || out.Usage.InputTokens != 100 {
		t.Fatalf("%+v %+v", got, out)
	}
	if _, err := (Claude{up.URL}).Complete(context.Background(), "m", "", nil, "bad"); err == nil || strings.Contains(err.Error(), "bad\"") {
		t.Fatalf("err %v", err)
	}
}

// A read-only role, or a route mounted without any role, never reaches the
// provider: 403 at the service, before the key lookup and the network.
func TestChatRefusesReadOnlyRoleBeforeProvider(t *testing.T) {
	hits := 0
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.Write([]byte(`{"content":[{"text":"hi"}],"usage":{"input_tokens":1,"output_tokens":1}}`))
	}))
	defer up.Close()
	s := newSvc(t, up.URL, "")
	s.Keys.Set("claude", "k")
	chat := `{"session":"a","provider":"claude","model":"claude-sonnet","page":"template_editor","messages":[{"role":"user","content":"x"}]}`
	for _, wrap := range []func(http.HandlerFunc) http.HandlerFunc{nil, AsRole(harness.RoleRead)} {
		mux := http.NewServeMux()
		s.Register(mux, wrap)
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, httptest.NewRequest("POST", "/api/assistant/chat", strings.NewReader(chat)))
		if rr.Code != 403 {
			t.Fatalf("chat %d %s", rr.Code, rr.Body.String())
		}
	}
	if hits != 0 {
		t.Fatalf("provider called %d times for refused turns", hits)
	}
	mux := http.NewServeMux()
	s.Register(mux, AsRole(harness.RoleAdmin))
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest("POST", "/api/assistant/chat", strings.NewReader(chat)))
	if rr.Code != 200 || hits != 1 {
		t.Fatalf("admin chat %d hits=%d", rr.Code, hits)
	}
}
