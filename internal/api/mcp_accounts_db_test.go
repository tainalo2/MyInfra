package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"myinfra/internal/box"
	"myinfra/internal/store"
)

// Provider, DNS and Git logins can be written and replaced through MCP.
// No answer, list or history entry gives the secret back.
func TestMCPAccountSecretsAreWriteOnly(t *testing.T) {
	a, h := tokenFixture(t)
	key := make([]byte, 32)
	sealed, err := box.New(key)
	if err != nil {
		t.Fatal(err)
	}
	a.Box = sealed
	a.Verify.AllowTest = true

	admin := makeToken(t, h, "sess-owner", store.RoleAdmin, 0).Token
	read := makeToken(t, h, "sess-owner", store.RoleRead, 0).Token
	deploy := makeToken(t, h, "sess-owner", store.RoleDeploy, 0).Token

	const hetzner = "test-hetzner-secret-9zq7"
	const hetznerNext = "test-hetzner-secret-3km2"
	const scw = "test-scw-secret-7hqp"
	const ovhSecret = "test-ovh-app-secret-4nwb"
	const ovhConsumer = "test-ovh-consumer-8rkt"
	const cloudflare = "test-cf-token-5vjd"
	const github = "test-gh-token-2plx"
	secrets := []string{hetzner, hetznerNext, scw, ovhSecret, ovhConsumer, cloudflare, github}

	if text, failed := mcpCallTool(t, h, deploy, "create_provider_account", map[string]any{
		"name": "perso", "provider": "hetzner", "secret": hetzner,
	}); !failed || !strings.Contains(text, "Droits insuffisants") || containsAny(text, secrets) {
		t.Fatal(text)
	}

	text, failed := mcpCallTool(t, h, admin, "create_provider_account", map[string]any{
		"name": "perso", "provider": "hetzner", "secret": hetzner,
	})
	if failed || containsAny(text, secrets) || !strings.Contains(text, "hetzner") || !strings.Contains(text, "ne sera pas relu") {
		t.Fatal(text)
	}
	text, failed = mcpCallTool(t, h, admin, "create_provider_account", map[string]any{
		"name": "scw", "provider": "scaleway", "secret": scw, "access_key": "SCWTESTKEY1", "project": "proj-1",
	})
	if failed || containsAny(text, secrets) || !strings.Contains(text, "SCWTESTKEY1") {
		t.Fatal(text)
	}
	text, failed = mcpCallTool(t, h, admin, "create_provider_account", map[string]any{
		"name": "ovh-cloud", "provider": "ovh", "key": "test-ovh-app", "key_secret": ovhSecret, "consumer_key": ovhConsumer,
	})
	if failed || containsAny(text, secrets) || !strings.Contains(text, "test-ovh-app") {
		t.Fatal(text)
	}

	text, failed = mcpCallTool(t, h, read, "list_provider_accounts", map[string]any{})
	if failed || containsAny(text, secrets) || strings.Contains(text, "9zq7") || strings.Contains(text, "7hqp") || strings.Contains(text, "8rkt") {
		t.Fatal(text)
	}
	var listed []map[string]string
	if err := json.Unmarshal([]byte(text), &listed); err != nil || len(listed) != 3 {
		t.Fatal(text, err)
	}
	for _, row := range listed {
		if row["name"] == "scw" && (row["provider"] != "scaleway" || row["login"] != "SCWTESTKEY1") {
			t.Fatal(row)
		}
		if row["name"] == "perso" && (row["provider"] != "hetzner" || row["login"] != "") {
			t.Fatal(row)
		}
		if _, ok := row["hint"]; ok {
			t.Fatal(row)
		}
	}

	rows, err := a.Store.SecretsOfKind(t.Context(), "provider_token")
	if err != nil {
		t.Fatal(err)
	}
	var scwID string
	for _, row := range rows {
		plain := openAccount(t, a, row)
		switch row.Name {
		case "scw":
			scwID = row.ID
			if plain.Token != scw || plain.KeyID != "SCWTESTKEY1" || plain.ProjectID != "proj-1" {
				t.Fatalf("%+v", plain)
			}
		case "ovh-cloud":
			if plain.ApplicationSecret != ovhSecret || plain.ConsumerKey != ovhConsumer || plain.ApplicationKey != "test-ovh-app" {
				t.Fatalf("%+v", plain)
			}
		}
		if rec := call(t, h, http.MethodGet, "/api/secrets/"+row.ID+"/private", admin, "", nil); containsAny(rec.Body.String(), secrets) {
			t.Fatal(rec.Body.String())
		}
	}
	if scwID == "" {
		t.Fatal("compte scaleway introuvable")
	}

	text, failed = mcpCallTool(t, h, admin, "update_provider_account", map[string]any{"name": "perso", "secret": hetznerNext})
	if failed || containsAny(text, secrets) || !strings.Contains(text, "mis à jour") {
		t.Fatal(text)
	}
	perso := accountByName(t, a, "provider_token", "perso")
	if openAccount(t, a, perso).Token != hetznerNext {
		t.Fatal(openAccount(t, a, perso))
	}

	a.Verify.AllowTest = false
	a.Verify.HetznerBase = "http://127.0.0.1:1"
	text, failed = mcpCallTool(t, h, admin, "update_provider_account", map[string]any{"name": "perso", "secret": hetzner})
	if !failed || containsAny(text, secrets) {
		t.Fatal(text)
	}
	a.Verify.AllowTest = true
	if openAccount(t, a, accountByName(t, a, "provider_token", "perso")).Token != hetznerNext {
		t.Fatal("le jeton refusé a été gardé")
	}

	if err := a.Store.CreateServer(t.Context(), "owner", "s1", "solo", "tok", scwID, "", "linked", nil, nil, nil, false); err != nil {
		t.Fatal(err)
	}
	text, failed = mcpCallTool(t, h, admin, "delete_provider_account", map[string]any{"name": "scw"})
	if !failed || !strings.Contains(text, "encore des serveurs") || containsAny(text, secrets) {
		t.Fatal(text)
	}

	text, failed = mcpCallTool(t, h, admin, "create_dns_account", map[string]any{
		"name": "cf", "provider": "cloudflare", "secret": cloudflare,
	})
	if failed || containsAny(text, secrets) {
		t.Fatal(text)
	}
	text, failed = mcpCallTool(t, h, read, "list_dns_accounts", map[string]any{})
	if failed || containsAny(text, secrets) || strings.Contains(text, "5vjd") || !strings.Contains(text, "cloudflare") {
		t.Fatal(text)
	}

	text, failed = mcpCallTool(t, h, admin, "create_git_account", map[string]any{
		"name": "forge", "provider": "github", "secret": github,
	})
	if failed || containsAny(text, secrets) {
		t.Fatal(text)
	}
	text, failed = mcpCallTool(t, h, read, "list_git_accounts", map[string]any{})
	if failed || containsAny(text, secrets) || strings.Contains(text, "2plx") || !strings.Contains(text, "github") {
		t.Fatal(text)
	}
	text, failed = mcpCallTool(t, h, admin, "delete_git_account", map[string]any{"name": "forge"})
	if failed || containsAny(text, secrets) || !strings.Contains(text, "github") {
		t.Fatal(text)
	}
	code := confirmCode.FindStringSubmatch(text)
	if code == nil {
		t.Fatal(text)
	}
	text, failed = mcpCallTool(t, h, admin, "delete_git_account", map[string]any{"name": "forge", "confirmation": code[1]})
	if failed || containsAny(text, secrets) || !strings.Contains(text, "retiré") {
		t.Fatal(text)
	}
	text, failed = mcpCallTool(t, h, read, "list_git_accounts", map[string]any{})
	if failed || strings.TrimSpace(text) != "[]" {
		t.Fatal(text)
	}

	if rec := call(t, h, http.MethodGet, "/api/events?source=mcp", "", "sess-owner", nil); containsAny(rec.Body.String(), secrets) || strings.Contains(rec.Body.String(), "9zq7") {
		t.Fatal(rec.Body.String())
	}
}

func containsAny(text string, parts []string) bool {
	for _, part := range parts {
		if part != "" && strings.Contains(text, part) {
			return true
		}
	}
	return false
}

func accountByName(t *testing.T, a *API, kind, name string) store.Secret {
	t.Helper()
	rows, err := a.Store.SecretsOfKind(t.Context(), kind)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if row.Name == name {
			return row
		}
	}
	t.Fatalf("compte %s introuvable", name)
	return store.Secret{}
}

func openAccount(t *testing.T, a *API, sec store.Secret) storedToken {
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
