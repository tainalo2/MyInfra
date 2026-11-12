package assistant

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	harness "myinfra/internal/harness-router"
)

// TestStatusReturnsAvailableModels verifies that /api/assistant/status
// returns the correct models grouped by provider.
func TestStatusReturnsAvailableModels(t *testing.T) {
	up := httptest.NewServer(http.NewServeMux())
	defer up.Close()
	s := newSvc(t, up.URL, "")

	// Set up credentials for both providers
	s.Keys.Set("claude", "sk-claude")
	s.Keys.Set("codex", "sk-openai")

	mux := http.NewServeMux()
	s.Register(mux, nil)

	// Call status endpoint
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest("GET", "/api/assistant/status", nil))

	if rr.Code != 200 {
		t.Fatalf("status code %d", rr.Code)
	}

	var resp struct {
		Enabled   bool
		Providers []string
		Models    map[string][]ModelCapability
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if !resp.Enabled {
		t.Fatal("should be enabled with credentials")
	}

	// Verify providers list
	if len(resp.Providers) != 2 {
		t.Fatalf("expected 2 providers, got %d", len(resp.Providers))
	}

	// Verify models exist for both providers
	claudeModels, hasclaude := resp.Models["claude"]
	codexModels, hascodex := resp.Models["codex"]

	if !hasclaude || !hascodex {
		t.Fatal("missing provider models")
	}

	if len(claudeModels) == 0 || len(codexModels) == 0 {
		t.Fatal("providers should have models")
	}

	// Verify Claude models have reasoning support
	for _, m := range claudeModels {
		if len(m.SupportedReasons) == 0 {
			t.Fatalf("model %s has no reasoning levels", m.Model)
		}
	}
}

// TestChatRequestWithReasoningEffort verifies that reasoning effort
// is accepted and passed through to the provider.
func TestChatRequestWithReasoningEffort(t *testing.T) {
	up := httptest.NewServer(http.NewServeMux())
	defer up.Close()
	s := newSvc(t, up.URL, "")
	s.Keys.Set("claude", "k")

	// Patch the provider to capture the reasoning effort
	var capturedReason ReasoningEffort
	originalProvider := s.Providers["claude"]
	s.Providers["claude"] = mockProviderWithCapture("claude", &capturedReason, originalProvider.ListModels())

	mux := http.NewServeMux()
	s.Register(mux, AsRole(harness.RoleDeploy))

	// Send chat request with high reasoning effort
	chatReq := `{
		"session": "s1",
		"provider": "claude",
		"model": "claude-sonnet",
		"reasoning_effort": "high",
		"messages": [{"role": "user", "content": "test"}]
	}`

	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest("POST", "/api/assistant/chat", strings.NewReader(chatReq)))

	if rr.Code != 200 {
		t.Fatalf("chat status code %d: %s", rr.Code, rr.Body.String())
	}

	if capturedReason != ReasoningHigh {
		t.Fatalf("expected high reasoning, got %q", capturedReason)
	}
}

// TestChatDefaultsToMediumReasoning verifies that when reasoning_effort
// is not provided, it defaults to medium.
func TestChatDefaultsToMediumReasoning(t *testing.T) {
	up := httptest.NewServer(http.NewServeMux())
	defer up.Close()
	s := newSvc(t, up.URL, "")
	s.Keys.Set("claude", "k")

	var capturedReason ReasoningEffort
	s.Providers["claude"] = mockProviderWithCapture("claude", &capturedReason, s.Providers["claude"].ListModels())

	mux := http.NewServeMux()
	s.Register(mux, AsRole(harness.RoleDeploy))

	// Send chat request WITHOUT reasoning_effort
	chatReq := `{
		"session": "s1",
		"provider": "claude",
		"model": "claude-sonnet",
		"messages": [{"role": "user", "content": "test"}]
	}`

	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest("POST", "/api/assistant/chat", strings.NewReader(chatReq)))

	if rr.Code != 200 {
		t.Fatalf("chat status code %d", rr.Code)
	}

	if capturedReason != ReasoningMedium {
		t.Fatalf("expected medium default reasoning, got %q", capturedReason)
	}
}

// TestModelCapabilityStructure verifies Claude model capabilities are correct.
func TestClaudeModelCapabilities(t *testing.T) {
	claude := Claude{}
	models := claude.ListModels()

	if len(models) == 0 {
		t.Fatal("claude should list models")
	}

	// Check that all Claude models support at least low reasoning
	for _, m := range models {
		if len(m.SupportedReasons) == 0 {
			t.Fatalf("model %s must support at least one reasoning level", m.Model)
		}

		// Find low reasoning support
		hasLow := false
		for _, r := range m.SupportedReasons {
			if r == ReasoningLow {
				hasLow = true
				break
			}
		}

		if !hasLow {
			t.Fatalf("model %s must support low reasoning", m.Model)
		}
	}
}

// Mock provider that captures reasoning effort
func mockProviderWithCapture(name string, captureReason *ReasoningEffort, models []ModelCapability) Provider {
	return &capturingProvider{
		name:          name,
		captureReason: captureReason,
		models:        models,
	}
}

type capturingProvider struct {
	name          string
	captureReason *ReasoningEffort
	models        []ModelCapability
}

func (p *capturingProvider) Name() string { return p.name }
func (p *capturingProvider) Complete(ctx context.Context, model, system string, msgs []Message, key string, effort ReasoningEffort) (Response, error) {
	*p.captureReason = effort
	return Response{Text: "captured", Usage: Usage{InputTokens: 1, OutputTokens: 1}}, nil
}
func (p *capturingProvider) SupportsSubscriptionAuth() bool { return false }
func (p *capturingProvider) ListModels() []ModelCapability  { return p.models }
