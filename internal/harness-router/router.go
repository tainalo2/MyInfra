package harness

import (
	"fmt"
	"time"
)

// RouteRequest is the input to the router.
type RouteRequest struct {
	User             string `json:"user"`
	FunctionalContext string `json:"functional_context"`
	ProviderKey      string `json:"provider_key,omitempty"` // empty => unavailable
}

// RouteResult is returned by Route.
type RouteResult struct {
	AssistantUnavailable bool            `json:"assistant_unavailable"`
	Profile              ContextProfile  `json:"profile,omitempty"`
	AllowedTools         []string        `json:"allowed_tools,omitempty"`
	SystemPrompt         string          `json:"system_prompt,omitempty"`
	MaxTurns             int             `json:"max_turns,omitempty"`
	MaxTokens            int             `json:"max_tokens,omitempty"`
	// ProviderAdapter is the interface handle placeholder; real execution
	// is delegated to the Provider Adapter (separate ticket).
	ProviderAdapter interface{} `json:"-"`
}

// Router holds config and performs routing.
type Router struct {
	Config *Config
}

func NewRouter(cfg *Config) *Router { return &Router{Config: cfg} }

// Route loads the appropriate context profile. If no provider key is configured,
// returns assistant_unavailable:true immediately without any network call.
func (r *Router) Route(req RouteRequest) (RouteResult, error) {
	// Check provider key first — immediate, no network.
	if req.ProviderKey == "" {
		return RouteResult{AssistantUnavailable: true}, nil
	}
	profile, err := r.Config.Get(req.FunctionalContext)
	if err != nil {
		return RouteResult{}, err
	}
	return RouteResult{
		AssistantUnavailable: false,
		Profile:              profile,
		AllowedTools:         append([]string(nil), profile.AllowedTools...),
		SystemPrompt:         profile.SystemPrompt,
		MaxTurns:             profile.MaxTurns,
		MaxTokens:            profile.MaxTokens,
	}, nil
}

// ToolGuard enforces the allowlist at call time.
type ToolGuard struct {
	Allowed map[string]bool
}

func NewToolGuard(profile ContextProfile) *ToolGuard {
	return &ToolGuard{Allowed: profile.AllowedSet()}
}

// Check returns nil if allowed, error otherwise.
func (g *ToolGuard) Check(tool string) error {
	if !g.Allowed[tool] {
		return fmt.Errorf("tool not allowed in this context: %s", tool)
	}
	return nil
}

// CallTool is a guarded dispatcher placeholder; real execution delegated to adapter.
func (g *ToolGuard) CallTool(tool string, args map[string]any, exec func(string, map[string]any) (any, error)) (any, error) {
	if err := g.Check(tool); err != nil {
		return nil, err
	}
	if exec == nil {
		return nil, fmt.Errorf("no executor provided")
	}
	return exec(tool, args)
}

// Benchmark helper for timing.
func RouteTimed(r *Router, req RouteRequest) (RouteResult, time.Duration, error) {
	start := time.Now()
	res, err := r.Route(req)
	return res, time.Since(start), err
}