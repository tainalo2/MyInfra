package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"myinfra/internal/store"
)

type mcpAnswer struct {
	ID     json.RawMessage `json:"id"`
	Result struct {
		ProtocolVersion string           `json:"protocolVersion"`
		Instructions    string           `json:"instructions"`
		Tools           []map[string]any `json:"tools"`
		Content         []struct {
			Text string `json:"text"`
		} `json:"content"`
		IsError  bool             `json:"isError"`
		Contents []map[string]any `json:"contents"`
	} `json:"result"`
	Error *rpcError `json:"error"`
}

func mcpRPC(t *testing.T, h http.Handler, bearer, method string, params any) mcpAnswer {
	t.Helper()
	raw, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 7, "method": method, "params": params})
	rec := mcpPost(t, h, bearer, string(raw))
	if rec.Code != http.StatusOK {
		t.Fatal(method, rec.Code, rec.Body.String())
	}
	var out mcpAnswer
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err, rec.Body.String())
	}
	return out
}

// mcpTool calls one tool and returns its text and whether it failed.
func mcpCallTool(t *testing.T, h http.Handler, bearer, name string, args any) (string, bool) {
	t.Helper()
	out := mcpRPC(t, h, bearer, "tools/call", map[string]any{"name": name, "arguments": args})
	if out.Error != nil {
		t.Fatal(name, out.Error.Message)
	}
	if len(out.Result.Content) == 0 {
		t.Fatal(name, "sans contenu")
	}
	return out.Result.Content[0].Text, out.Result.IsError
}

func TestMCPProtocol(t *testing.T) {
	_, h := tokenFixture(t)
	read := makeToken(t, h, "sess-owner", store.RoleRead, 0).Token

	init := mcpRPC(t, h, read, "initialize", map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "test"}})
	if init.Result.ProtocolVersion != "2025-06-18" || !strings.Contains(init.Result.Instructions, "deux temps") {
		t.Fatal(init.Result.ProtocolVersion, init.Result.Instructions)
	}
	if got := mcpRPC(t, h, read, "initialize", map[string]any{"protocolVersion": "1999-01-01"}); got.Result.ProtocolVersion != mcpVersions[0] {
		t.Fatal(got.Result.ProtocolVersion)
	}
	if rec := mcpPost(t, h, read, `{"jsonrpc":"2.0","method":"notifications/initialized"}`); rec.Code != http.StatusAccepted {
		t.Fatal(rec.Code)
	}
	if got := mcpRPC(t, h, read, "nothing/here", nil); got.Error == nil || got.Error.Code != rpcNoMethod {
		t.Fatal(got.Error)
	}
	rec := mcpPost(t, h, read, `[{"jsonrpc":"2.0","id":1,"method":"ping"},{"jsonrpc":"2.0","method":"notifications/initialized"},{"jsonrpc":"2.0","id":2,"method":"ping"}]`)
	var batch []mcpAnswer
	if err := json.Unmarshal(rec.Body.Bytes(), &batch); err != nil || len(batch) != 2 {
		t.Fatal(rec.Body.String())
	}

	text, failed := mcpCallTool(t, h, read, "list_clusters", map[string]any{})
	if failed || strings.TrimSpace(text) != "[]" {
		t.Fatal(text)
	}
	text, failed = mcpCallTool(t, h, read, "list_clusters", map[string]any{"bogus": 1})
	if !failed || !strings.Contains(text, "champ inconnu") {
		t.Fatal(text)
	}
	text, failed = mcpCallTool(t, h, read, "get_cluster_usage", map[string]any{"cluster": "SW"})
	if !failed || !strings.Contains(text, "Aucun(e) cluster") {
		t.Fatal(text)
	}
	if got := mcpRPC(t, h, read, "tools/call", map[string]any{"name": "nope"}); got.Error == nil || got.Error.Code != rpcInvalidParams {
		t.Fatal(got.Error)
	}

	res := mcpRPC(t, h, read, "resources/read", map[string]any{"uri": guideURI})
	if len(res.Result.Contents) != 1 || !strings.Contains(fmt.Sprint(res.Result.Contents[0]["text"]), "myinfra-package") {
		t.Fatal(res.Result.Contents)
	}
	res = mcpRPC(t, h, read, "resources/read", map[string]any{"uri": "myinfra://catalogue/nextcloud"})
	if len(res.Result.Contents) != 1 || !strings.Contains(fmt.Sprint(res.Result.Contents[0]["text"]), "mariadb") {
		t.Fatal(res.Result.Contents)
	}
}

