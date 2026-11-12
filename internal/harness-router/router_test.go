package harness

import (
	"os"
	"testing"
	"time"
)

const testConfig = `
contexts:
  general:
    name: general
    system_prompt: "You are a helpful assistant."
    allowed_tools: []
    max_turns: 10
    max_tokens: 4000
  
  template_editor:
    name: template_editor
    system_prompt: "You may only edit the template."
    allowed_tools:
      - read_template
      - patch_template
      - validate_template
    max_turns: 10
    max_tokens: 4000

pages:
  - page: template_editor
    context: template_editor
`

func TestConfigDriven(t *testing.T) {
	cfg, err := ParseConfig([]byte(testConfig))
	if err != nil {
		t.Fatal(err)
	}
	p, err := cfg.Get("template_editor")
	if err != nil {
		t.Fatal(err)
	}
	if len(p.AllowedTools) != 3 {
		t.Fatalf("expected 3 tools, got %d", len(p.AllowedTools))
	}
	// changing config (in memory) needs no code change
	cfg.Contexts["template_editor"] = ContextProfile{
		Name:         "template_editor",
		SystemPrompt: "x",
		AllowedTools: []string{"read_template", "patch_template", "validate_template", "extra_tool"},
		MaxTurns:     5,
		MaxTokens:    100,
	}
	p2, _ := cfg.Get("template_editor")
	if len(p2.AllowedTools) != 4 {
		t.Fatal("config change not reflected")
	}
}

func TestPageToContextMapping(t *testing.T) {
	cfg, _ := ParseConfig([]byte(testConfig))
	// template_editor page maps to template_editor context
	ctx := cfg.GetContextForPage("template_editor")
	if ctx != "template_editor" {
		t.Fatalf("expected template_editor, got %s", ctx)
	}
	// Unknown page falls back to general
	ctx = cfg.GetContextForPage("unknown_page")
	if ctx != "general" {
		t.Fatalf("expected general for unknown page, got %s", ctx)
	}
	// Empty page falls back to general
	ctx = cfg.GetContextForPage("")
	if ctx != "general" {
		t.Fatalf("expected general for empty page, got %s", ctx)
	}
}

func TestToolGuardRejects(t *testing.T) {
	cfg, _ := ParseConfig([]byte(testConfig))
	p, _ := cfg.Get("template_editor")
	g := NewToolGuard(p)
	if err := g.Check("read_template"); err != nil {
		t.Fatal(err)
	}
	if err := g.Check("delete_user"); err == nil {
		t.Fatal("expected rejection for disallowed tool")
	}
	// CallTool returns error, no crash
	_, err := g.CallTool("delete_user", nil, nil)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestUnavailableNoKey(t *testing.T) {
	cfg, _ := ParseConfig([]byte(testConfig))
	r := NewRouter(cfg)
	start := time.Now()
	res, err := r.Route(RouteRequest{User: "u", Role: RoleDeploy, Page: "template_editor", ProviderKey: ""}, nil)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatal(err)
	}
	if !res.AssistantUnavailable {
		t.Fatal("expected assistant_unavailable true")
	}
	if elapsed > 50*time.Millisecond {
		t.Fatalf("too slow: %v", elapsed)
	}
	// ensure no network: we can't easily observe, but no HTTP calls made by code.
	_ = os.Args
}

func TestLoadConfigFile(t *testing.T) {
	f, _ := os.CreateTemp("", "cfg*.yaml")
	defer os.Remove(f.Name())
	f.WriteString(testConfig)
	f.Close()
	cfg, err := LoadConfig(f.Name())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cfg.Get("template_editor"); err != nil {
		t.Fatal(err)
	}
}

// A read-only role, or no role at all, is refused before the provider key
// is even looked at: no profile, no network.
func TestRouteRefusesReadOnlyRole(t *testing.T) {
	cfg, _ := ParseConfig([]byte(testConfig))
	r := NewRouter(cfg)
	for _, role := range []string{RoleRead, "", "bogus"} {
		_, err := r.Route(RouteRequest{User: "u", Role: role, Page: "template_editor", ProviderKey: "sk-live"}, nil)
		if err != ErrReadOnly {
			t.Fatalf("role %q: err=%v, want ErrReadOnly", role, err)
		}
	}
	for _, role := range []string{RoleDeploy, RoleAdmin} {
		res, err := r.Route(RouteRequest{User: "u", Role: role, Page: "template_editor", ProviderKey: "sk-live"}, nil)
		if err != nil || res.AssistantUnavailable {
			t.Fatalf("role %q: err=%v res=%+v", role, err, res)
		}
	}
}

// Test dynamic context resolution: template_editor page with successful
// resource access verification should use template_editor context.
func TestDynamicContextResolution(t *testing.T) {
	cfg, _ := ParseConfig([]byte(testConfig))
	r := NewRouter(cfg)
	
	// Verifier that grants access
	grantingVerifier := func(userID, resourceID string) bool {
		return userID == "alice" && resourceID == "tpl-123"
	}
	
	// User alice with tpl-123 → template_editor context (has edit tools)
	res, err := r.Route(RouteRequest{
		User:       "alice",
		Role:       RoleDeploy,
		Page:       "template_editor",
		ResourceID: "tpl-123",
		ProviderKey: "sk-live",
	}, grantingVerifier)
	if err != nil || len(res.AllowedTools) == 0 {
		t.Fatalf("expected tools for template_editor context, got: %+v", res)
	}
	if !contains(res.AllowedTools, "patch_template") {
		t.Fatal("expected patch_template tool in template_editor context")
	}
	
	// User bob with tpl-123 → general context (access denied, falls back)
	res2, err := r.Route(RouteRequest{
		User:       "bob",
		Role:       RoleDeploy,
		Page:       "template_editor",
		ResourceID: "tpl-123",
		ProviderKey: "sk-live",
	}, grantingVerifier)
	if err != nil || len(res2.AllowedTools) > 0 {
		t.Fatalf("expected general context (no tools), got: %+v", res2)
	}
}

// Test read-only role always falls back to general context, even with
// valid resource access.
func TestReadOnlyFallbackToGeneral(t *testing.T) {
	cfg, _ := ParseConfig([]byte(testConfig))
	r := NewRouter(cfg)
	
	// Verifier that would grant access
	grantingVerifier := func(userID, resourceID string) bool {
		return true
	}
	
	// Read-only role with template_editor page → still refused (ErrReadOnly)
	_, err := r.Route(RouteRequest{
		User:       "alice",
		Role:       RoleRead,
		Page:       "template_editor",
		ResourceID: "tpl-123",
		ProviderKey: "sk-live",
	}, grantingVerifier)
	if err != ErrReadOnly {
		t.Fatalf("expected ErrReadOnly, got %v", err)
	}
}

// Helper function to check if a slice contains a string
func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}
