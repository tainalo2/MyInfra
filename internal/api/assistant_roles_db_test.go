package api

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"myinfra/internal/assistant"
	"myinfra/internal/commit"
	"myinfra/internal/store"
)

// countingProvider stands for the Provider Adapter: it answers at once and
// counts the turns it was asked for, so a refusal can be shown to have
// reached no provider at all.
type countingProvider struct{ calls atomic.Int32 }

func (p *countingProvider) Name() string { return "fake" }
func (p *countingProvider) Complete(_ context.Context, _, _ string, _ []assistant.Message, _ string, _ assistant.ReasoningEffort) (assistant.Response, error) {
	p.calls.Add(1)
	return assistant.Response{Text: "ok", Usage: assistant.Usage{InputTokens: 1, OutputTokens: 1}}, nil
}
func (p *countingProvider) SupportsSubscriptionAuth() bool { return false }
func (p *countingProvider) ListModels() []assistant.ModelCapability {
	return []assistant.ModelCapability{
		{Model: "fake-model", SupportedReasons: []assistant.ReasoningEffort{assistant.ReasoningLow}, DefaultReason: assistant.ReasoningLow},
	}
}

// disposableRepo is a git repository created for one test, with one commit.
func disposableRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com",
			"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# disposable\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-q", "-m", "initial")
	return dir
}

