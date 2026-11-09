package harness

import (
	"os"
	"testing"
	"time"
)

const testConfig = `
contexts:
  template_edit:
    name: template_edit
    system_prompt: "You may only edit the template."
    allowed_tools:
      - read_template
      - patch_template
      - validate_template
    max_turns: 10
    max_tokens: 4000
`

func TestConfigDriven(t *testing.T) {
	cfg, err := ParseConfig([]byte(testConfig))
	if err != nil {
		t.Fatal(err)
	}
	p, err := cfg.Get("template_edit")
	if err != nil {
		t.Fatal(err)
	}
	if len(p.AllowedTools) != 3 {
		t.Fatalf("expected 3 tools, got %d", len(p.AllowedTools))
	}
	// changing config (in memory) needs no code change
	cfg.Contexts["template_edit"] = ContextProfile{
		Name:         "template_edit",
		SystemPrompt: "x",
		AllowedTools: []string{"read_template", "patch_template", "validate_template", "extra_tool"},
		MaxTurns:     5,
		MaxTokens:    100,
	}
	p2, _ := cfg.Get("template_edit")
	if len(p2.AllowedTools) != 4 {
		t.Fatal("config change not reflected")
	}
}

func TestToolGuardRejects(t *testing.T) {
	cfg, _ := ParseConfig([]byte(testConfig))
	p, _ := cfg.Get("template_edit")
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
	res, err := r.Route(RouteRequest{User: "u", FunctionalContext: "template_edit", ProviderKey: ""})
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
	if _, err := cfg.Get("template_edit"); err != nil {
		t.Fatal(err)
	}
}