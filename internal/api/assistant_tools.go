package api

import (
	"context"
	"encoding/json"
	"net/http"

	"myinfra/internal/assistant"
)

// sourceAssistant marks the requests the assistant of the interface makes
// through the MCP tools: the history says so, and a confirmation code is
// bound to the person rather than to an access token.
const sourceAssistant = "assistant"

// assistantToolbox gives the MCP tools to the assistant built into the
// interface. They run exactly as for the MCP server: through the same
// router, with the person's session, so roles, replicas and every check of
// the API hold as for a click in the interface.
type assistantToolbox struct {
	s *mcpSession
}

// assistantTools builds the toolbox of one chat request; inner is the
// router the tools call through.
func (a *API) assistantTools(inner http.Handler) func(r *http.Request) assistant.Toolbox {
	return func(r *http.Request) assistant.Toolbox {
		outer := r.WithContext(withSource(r.Context(), sourceAssistant))
		return &assistantToolbox{s: &mcpSession{a: a, inner: inner, outer: outer}}
	}
}

// Tools lists the tools of the person's role, as tools/list does.
func (b *assistantToolbox) Tools(ctx context.Context) []assistant.ToolSpec {
	a := b.s.a
	role := a.effectiveRole(b.s.outer.Context())
	var out []assistant.ToolSpec
	for _, tool := range a.mcpTools() {
		if roleRank(role) < roleRank(tool.Role) {
			continue
		}
		out = append(out, assistant.ToolSpec{
			Name: tool.Name, Title: tool.Title, Description: tool.Description, Schema: tool.Schema,
			Write: tool.Write, Destructive: tool.Destructive,
		})
	}
	return out
}

// Call runs one tool as tools/call does: role, then schema, then the tool.
// The confirmation code, when there is one, is added to the arguments here:
// the model never had it.
func (b *assistantToolbox) Call(ctx context.Context, name string, args json.RawMessage, confirmation string) assistant.ToolOutcome {
	a := b.s.a
	tool, ok := a.mcpTool(name)
	if !ok {
		return assistant.ToolOutcome{Text: "Outil inconnu : " + name + ".", IsError: true}
	}
	if len(args) == 0 || string(args) == "null" {
		args = json.RawMessage("{}")
	}
	var parsed any
	if err := json.Unmarshal(args, &parsed); err != nil {
		return assistant.ToolOutcome{Text: "Arguments illisibles.", IsError: true}
	}
	if confirmation != "" {
		obj, _ := parsed.(map[string]any)
		if obj == nil {
			return assistant.ToolOutcome{Text: "Arguments illisibles.", IsError: true}
		}
		obj["confirmation"] = confirmation
		raw, err := json.Marshal(obj)
		if err != nil {
			return assistant.ToolOutcome{Text: "Arguments illisibles.", IsError: true}
		}
		args, parsed = raw, obj
	}
	if err := checkSchema(tool.Schema, parsed, ""); err != nil {
		return assistant.ToolOutcome{Text: err.Error(), IsError: true}
	}
	text, isError, pending := b.s.runTool(tool, args)
	if pending != nil {
		return assistant.ToolOutcome{Text: pending.recap, Pending: true, Code: pending.code}
	}
	return assistant.ToolOutcome{Text: text, IsError: isError}
}