func commitCount(t *testing.T, repo string) int {
	t.Helper()
	out, err := exec.Command("git", "-C", repo, "rev-list", "--count", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	n, _ := strconv.Atoi(strings.TrimSpace(string(out)))
	return n
}

// rolesFixture opens a database with the four accounts of the matrix, each
// with a session, a fake provider the chat can reach, and a Commit Gateway
// on a disposable repository.
func rolesFixture(t *testing.T) (*API, http.Handler, *countingProvider, string) {
	t.Helper()
	a, _ := leadAPI(t, false, store.CopyRole{})
	ctx := t.Context()
	if err := a.Store.CreateFirstUser(ctx, "admin", "hash"); err != nil {
		t.Fatal(err)
	}
	for _, u := range []struct{ id, role string }{{"reader", store.RoleRead}, {"deployer", store.RoleDeploy}, {"writer", store.RoleDeploy}} {
		if err := a.Store.CreateUserAccount(ctx, u.id, u.id, u.role, "hash"); err != nil {
			t.Fatal(err)
		}
	}
	// The permission is distinct from the role: granted to one deployer only.
	if err := a.Store.SetUserWriteTemplates(ctx, "writer", true); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"admin", "reader", "deployer", "writer"} {
		if err := a.Store.CreateSession(ctx, hashToken("sess-"+id), id, time.Now().Add(time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
	master := sha256.Sum256([]byte("byok test master"))
	helper, err := assistant.New(master[:], nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	prov := &countingProvider{}
	helper.Providers = map[string]assistant.Provider{"fake": prov}
	if err := helper.Keys.Set("fake", "sk-fake"); err != nil {
		t.Fatal(err)
	}
	a.Assistant = helper
	repo := disposableRepo(t)
	a.Commit = &commit.Gateway{Repo: commit.LiveRepo{Path: repo}, Staging: a.Store}
	return a, a.Handler(), prov, repo
}

func newFilePatch(who string) map[string]any {
	return map[string]any{
		"id":      "p-" + who,
		"hunkId":  "@@ -0,0 +1 @@",
		"path":    who + ".txt",
		"content": fmt.Sprintf("--- /dev/null\n+++ b/%s.txt\n@@ -0,0 +1 @@\n+hello from %s\n", who, who),
	}
}

// The matrix of the ticket: four accounts × chat / open context / patch /
// consult / commit, every right checked by the backend. A refusal is 403,
// never 500; a refused commit leaves the live repository untouched.
func TestAssistantRolesMatrix(t *testing.T) {
	a, h, prov, repo := rolesFixture(t)
	_ = a

	type row struct {
		who                                 string
		chat, open, patch, consult, commits int
	}
	want := []row{
		{"reader", http.StatusForbidden, http.StatusForbidden, http.StatusForbidden, http.StatusOK, http.StatusForbidden},
		{"deployer", http.StatusOK, http.StatusCreated, http.StatusOK, http.StatusOK, http.StatusForbidden},
		{"writer", http.StatusOK, http.StatusCreated, http.StatusOK, http.StatusOK, http.StatusOK},
		{"admin", http.StatusOK, http.StatusCreated, http.StatusOK, http.StatusOK, http.StatusOK},
	}
	t.Logf("%-9s %-5s %-5s %-5s %-8s %-6s", "account", "chat", "open", "patch", "consult", "commit")
	for _, r := range want {
		sess := "sess-" + r.who
		got := row{who: r.who}
		callsBefore := prov.calls.Load()
		commitsBefore := commitCount(t, repo)

		rec := call(t, h, http.MethodPost, "/api/assistant/chat", "", sess, map[string]any{
			"session": "s-" + r.who, "provider": "fake", "model": "m", "context": "template_edit",
			"messages": []map[string]string{{"role": "user", "content": "hi"}},
		})
		got.chat = rec.Code
		if r.chat == http.StatusForbidden && prov.calls.Load() != callsBefore {
			t.Fatalf("%s: le Provider Adapter a été appelé malgré le refus", r.who)
		}
		if r.chat == http.StatusOK && (prov.calls.Load() != callsBefore+1 || !strings.Contains(rec.Body.String(), `"text":"ok"`)) {
			t.Fatalf("%s: chat %d %s (calls %d)", r.who, rec.Code, rec.Body.String(), prov.calls.Load())
		}

		rec = call(t, h, http.MethodPost, "/api/diff/staging", "", sess, map[string]any{
			"kind": "template", "name": "demo", "context": "template_edit", "patches": []any{},
		})
		got.open = rec.Code
		stagingID := "missing"
		if rec.Code == http.StatusCreated {
			var out struct {
				SessionID string `json:"sessionId"`
			}
			_ = json.Unmarshal(rec.Body.Bytes(), &out)
			stagingID = out.SessionID
		}

		rec = call(t, h, http.MethodPut, "/api/diff/staging/"+stagingID+"/patches", "", sess, map[string]any{
			"patches": []any{newFilePatch(r.who)},
		})
		got.patch = rec.Code

		rec = call(t, h, http.MethodGet, "/api/diff/staging", "", sess, nil)
		got.consult = rec.Code
		if r.patch == http.StatusOK && !strings.Contains(rec.Body.String(), `"id":"p-`+r.who+`"`) {
			t.Fatalf("%s: la proposition n'est pas dans le staging: %s", r.who, rec.Body.String())
		}

		rec = call(t, h, http.MethodPost, "/api/diff/commit", "", sess, map[string]any{
			"sessionId": stagingID, "hunkIds": []string{"p-" + r.who},
		})
		got.commits = rec.Code
		if r.commits == http.StatusForbidden {
			if commitCount(t, repo) != commitsBefore {
				t.Fatalf("%s: le dépôt a reçu un commit malgré le refus", r.who)
			}
			if _, err := os.Stat(filepath.Join(repo, r.who+".txt")); err == nil {
				t.Fatalf("%s: le fichier a été écrit malgré le refus", r.who)
			}
		} else {
			if commitCount(t, repo) != commitsBefore+1 {
				t.Fatalf("%s: commit %d %s, dépôt inchangé", r.who, rec.Code, rec.Body.String())
			}
			if _, err := os.Stat(filepath.Join(repo, r.who+".txt")); err != nil {
				t.Fatalf("%s: fichier absent après commit", r.who)
			}
		}

		t.Logf("%-9s %-5d %-5d %-5d %-8d %-6d", got.who, got.chat, got.open, got.patch, got.consult, got.commits)
		if got != r {
			t.Fatalf("%s: got %+v, want %+v", r.who, got, r)
		}
		for _, code := range []int{got.chat, got.open, got.patch, got.consult, got.commits} {
			if code >= 500 {
				t.Fatalf("%s: réponse %d sur un refus d'autorisation", r.who, code)
			}
		}
	}
}

// Bypassing the UI: a deploy account without the permission posts straight
// to the commit API, for a hunk another account staged. 403, and the live
// repository does not move.
func TestCommitAPIRefusesWithoutWriteTemplates(t *testing.T) {
	_, h, _, repo := rolesFixture(t)
	rec := call(t, h, http.MethodPost, "/api/diff/staging", "", "sess-writer", map[string]any{
		"kind": "template", "patches": []any{newFilePatch("staged")},
	})
	if rec.Code != http.StatusCreated {
		t.Fatal(rec.Code, rec.Body.String())
	}
	var out struct {
		SessionID string `json:"sessionId"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	before := commitCount(t, repo)
	for _, sess := range []string{"sess-deployer", "sess-reader"} {
		rec = call(t, h, http.MethodPost, "/api/diff/commit", "", sess, map[string]any{"sessionId": out.SessionID, "hunkIds": []string{"p-staged"}})
		if rec.Code != http.StatusForbidden {
			t.Fatalf("%s: %d %s", sess, rec.Code, rec.Body.String())
		}
	}
	// An admin acting through a read token is read-only too.
	readToken := makeToken(t, h, "sess-admin", store.RoleRead, 0).Token
	rec = call(t, h, http.MethodPost, "/api/diff/commit", readToken, "", map[string]any{"sessionId": out.SessionID, "hunkIds": []string{"p-staged"}})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("jeton lecture: %d %s", rec.Code, rec.Body.String())
	}
	if commitCount(t, repo) != before {
		t.Fatal("le dépôt a bougé")
	}
	if _, err := os.Stat(filepath.Join(repo, "staged.txt")); err == nil {
		t.Fatal("fichier écrit malgré le refus")
	}
	// Without a session at all: 401, not 500.
	rec = call(t, h, http.MethodPost, "/api/diff/commit", "", "", map[string]any{"hunkIds": []string{"p-staged"}})
	if rec.Code != http.StatusUnauthorized {
		t.Fatal(rec.Code)
	}
}

// Granting then revoking the permission is admin work, and takes effect on
// the next request: no session restart.
func TestWriteTemplatesPermissionIsGrantedExplicitly(t *testing.T) {
	_, h, _, repo := rolesFixture(t)
	rec := call(t, h, http.MethodPut, "/api/users/deployer/permissions", "", "sess-deployer", map[string]any{"write_templates": true})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("un déployeur s'accorde le droit: %d", rec.Code)
	}
	rec = call(t, h, http.MethodPut, "/api/users/deployer/permissions", "", "sess-admin", map[string]any{"write_templates": true})
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"write_templates":true`) {
		t.Fatal(rec.Code, rec.Body.String())
	}
	rec = call(t, h, http.MethodGet, "/api/session", "", "sess-deployer", nil)
	if !strings.Contains(rec.Body.String(), `"write_templates":true`) {
		t.Fatal(rec.Body.String())
	}
	rec = call(t, h, http.MethodPost, "/api/diff/staging", "", "sess-deployer", map[string]any{"kind": "template", "patches": []any{newFilePatch("granted")}})
	var out struct {
		SessionID string `json:"sessionId"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	before := commitCount(t, repo)
	rec = call(t, h, http.MethodPost, "/api/diff/commit", "", "sess-deployer", map[string]any{"sessionId": out.SessionID, "hunkIds": []string{"p-granted"}})
	if rec.Code != http.StatusOK || commitCount(t, repo) != before+1 {
		t.Fatal(rec.Code, rec.Body.String())
	}
	rec = call(t, h, http.MethodPut, "/api/users/deployer/permissions", "", "sess-admin", map[string]any{"write_templates": false})
	if rec.Code != http.StatusOK {
		t.Fatal(rec.Code)
	}
	rec = call(t, h, http.MethodPost, "/api/diff/commit", "", "sess-deployer", map[string]any{"sessionId": out.SessionID, "hunkIds": []string{"p-granted"}})
	if rec.Code != http.StatusForbidden {
		t.Fatal(rec.Code, rec.Body.String())
	}
}

// The ChatGPT sign-in is one credential for the whole instance, like a
// provider key: only an admin starts it, completes it or removes it.
func TestChatGPTSigninIsAdminOnly(t *testing.T) {
	_, h, _, _ := rolesFixture(t)
	routes := []struct{ method, path string }{
		{http.MethodGet, "/api/assistant/openai/start"},
		{http.MethodDelete, "/api/assistant/openai"},
	}
	for _, who := range []string{"reader", "deployer", "writer"} {
		for _, r := range routes {
			if rec := call(t, h, r.method, r.path, "", "sess-"+who, nil); rec.Code != http.StatusForbidden {
				t.Fatalf("%s %s %s: %d, want 403", who, r.method, r.path, rec.Code)
			}
		}
	}
	// Not enabled in the fixture: the admin passes the role check and gets 503.
	if rec := call(t, h, http.MethodGet, "/api/assistant/openai/start", "", "sess-admin", nil); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("admin start: %d", rec.Code)
	}
	if rec := call(t, h, http.MethodDelete, "/api/assistant/openai", "", "sess-admin", nil); rec.Code != http.StatusNoContent {
		t.Fatalf("admin delete: %d", rec.Code)
	}
}

// OpenAI redirects back with no session cookie (SameSite=Strict): the
// callback must answer without one, never with 401.
func TestChatGPTCallbackNeedsNoSession(t *testing.T) {
	a, h, _, _ := rolesFixture(t)
	if err := a.Assistant.EnableSubscription(t.Context(), "MyInfra", "http://127.0.0.1:8080/auth/callback"); err != nil {
		t.Fatal(err)
	}
	rec := call(t, h, http.MethodGet, "/auth/callback?state=unknown&code=c", "", "", nil)
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/assistant-ia?chatgpt=error&step=state" {
		t.Fatalf("%d %q", rec.Code, rec.Header().Get("Location"))
	}
}

// Each person keeps their own conversations: listed latest first, titled by
// the first question, and out of reach of another account, even by id.
func TestAssistantConversationsArePrivate(t *testing.T) {
	_, h, _, _ := rolesFixture(t)
	msgs := map[string]any{"messages": []map[string]any{
		{"role": "user", "content": "Pourquoi blog ne démarre pas ?\nDepuis ce matin."},
		{"role": "assistant", "content": "Je regarde.", "tools": []map[string]string{{"name": "get_application", "title": "Lire une application"}}},
	}}
	if rec := call(t, h, http.MethodPut, "/api/assistant/conversations/c1", "", "sess-admin", msgs); rec.Code != http.StatusNoContent {
		t.Fatalf("put: %d %s", rec.Code, rec.Body.String())
	}
	rec := call(t, h, http.MethodGet, "/api/assistant/conversations", "", "sess-admin", nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"title":"Pourquoi blog ne démarre pas ?"`) {
		t.Fatalf("list: %d %s", rec.Code, rec.Body.String())
	}
	if rec := call(t, h, http.MethodGet, "/api/assistant/conversations/c1", "", "sess-admin", nil); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "get_application") {
		t.Fatalf("get: %d %s", rec.Code, rec.Body.String())
	}
	for _, who := range []string{"reader", "deployer"} {
		if rec := call(t, h, http.MethodGet, "/api/assistant/conversations/c1", "", "sess-"+who, nil); rec.Code != http.StatusNotFound {
			t.Fatalf("%s read another's conversation: %d", who, rec.Code)
		}
		if rec := call(t, h, http.MethodPut, "/api/assistant/conversations/c1", "", "sess-"+who, map[string]any{"messages": []any{}}); rec.Code != http.StatusNotFound {
			t.Fatalf("%s overwrote another's conversation: %d", who, rec.Code)
		}
		if rec := call(t, h, http.MethodGet, "/api/assistant/conversations", "", "sess-"+who, nil); strings.Contains(rec.Body.String(), "c1") {
			t.Fatalf("%s lists another's conversation: %s", who, rec.Body.String())
		}
	}
	if rec := call(t, h, http.MethodDelete, "/api/assistant/conversations/c1", "", "sess-admin", nil); rec.Code != http.StatusNoContent {
		t.Fatalf("delete: %d", rec.Code)
	}
	if rec := call(t, h, http.MethodGet, "/api/assistant/conversations/c1", "", "sess-admin", nil); rec.Code != http.StatusNotFound {
		t.Fatalf("after delete: %d", rec.Code)
	}
}
