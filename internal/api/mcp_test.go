package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

func TestMCPToolsAreWellFormed(t *testing.T) {
	a := &API{}
	a.Handler()
	seen := map[string]bool{}
	name := regexp.MustCompile(`^[a-z][a-z0-9_]{2,40}$`)
	param := regexp.MustCompile(`\{[a-z]+\}`)
	for _, tool := range a.mcpTools() {
		if !name.MatchString(tool.Name) || seen[tool.Name] {
			t.Fatalf("nom %q invalide ou en double", tool.Name)
		}
		seen[tool.Name] = true
		if tool.Title == "" || len(tool.Description) < 20 || tool.Run == nil {
			t.Fatalf("%s : titre, description ou action manquants", tool.Name)
		}
		if roleRank(tool.Role) == 0 {
			t.Fatalf("%s : rôle %q", tool.Name, tool.Role)
		}
		if tool.Destructive && !tool.Write {
			t.Fatalf("%s : destructif mais en lecture", tool.Name)
		}
		if tool.Schema["type"] != "object" {
			t.Fatalf("%s : schéma", tool.Name)
		}
		if _, err := json.Marshal(tool.listing()); err != nil {
			t.Fatalf("%s : %v", tool.Name, err)
		}
		if tool.Destructive {
			props, _ := tool.Schema["properties"].(map[string]any)
			if _, ok := props["confirmation"]; !ok {
				t.Fatalf("%s : destructif sans confirmation", tool.Name)
			}
		}
		if len(tool.Routes) == 0 {
			t.Fatalf("%s : aucune route déclarée", tool.Name)
		}
		// Each route a tool calls exists, under this very pattern.
		for _, route := range tool.Routes {
			method, path, _ := strings.Cut(route, " ")
			req := httptest.NewRequest(method, param.ReplaceAllString(path, "x"), nil)
			if _, pattern := a.router.Handler(req); pattern != route {
				t.Fatalf("%s : la route %q n'existe pas (trouvé %q)", tool.Name, route, pattern)
			}
		}
	}
}

func TestCheckSchema(t *testing.T) {
	schema := object([]string{"name"}, map[string]any{
		"name":   text("x"),
		"kind":   choice("x", "a", "b"),
		"copies": whole("x", 1, 10),
		"tags":   list("x", text("x")),
		"vars":   textMap("x"),
		"ok":     flag("x"),
	})
	good := map[string]any{"name": "n", "kind": "a", "copies": float64(3), "tags": []any{"x"}, "vars": map[string]any{"A": "1"}, "ok": true}
	if err := checkSchema(schema, good, ""); err != nil {
		t.Fatal(err)
	}
	for label, bad := range map[string]map[string]any{
		"requis":  {},
		"inconnu": {"name": "n", "other": 1},
		"enum":    {"name": "n", "kind": "c"},
		"entier":  {"name": "n", "copies": 1.5},
		"maximum": {"name": "n", "copies": float64(11)},
		"type":    {"name": float64(1)},
		"liste":   {"name": "n", "tags": []any{float64(1)}},
		"carte":   {"name": "n", "vars": map[string]any{"A": float64(1)}},
	} {
		if err := checkSchema(schema, bad, ""); err == nil {
			t.Fatalf("%s : accepté", label)
		}
	}
}

func TestPickNeverGuesses(t *testing.T) {
	rows := []named{{"1", "SW"}, {"2", "prod"}, {"3", "Prod"}}
	if row, err := pickNamed("cluster", "sw", rows); err != nil || row.ID != "1" {
		t.Fatal(row, err)
	}
	if row, err := pickNamed("cluster", "2", rows); err != nil || row.Name != "prod" {
		t.Fatal(row, err)
	}
	if _, err := pickNamed("cluster", "PROD", rows); err == nil || !strings.Contains(err.Error(), "Plusieurs") {
		t.Fatal(err)
	}
	if _, err := pickNamed("cluster", "staging", rows); err == nil || !strings.Contains(err.Error(), "Disponibles : Prod, SW, prod") {
		t.Fatal(err)
	}
}

func TestArgsSubject(t *testing.T) {
	got := argsSubject(json.RawMessage(`{"article":"nextcloud","cluster":"SW","domains":["a.b.c"],"copies":3,"confirmation":"X"}`))
	if got != "article=nextcloud, cluster=SW, domains=a.b.c, copies=3" {
		t.Fatal(got)
	}
}

// mcpPost sends one JSON-RPC body to /mcp.
func mcpPost(t *testing.T, h http.Handler, bearer string, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestMCPRefusesWithoutToken(t *testing.T) {
	h := (&API{}).Handler()
	rec := mcpPost(t, h, "", `{"jsonrpc":"2.0","id":1,"method":"ping"}`)
	if rec.Code != http.StatusUnauthorized || rec.Header().Get("WWW-Authenticate") == "" {
		t.Fatal(rec.Code, rec.Body.String())
	}
	req := httptest.NewRequest(http.MethodGet, "/mcp", nil)
	got := httptest.NewRecorder()
	h.ServeHTTP(got, req)
	if got.Code != http.StatusMethodNotAllowed {
		t.Fatal(got.Code)
	}
	req = httptest.NewRequest(http.MethodGet, "/mcp", nil)
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	got = httptest.NewRecorder()
	h.ServeHTTP(got, req)
	if got.Code != http.StatusFound || got.Header().Get("Location") != "/assistants" {
		t.Fatal(got.Code, got.Header())
	}
	req = httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader("{}"))
	req.Header.Set("Origin", "https://evil.example")
	got = httptest.NewRecorder()
	h.ServeHTTP(got, req)
	if got.Code != http.StatusForbidden {
		t.Fatal(got.Code)
	}
}
