package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"myinfra/internal/assistant"
	"myinfra/internal/store"
)

// A copy started without a BYOK master key serves as usual: the assistant
// routes are mounted and answer as unavailable, nothing fails at start.
func TestAssistantRoutesWithoutMasterKey(t *testing.T) {
	helper, err := assistant.New(nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	a := &API{Assistant: helper}
	handler := a.Handler()
	for _, route := range []string{"GET /api/assistant/status", "POST /api/assistant/chat", "PUT /api/assistant/keys/{provider}"} {
		method, path, _ := strings.Cut(route, " ")
		req := httptest.NewRequest(method, strings.ReplaceAll(path, "{provider}", "claude"), nil)
		if _, pattern := a.router.Handler(req); pattern != route {
			t.Fatalf("route %s absente (%q)", route, pattern)
		}
	}
	// Without a session the routes stay behind auth: a reader must sign in.
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/assistant/status", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status sans session %d", rec.Code)
	}
}

func TestAssistantStatusUnavailableWithoutMasterKey(t *testing.T) {
	a, _ := leadAPI(t, false, store.CopyRole{})
	helper, _ := assistant.New(nil, nil, nil)
	a.Assistant = helper
	handler := a.Handler()
	setup := httptest.NewRecorder()
	handler.ServeHTTP(setup, httptest.NewRequest(http.MethodPost, "/api/setup", strings.NewReader(`{"password":"hunter2pass"}`)))
	if setup.Code != http.StatusOK {
		t.Fatalf("setup %d %s", setup.Code, setup.Body.String())
	}
	admin := setup.Result().Cookies()[0]
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/assistant/status", nil)
	req.AddCookie(admin)
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"assistant_unavailable":true`) {
		t.Fatalf("status %d %s", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPut, "/api/assistant/keys/claude", strings.NewReader(`{"key":"sk"}`))
	req.AddCookie(admin)
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("set key %d %s", rec.Code, rec.Body.String())
	}
}

// TestProviderCredentialTypeValidation ensures subscription_token is rejected
// for providers that don't support it (Claude) and accepted for those that do (Codex).
func TestProviderCredentialTypeValidation(t *testing.T) {
	master := make([]byte, 32) // all zeros, for test only
	helper, _ := assistant.New(master, nil, nil)
	a, _ := leadAPI(t, false, store.CopyRole{})
	a.Assistant = helper
	handler := a.Handler()

	setup := httptest.NewRecorder()
	handler.ServeHTTP(setup, httptest.NewRequest(http.MethodPost, "/api/setup", strings.NewReader(`{"password":"hunter2pass"}`)))
	admin := setup.Result().Cookies()[0]

	// Test 1: subscription_token for Claude (unsupported) should return 403
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/assistant/keys/claude",
		strings.NewReader(`{"key":"claude_pro_token_123","type":"subscription_token"}`))
	req.AddCookie(admin)
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("Claude subscription_token expected 403, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "not supported") {
		t.Fatalf("Expected 'not supported' error message, got: %s", rec.Body.String())
	}

	// Test 2: api_key for Claude (supported) should return 204
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPut, "/api/assistant/keys/claude",
		strings.NewReader(`{"key":"sk-ant-xxxxx","type":"api_key"}`))
	req.AddCookie(admin)
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("Claude api_key expected 204, got %d: %s", rec.Code, rec.Body.String())
	}

	// Test 3: subscription_token for Codex (supported) should return 204
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPut, "/api/assistant/keys/codex",
		strings.NewReader(`{"key":"org-123456subscription","type":"subscription_token"}`))
	req.AddCookie(admin)
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("Codex subscription_token expected 204, got %d: %s", rec.Code, rec.Body.String())
	}

	// Test 4: api_key for Codex (also supported) should return 204
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPut, "/api/assistant/keys/codex",
		strings.NewReader(`{"key":"sk-proj-xxxxx","type":"api_key"}`))
	req.AddCookie(admin)
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("Codex api_key expected 204, got %d: %s", rec.Code, rec.Body.String())
	}

	// Test 5: type omitted defaults to api_key (backward compat) - should work for Claude
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPut, "/api/assistant/keys/claude",
		strings.NewReader(`{"key":"sk-ant-legacy"}`))
	req.AddCookie(admin)
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("Claude default type expected 204, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestProviderCapabilitiesEndpoint verifies the /api/assistant/providers endpoint
// correctly reports which providers support subscription auth.
func TestProviderCapabilitiesEndpoint(t *testing.T) {
	master := make([]byte, 32)
	helper, _ := assistant.New(master, nil, nil)
	a, _ := leadAPI(t, false, store.CopyRole{})
	a.Assistant = helper
	handler := a.Handler()

	setup := httptest.NewRecorder()
	handler.ServeHTTP(setup, httptest.NewRequest(http.MethodPost, "/api/setup", strings.NewReader(`{"password":"hunter2pass"}`)))
	admin := setup.Result().Cookies()[0]

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/assistant/providers", nil)
	req.AddCookie(admin)
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("providers endpoint expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	body := rec.Body.String()
	// Verify Claude doesn't support subscription auth
	if !strings.Contains(body, `"provider":"claude"`) || !strings.Contains(body, `"supports_subscription_auth":false`) {
		t.Fatalf("Claude should have supports_subscription_auth:false, got: %s", body)
	}
	// Verify Codex supports subscription auth
	if !strings.Contains(body, `"provider":"codex"`) || !strings.Contains(body, `"supports_subscription_auth":true`) {
		t.Fatalf("Codex should have supports_subscription_auth:true, got: %s", body)
	}
}
