package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"myinfra/internal/box"
	"myinfra/internal/store"
)

// A registry token can be written and replaced through MCP, and no answer,
// list or history entry gives it back.
func TestMCPRegistrySecretIsWriteOnly(t *testing.T) {
	prev := registryLoginCheck
	registryLoginCheck = func(context.Context, string, string, string) error { return nil }
	t.Cleanup(func() { registryLoginCheck = prev })

	a, h := tokenFixture(t)
	key := make([]byte, 32)
	sealed, err := box.New(key)
	if err != nil {
		t.Fatal(err)
	}
	a.Box = sealed

	admin := makeToken(t, h, "sess-owner", store.RoleAdmin, 0).Token
	read := makeToken(t, h, "sess-owner", store.RoleRead, 0).Token
	deploy := makeToken(t, h, "sess-owner", store.RoleDeploy, 0).Token
	const secret = "jeton-privé-ghcr-9f3a"
	const rotated = "autre-jeton-rotation-77"

	if text, failed := mcpCallTool(t, h, deploy, "create_registry", map[string]any{
		"name": "images", "registry": "registry-ghcr", "username": "alice", "secret": secret,
	}); !failed || !strings.Contains(text, "Droits insuffisants") || strings.Contains(text, secret) {
		t.Fatal(text)
	}

	text, failed := mcpCallTool(t, h, admin, "create_registry", map[string]any{
		"name": "images", "registry": "registry-ghcr", "username": "alice", "secret": secret,
	})
	if failed || strings.Contains(text, secret) || !strings.Contains(text, "alice@ghcr.io") || !strings.Contains(text, "ne sera pas relu") {
		t.Fatal(text)
	}

	text, failed = mcpCallTool(t, h, read, "list_registries", map[string]any{})
	if failed || strings.Contains(text, secret) || strings.Contains(text, "9f3a") {
		t.Fatal(text)
	}
	var listed []map[string]string
	if err := json.Unmarshal([]byte(text), &listed); err != nil || len(listed) != 1 {
		t.Fatal(text, err)
	}
	if listed[0]["name"] != "images" || listed[0]["registry"] != "registry-ghcr" || listed[0]["login"] != "alice@ghcr.io" || len(listed[0]) != 3 {
		t.Fatal(listed)
	}

	if rec := call(t, h, http.MethodGet, "/api/secrets", read, "", nil); strings.Contains(rec.Body.String(), secret) {
		t.Fatal(rec.Body.String())
	}
	rows, err := a.Store.SecretsOfKind(t.Context(), registryKind)
	if err != nil || len(rows) != 1 {
		t.Fatal(err, len(rows))
	}
	if rec := call(t, h, http.MethodGet, "/api/secrets/"+rows[0].ID+"/private", admin, "", nil); strings.Contains(rec.Body.String(), secret) {
		t.Fatal(rec.Body.String())
	}
	plain := openRegistry(t, a, rows[0])
	if plain.Token != secret || plain.KeyID != "alice" || plain.ApplicationKey != "ghcr.io" {
		t.Fatalf("%+v", plain)
	}

	if rec := call(t, h, http.MethodGet, "/api/events?source=mcp", "", "sess-owner", nil); strings.Contains(rec.Body.String(), secret) || strings.Contains(rec.Body.String(), "9f3a") {
		t.Fatal(rec.Body.String())
	}

	text, failed = mcpCallTool(t, h, admin, "update_registry", map[string]any{"name": "images", "secret": rotated})
	if failed || strings.Contains(text, secret) || strings.Contains(text, rotated) || !strings.Contains(text, "alice@ghcr.io") {
		t.Fatal(text)
	}
	rows, err = a.Store.SecretsOfKind(t.Context(), registryKind)
	if err != nil || len(rows) != 1 {
		t.Fatal(err, len(rows))
	}
	plain = openRegistry(t, a, rows[0])
	if plain.Token != rotated {
		t.Fatalf("%+v", plain)
	}

	registryLoginCheck = func(context.Context, string, string, string) error {
		return errString("Identifiant refusé par le registre.")
	}
	text, failed = mcpCallTool(t, h, admin, "update_registry", map[string]any{"name": "images", "secret": secret})
	if !failed || strings.Contains(text, secret) || !strings.Contains(text, "refusé") {
		t.Fatal(text)
	}
	rows, err = a.Store.SecretsOfKind(t.Context(), registryKind)
	if err != nil || len(rows) != 1 {
		t.Fatal(err, len(rows))
	}
	plain = openRegistry(t, a, rows[0])
	if plain.Token != rotated {
		t.Fatal("le jeton refusé a été gardé")
	}

	text, failed = mcpCallTool(t, h, admin, "delete_registry", map[string]any{"name": "images"})
	if failed || strings.Contains(text, secret) || strings.Contains(text, rotated) || !strings.Contains(text, "alice@ghcr.io") {
		t.Fatal(text)
	}
	code := confirmCode.FindStringSubmatch(text)
	if code == nil {
		t.Fatal(text)
	}
	text, failed = mcpCallTool(t, h, admin, "delete_registry", map[string]any{"name": "images", "confirmation": code[1]})
	if failed || strings.Contains(text, rotated) || !strings.Contains(text, "retiré") {
		t.Fatal(text)
	}
	text, failed = mcpCallTool(t, h, read, "list_registries", map[string]any{})
	if failed || strings.TrimSpace(text) != "[]" {
		t.Fatal(text)
	}
}

func openRegistry(t *testing.T, a *API, sec store.Secret) storedToken {
	t.Helper()
	raw, err := a.Box.Open(box.Sealed{Ciphertext: sec.Ciphertext, Nonce: sec.Nonce, WrappedDEK: sec.WrappedDEK})
	if err != nil {
		t.Fatal(err)
	}
	var tok storedToken
	if err := json.Unmarshal(raw, &tok); err != nil {
		t.Fatal(err)
	}
	return tok
}
