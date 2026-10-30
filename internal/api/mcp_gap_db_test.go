package api

import (
	"encoding/json"
	"strings"
	"testing"

	"myinfra/internal/store"
)

func TestMCPCoversTheMissingSurfaces(t *testing.T) {
	a, h := tokenFixture(t)
	ctx := t.Context()
	if err := a.Store.CreateServer(ctx, "owner", "s-hand", "manuel", "", "", "", "linked", nil, nil, nil, false); err != nil {
		t.Fatal(err)
	}
	admin := makeToken(t, h, "sess-owner", store.RoleAdmin, 0).Token
	deploy := makeToken(t, h, "sess-owner", store.RoleDeploy, 0).Token
	read := makeToken(t, h, "sess-owner", store.RoleRead, 0).Token

	tools := mcpRPC(t, h, read, "tools/list", nil).Result.Tools
	var schema string
	for _, tool := range tools {
		if tool["name"] == "find_server_offers" {
			schema = strings.ToLower(string(mustJSON(t, tool["inputSchema"])))
		}
	}
	if !strings.Contains(schema, "min_gpus") || !strings.Contains(schema, "gpu") {
		t.Fatal("recherche sans GPU")
	}

	text, failed := mcpCallTool(t, h, read, "list_host_tools", map[string]any{})
	if failed || !strings.Contains(text, "ansible") || !strings.Contains(text, "tofu") {
		t.Fatal(text, failed)
	}
	text, failed = mcpCallTool(t, h, read, "list_templates", map[string]any{})
	if failed || text != "[]" {
		t.Fatal(text, failed)
	}
	text, failed = mcpCallTool(t, h, read, "get_copies", map[string]any{})
	if failed || !strings.Contains(text, "replica") {
		t.Fatal(text, failed)
	}
	text, failed = mcpCallTool(t, h, read, "list_starters", map[string]any{})
	if failed || !strings.Contains(text, "[") {
		t.Fatal(text, failed)
	}
	text, failed = mcpCallTool(t, h, read, "get_server_runtime", map[string]any{"server": "manuel"})
	if failed || !strings.Contains(text, "pods") {
		t.Fatal(text, failed)
	}

	text, failed = mcpCallTool(t, h, deploy, "set_server_place", map[string]any{"server": "manuel", "place": "Paris"})
	if failed || !strings.Contains(text, "enregistré") {
		t.Fatal(text, failed)
	}
	text, failed = mcpCallTool(t, h, read, "get_server", map[string]any{"server": "manuel"})
	if failed || !strings.Contains(text, "Paris") {
		t.Fatal(text, failed)
	}
	text, failed = mcpCallTool(t, h, deploy, "create_template", map[string]any{
		"name": "petit",
		"body": map[string]any{"servers": []any{map[string]any{"name": "web"}}},
	})
	if failed || !strings.Contains(text, "petit") {
		t.Fatal(text, failed)
	}
	text, failed = mcpCallTool(t, h, deploy, "delete_template", map[string]any{"template": "petit"})
	if failed || !strings.Contains(text, "confirmation=") {
		t.Fatal(text, failed)
	}
	text, failed = mcpCallTool(t, h, deploy, "run_template", map[string]any{"template": "petit"})
	if failed || !strings.Contains(text, "confirmation=") || !strings.Contains(text, "web") {
		t.Fatal(text, failed)
	}

	text, failed = mcpCallTool(t, h, admin, "create_ssh_key", map[string]any{"name": "poste"})
	if failed || strings.Contains(text, "PRIVATE") || strings.Contains(text, "privateKey") || !strings.Contains(text, "ssh-") {
		t.Fatal(text, failed)
	}
	text, failed = mcpCallTool(t, h, admin, "import_ssh_key", map[string]any{
		"name": "refusee", "public_key": "pas une clé", "private_key": "secret-privé-ssh-9f3a",
	})
	if !failed || strings.Contains(text, "secret-privé") {
		t.Fatal(text, failed)
	}
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
