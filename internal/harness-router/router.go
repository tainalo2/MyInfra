package harness

import (
	"errors"
	"fmt"
	"time"
)

// Roles the router knows. They mirror the account roles of MyInfra: a
// read-only account may consult the assistant's past work but never opens
// a new turn with a provider, so the router refuses before any network.
const (
	RoleAdmin  = "admin"
	RoleDeploy = "deploy"
	RoleRead   = "read"
)

// ErrReadOnly is returned by Route when the role may not start a turn.
var ErrReadOnly = errors.New("harness: read-only role may not call the assistant")

// CanChat tells whether a role may open an assistant turn (chat, patch
// proposals). An empty role is unknown and refused: the caller must say.
func CanChat(role string) bool {
	return role == RoleAdmin || role == RoleDeploy
}

// RouteRequest is the input to the router.
type RouteRequest struct {
	User              string `json:"user"`
	Role              string `json:"role,omitempty"` // empty => refused
	Page              string `json:"page,omitempty"` // page name for dynamic context resolution
	ResourceID        string `json:"resource_id,omitempty"` // resource identifier for access control
	ProviderKey       string `json:"provider_key,omitempty"` // empty => unavailable
}

// RouteResult is returned by Route.
type RouteResult struct {
	AssistantUnavailable bool           `json:"assistant_unavailable"`
	Profile              ContextProfile `json:"profile,omitempty"`
	AllowedTools         []string       `json:"allowed_tools,omitempty"`
	SystemPrompt         string         `json:"system_prompt,omitempty"`
	MaxTurns             int            `json:"max_turns,omitempty"`
	MaxTokens            int            `json:"max_tokens,omitempty"`
	// ProviderAdapter is the interface handle placeholder; real execution
	// is delegated to the Provider Adapter (separate ticket).
	ProviderAdapter interface{} `json:"-"`
}

// Router holds config and performs routing.
type Router struct {
	Config *Config
}

func NewRouter(cfg *Config) *Router { return &Router{Config: cfg} }

// Route loads the appropriate context profile based on the page and resource access.
// If no provider key is configured, returns assistant_unavailable:true immediately without any network call.
// 
// Dynamic context resolution:
// - If page is provided, looks up the context name via config.GetContextForPage()
// - If ResourceID is provided and page maps to a context requiring access verification,
//   the backend MUST verify the user has access (implementation delegated to the caller).
// - A read-only role always falls back to "general" context regardless of page.
// - Unknown pages fall back to "general" context.
func (r *Router) Route(req RouteRequest, resourceAccessVerifier func(userID, resourceID string) bool) (RouteResult, error) {
	// Authorization first: a read-only role never reaches a provider.
	// Always fall back to general context for read-only roles.
	if !CanChat(req.Role) {
		return RouteResult{}, ErrReadOnly
	}
	// Check provider key first — immediate, no network.
	if req.ProviderKey == "" {
		return RouteResult{AssistantUnavailable: true}, nil
	}
	
	// Resolve the functional context name from page.
	contextName := "general"
	if req.Page != "" {
		contextName = r.Config.GetContextForPage(req.Page)
	}
	
	// If the resolved context is "template_editor" and we have a resource ID,
	// verify the user has access to that resource. If verification fails, fall back to general.
	if contextName == "template_editor" && req.ResourceID != "" {
		if resourceAccessVerifier == nil || !resourceAccessVerifier(req.User, req.ResourceID) {
			// Access denied; fall back to general context
			contextName = "general"
		}
	}
	
	profile, err := r.Config.Get(contextName)
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
func RouteTimed(r *Router, req RouteRequest, resourceAccessVerifier func(userID, resourceID string) bool) (RouteResult, time.Duration, error) {
	start := time.Now()
	res, err := r.Route(req, resourceAccessVerifier)
	return res, time.Since(start), err
}