// Each tool is offered only to the roles its routes accept, and each route
// it calls lets its role through, as the API decides for a session.
func TestMCPToolsFollowRoles(t *testing.T) {
	a, h := tokenFixture(t)
	tokens := map[string]string{}
	for _, role := range []string{store.RoleRead, store.RoleDeploy, store.RoleAdmin} {
		tokens[role] = makeToken(t, h, "sess-owner", role, 0).Token
	}
	listed := map[string]map[string]bool{}
	for role, token := range tokens {
		listed[role] = map[string]bool{}
		for _, tool := range mcpRPC(t, h, token, "tools/list", nil).Result.Tools {
			listed[role][fmt.Sprint(tool["name"])] = true
		}
	}
	param := regexp.MustCompile(`\{[a-z]+\}`)
	lower := map[string]string{store.RoleDeploy: store.RoleRead, store.RoleAdmin: store.RoleDeploy}
	for _, tool := range a.mcpTools() {
		for role := range tokens {
			want := roleRank(role) >= roleRank(tool.Role)
			if listed[role][tool.Name] != want {
				t.Fatalf("%s listé pour %s : %v", tool.Name, role, listed[role][tool.Name])
			}
		}
		refusedBelow := false
		for _, route := range tool.Routes {
			method, path, _ := strings.Cut(route, " ")
			path = param.ReplaceAllString(path, "none")
			var body any
			if method != http.MethodGet {
				body = map[string]any{}
			}
			if rec := call(t, h, method, path, tokens[tool.Role], "", body); rec.Code == http.StatusForbidden {
				t.Fatalf("%s : %s refusée au rôle %s : %s", tool.Name, route, tool.Role, rec.Body.String())
			}
			if below, ok := lower[tool.Role]; ok {
				if rec := call(t, h, method, path, tokens[below], "", body); rec.Code == http.StatusForbidden {
					refusedBelow = true
				}
			}
		}
		if _, ok := lower[tool.Role]; ok && !refusedBelow {
			t.Fatalf("%s demande %s alors que ses routes acceptent moins", tool.Name, tool.Role)
		}
		if roleRank(tool.Role) > roleRank(store.RoleRead) {
			text, failed := mcpCallTool(t, h, tokens[store.RoleRead], tool.Name, map[string]any{})
			if !failed || !strings.Contains(text, "Droits insuffisants") {
				t.Fatalf("%s accepté en lecture : %s", tool.Name, text)
			}
		}
	}
}

var confirmCode = regexp.MustCompile(`confirmation="([A-Z0-9]{3}-[A-Z0-9]{3})"`)

// A deletion through MCP shows what goes first, and runs only with the
// code the same token got, for the same arguments, once.
func TestMCPDeletionAsksFirst(t *testing.T) {
	a, h := tokenFixture(t)
	ctx := t.Context()
	if err := a.Store.CreateServer(ctx, "owner", "s1", "solo", "tok-solo-123", "", "", "linked", nil, nil, nil, false); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"demo", "other"} {
		if err := a.Store.CreateApplication(ctx, store.NewApplication{
			ID: "app-" + name, OwnerID: "owner", Name: name, SourceKind: "image", Source: "nginx:1",
			DestinationKind: "server", ServerID: "s1", Replicas: 1, DefaultID: "d-" + name, DefaultHost: name + ".apps.test",
		}); err != nil {
			t.Fatal(err)
		}
	}
	deploy := makeToken(t, h, "sess-owner", store.RoleDeploy, 0).Token
	other := makeToken(t, h, "sess-owner", store.RoleDeploy, 0).Token

	text, failed := mcpCallTool(t, h, deploy, "delete_application", map[string]any{"name": "demo"})
	match := confirmCode.FindStringSubmatch(text)
	if failed || match == nil || !strings.Contains(text, "Applications supprimées : demo") {
		t.Fatal(text)
	}
	if _, err := a.Store.Application(ctx, "owner", "app-demo"); err != nil {
		t.Fatal("supprimée avant l'accord", err)
	}
	// Another token, or other arguments, cannot use the code.
	if text, failed := mcpCallTool(t, h, other, "delete_application", map[string]any{"name": "demo", "confirmation": match[1]}); !failed || !strings.Contains(text, "ne correspond pas") {
		t.Fatal(text)
	}
	text, _ = mcpCallTool(t, h, deploy, "delete_application", map[string]any{"name": "demo"})
	code := confirmCode.FindStringSubmatch(text)[1]
	if text, failed := mcpCallTool(t, h, deploy, "delete_application", map[string]any{"name": "other", "confirmation": code}); !failed || !strings.Contains(text, "ne correspond pas") {
		t.Fatal(text)
	}
	text, _ = mcpCallTool(t, h, deploy, "delete_application", map[string]any{"name": "demo"})
	code = confirmCode.FindStringSubmatch(text)[1]
	text, failed = mcpCallTool(t, h, deploy, "delete_application", map[string]any{"name": "demo", "confirmation": strings.ToLower(code)})
	if failed || !strings.Contains(text, "Suppression de « demo » lancée") {
		t.Fatal(text)
	}
	if text, failed := mcpCallTool(t, h, deploy, "delete_application", map[string]any{"name": "demo", "confirmation": code}); !failed || !strings.Contains(text, "déjà utilisé") {
		t.Fatal(text)
	}
	rec := call(t, h, http.MethodGet, "/api/events?source=mcp", "", "sess-owner", nil)
	body := rec.Body.String()
	for _, want := range []string{`"kind":"app.delete"`, `"kind":"mcp.call"`, `"kind":"mcp.refused"`} {
		if !strings.Contains(body, want) {
			t.Fatal(want, body)
		}
	}
}
