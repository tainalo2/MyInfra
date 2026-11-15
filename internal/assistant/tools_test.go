package assistant

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	harness "myinfra/internal/harness-router"
)

// scripted is a fake provider: it answers each call with the next turn and
// keeps what it was sent.
type scripted struct {
	mu    sync.Mutex
	turns []ToolTurn
	got   []ToolRequest
}

func (p *scripted) Name() string                   { return "fake" }
func (p *scripted) SupportsSubscriptionAuth() bool { return false }
func (p *scripted) ListModels() []ModelCapability  { return nil }
func (p *scripted) Complete(context.Context, string, string, []Message, string, ReasoningEffort) (Response, error) {
	return Response{Text: "without tools"}, nil
}

func (p *scripted) CompleteWithTools(_ context.Context, q ToolRequest) (ToolTurn, Usage, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.got = append(p.got, q)
	if len(p.turns) == 0 {
		return ToolTurn{Text: "done"}, Usage{1, 1}, nil
	}
	t := p.turns[0]
	p.turns = p.turns[1:]
	return t, Usage{10, 5}, nil
}

// fakeBox stands for the MCP tools of the API.
type fakeBox struct {
	mu    sync.Mutex
	calls []string
}

func (b *fakeBox) Tools(context.Context) []ToolSpec {
	return []ToolSpec{
		{Name: "list_applications", Title: "Lister les applications"},
		{Name: "restart_application", Title: "Redémarrer une application", Write: true},
		{Name: "delete_package", Title: "Supprimer un paquet", Write: true, Destructive: true},
		{Name: "create_ssh_key", Title: "Créer une clé SSH", Write: true},
	}
}

func (b *fakeBox) Call(_ context.Context, name string, args json.RawMessage, confirmation string) ToolOutcome {
	b.mu.Lock()
	b.calls = append(b.calls, name+" "+string(args)+" "+confirmation)
	b.mu.Unlock()
	switch name {
	case "list_applications":
		return ToolOutcome{Text: `[{"name":"blog","server":"srv-1"}]`}
	case "delete_package":
		if confirmation == "" {
			return ToolOutcome{Text: "Le paquet wiki sera supprimé.", Pending: true, Code: "ABC-DEF"}
		}
		return ToolOutcome{Text: "Paquet supprimé."}
	}
	return ToolOutcome{Text: "Fait."}
}

func (b *fakeBox) called() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]string(nil), b.calls...)
}

const toolsConfig = `contexts:
  general:
    system_prompt: sys
    allowed_tools: [list_applications, restart_application, delete_package]
    max_turns: 4
`

func toolSvc(t *testing.T, prov Provider, box Toolbox) (*Service, http.Handler) {
	ks, err := NewKeyStore(bytes.Repeat([]byte{7}, 32))
	if err != nil {
		t.Fatal(err)
	}
	ks.Set("fake", "k")
	cfg, _ := harness.ParseConfig([]byte(toolsConfig))
	p := DefaultPricing()
	s := &Service{Keys: ks, Pricing: p, Meter: NewMeter(p), Router: harness.NewRouter(cfg),
		Providers: map[string]Provider{"fake": prov}, Toolbox: func(*http.Request) Toolbox { return box }}
	mux := http.NewServeMux()
	s.Register(mux, func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			ctx := WithUser(WithRole(r.Context(), harness.RoleDeploy), r.Header.Get("x-user"))
			next(w, r.WithContext(ctx))
		}
	})
	return s, mux
}

func call(t *testing.T, h http.Handler, user, path, body string, out any) int {
	t.Helper()
	req := httptest.NewRequest("POST", path, strings.NewReader(body))
	req.Header.Set("x-user", user)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if out != nil {
		if err := json.Unmarshal(rr.Body.Bytes(), out); err != nil {
			t.Fatalf("%d %s", rr.Code, rr.Body.String())
		}
	}
	return rr.Code
}

const ask = `{"session":"s","provider":"fake","model":"m","messages":[{"role":"user","content":"Quelles applications tournent sur srv-1 ?"}]}`

type chatOut struct {
	Text    string     `json:"text"`
	Tools   []usedTool `json:"tools"`
	Actions []Action   `json:"actions"`
	Usage   Snapshot   `json:"usage"`
}

// A question about the infrastructure makes the model read with a tool,
// and its answer is built on the result, which went back to it.
func TestChatReadsWithATool(t *testing.T) {
	prov := &scripted{turns: []ToolTurn{
		{Calls: []ToolCall{{ID: "c1", Name: "list_applications", Args: json.RawMessage(`{}`)}}},
		{Text: "Sur srv-1 tourne blog."},
	}}
	box := &fakeBox{}
	_, h := toolSvc(t, prov, box)
	var out chatOut
	if code := call(t, h, "u1", "/api/assistant/chat", ask, &out); code != 200 {
		t.Fatal(code)
	}
	if out.Text != "Sur srv-1 tourne blog." || len(out.Tools) != 1 || out.Tools[0].Name != "list_applications" {
		t.Fatalf("%+v", out)
	}
	if len(prov.got) != 2 {
		t.Fatalf("%d calls", len(prov.got))
	}
	// The tools offered are the role's, kept to the profile's allowlist.
	names := []string{}
	for _, tool := range prov.got[0].Tools {
		names = append(names, tool.Name)
	}
	if strings.Join(names, ",") != "list_applications,restart_application,delete_package" {
		t.Fatalf("offered %v", names)
	}
	steps := prov.got[1].Steps
	if len(steps) != 1 || steps[0].Results[0].CallID != "c1" || !strings.Contains(steps[0].Results[0].Text, "blog") {
		t.Fatalf("steps %+v", steps)
	}
	if out.Usage.InputTokens != 20 || out.Usage.OutputTokens != 10 {
		t.Fatalf("usage %+v", out.Usage)
	}
}

// A tool outside the profile's allowlist is refused at call time, even when
// the role has it: the guard holds whatever the model asks.
func TestChatRefusesToolOutsideAllowlist(t *testing.T) {
	prov := &scripted{turns: []ToolTurn{
		{Calls: []ToolCall{{ID: "c1", Name: "create_ssh_key", Args: json.RawMessage(`{"name":"x"}`)}}},
	}}
	box := &fakeBox{}
	_, h := toolSvc(t, prov, box)
	var out chatOut
	call(t, h, "u1", "/api/assistant/chat", ask, &out)
	if len(box.called()) != 0 || len(out.Actions) != 0 {
		t.Fatalf("ran %v, actions %v", box.called(), out.Actions)
	}
	if r := prov.got[1].Steps[0].Results[0]; !r.IsError {
		t.Fatalf("%+v", r)
	}
}

// The assistant acts with the person's rights: a plain change runs at once,
// as a click of theirs would, and the reply says it was done.
func TestPlainChangeRunsAtOnce(t *testing.T) {
	prov := &scripted{turns: []ToolTurn{
		{Calls: []ToolCall{{ID: "c1", Name: "restart_application", Args: json.RawMessage(`{"name":"blog"}`)}}},
		{Text: "blog a redémarré."},
	}}
	box := &fakeBox{}
	_, h := toolSvc(t, prov, box)
	var out chatOut
	call(t, h, "u1", "/api/assistant/chat", ask, &out)
	if got := box.called(); len(got) != 1 || got[0] != `restart_application {"name":"blog"} ` {
		t.Fatalf("%v", got)
	}
	if len(out.Actions) != 0 || len(out.Tools) != 1 || !out.Tools[0].Write || out.Text != "blog a redémarré." {
		t.Fatalf("%+v", out)
	}
}

// A destruction waits as an action, which only the person who was asked can
// confirm, once.
func TestDestructionConfirmedOnceByThePerson(t *testing.T) {
	prov := &scripted{turns: []ToolTurn{
		{Calls: []ToolCall{{ID: "c1", Name: "delete_package", Args: json.RawMessage(`{"name":"wiki"}`)}}},
		{Text: "Confirmez dans l'interface."},
	}}
	box := &fakeBox{}
	_, h := toolSvc(t, prov, box)
	var out chatOut
	call(t, h, "u1", "/api/assistant/chat", ask, &out)
	if len(out.Actions) != 1 {
		t.Fatalf("%+v", out)
	}
	id := out.Actions[0].ID
	if code := call(t, h, "u2", "/api/assistant/actions/"+id, `{"confirm":true}`, nil); code != 404 {
		t.Fatalf("another person took it: %d", code)
	}
	var done struct {
		Done bool `json:"done"`
	}
	if code := call(t, h, "u1", "/api/assistant/actions/"+id, `{"confirm":true}`, &done); code != 200 || !done.Done {
		t.Fatalf("%d %+v", code, done)
	}
	if code := call(t, h, "u1", "/api/assistant/actions/"+id, `{"confirm":true}`, nil); code != 404 {
		t.Fatalf("confirmed twice: %d", code)
	}
	// The recap call, then the one confirmed: never one for u2, never a third.
	if got := box.called(); len(got) != 2 {
		t.Fatalf("%v", got)
	}
}

func TestCancelledDestructionNeverRuns(t *testing.T) {
	prov := &scripted{turns: []ToolTurn{
		{Calls: []ToolCall{{ID: "c1", Name: "delete_package", Args: json.RawMessage(`{"name":"wiki"}`)}}},
	}}
	box := &fakeBox{}
	_, h := toolSvc(t, prov, box)
	var out chatOut
	call(t, h, "u1", "/api/assistant/chat", ask, &out)
	call(t, h, "u1", "/api/assistant/actions/"+out.Actions[0].ID, `{"confirm":false}`, nil)
	// Only the recap call: the deletion itself never ran.
	if got := box.called(); len(got) != 1 || strings.HasSuffix(got[0], "ABC-DEF") {
		t.Fatalf("%v", got)
	}
}

// A purchase or a destruction keeps the MCP's two calls: the tool's recap
// is shown, and its code, which the model never sees, goes with the second
// call once the person confirms.
func TestDestructiveKeepsTheCodeFromTheModel(t *testing.T) {
	prov := &scripted{turns: []ToolTurn{
		{Calls: []ToolCall{{ID: "c1", Name: "delete_package", Args: json.RawMessage(`{"name":"wiki"}`)}}},
		{Text: "Confirmez dans l'interface."},
	}}
	box := &fakeBox{}
	_, h := toolSvc(t, prov, box)
	var out chatOut
	call(t, h, "u1", "/api/assistant/chat", ask, &out)
	if len(out.Actions) != 1 || out.Actions[0].Recap != "Le paquet wiki sera supprimé." {
		t.Fatalf("%+v", out)
	}
	for _, q := range prov.got {
		for _, st := range q.Steps {
			for _, r := range st.Results {
				if strings.Contains(r.Text, "ABC-DEF") {
					t.Fatal("the model saw the confirmation code")
				}
			}
		}
	}
	call(t, h, "u1", "/api/assistant/actions/"+out.Actions[0].ID, `{"confirm":true}`, nil)
	got := box.called()
	if len(got) != 2 || got[1] != `delete_package {"name":"wiki"} ABC-DEF` {
		t.Fatalf("%v", got)
	}
}

// The loop stops at the profile's max_turns.
func TestToolLoopStopsAtMaxTurns(t *testing.T) {
	loop := ToolTurn{Calls: []ToolCall{{ID: "c", Name: "list_applications", Args: json.RawMessage(`{}`)}}}
	prov := &scripted{turns: []ToolTurn{loop, loop, loop, loop, loop, loop}}
	_, h := toolSvc(t, prov, &fakeBox{})
	var out chatOut
	call(t, h, "u1", "/api/assistant/chat", ask, &out)
	if len(prov.got) != 4 || out.Text == "" {
		t.Fatalf("%d calls, %q", len(prov.got), out.Text)
	}
}

// Claude: tools in the Messages API, tool_use read back, and on the next
// call the assistant's content as it was, then a tool_result.
func TestClaudeToolUseRoundTrip(t *testing.T) {
	var bodies []map[string]any
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var b map[string]any
		json.NewDecoder(r.Body).Decode(&b)
		bodies = append(bodies, b)
		fmt.Fprint(w, `{"content":[{"type":"thinking","thinking":"…","signature":"sig"},{"type":"text","text":"Je regarde."},{"type":"tool_use","id":"tu1","name":"list_applications","input":{"server":"srv-1"}}],"usage":{"input_tokens":7,"output_tokens":3}}`)
	}))
	defer up.Close()
	c := Claude{up.URL}
	q := ToolRequest{Model: "m", System: "sys", Messages: []Message{{"user", "q"}}, Key: "k", Effort: ReasoningMedium,
		Tools: []ToolSpec{{Name: "list_applications", Title: "Lister", Description: "Liste.", Schema: map[string]any{"type": "object"}}}}
	turn, usage, err := c.CompleteWithTools(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	if turn.Text != "Je regarde." || len(turn.Calls) != 1 || turn.Calls[0].ID != "tu1" || string(turn.Calls[0].Args) != `{"server":"srv-1"}` || usage.InputTokens != 7 {
		t.Fatalf("%+v %+v", turn, usage)
	}
	tool := bodies[0]["tools"].([]any)[0].(map[string]any)
	if tool["name"] != "list_applications" || tool["input_schema"] == nil {
		t.Fatalf("%v", tool)
	}
	q.Steps = []ToolStep{{Turn: turn, Results: []ToolResult{{CallID: "tu1", Text: "blog"}}}}
	if _, _, err := c.CompleteWithTools(context.Background(), q); err != nil {
		t.Fatal(err)
	}
	msgs := bodies[1]["messages"].([]any)
	if len(msgs) != 3 {
		t.Fatalf("%v", msgs)
	}
	back := msgs[1].(map[string]any)["content"].([]any)
	if back[0].(map[string]any)["signature"] != "sig" {
		t.Fatalf("thinking not sent back: %v", back)
	}
	res := msgs[2].(map[string]any)["content"].([]any)[0].(map[string]any)
	if msgs[2].(map[string]any)["role"] != "user" || res["type"] != "tool_result" || res["tool_use_id"] != "tu1" || res["content"] != "blog" {
		t.Fatalf("%v", msgs[2])
	}
}

// OpenAI: function tools only (the plan token refuses hosted ones), store
// false, and every step sent back as function_call and its output.
func TestOpenAIFunctionCallRoundTrip(t *testing.T) {
	var bodies []map[string]any
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var b map[string]any
		json.Unmarshal(raw, &b)
		bodies = append(bodies, b)
		w.Header().Set("content-type", "text/event-stream")
		fmt.Fprint(w, `data: {"type":"response.completed","response":{"output":[{"type":"reasoning"},{"type":"function_call","call_id":"call_1","name":"list_applications","arguments":"{\"server\":\"srv-1\"}"}],"usage":{"input_tokens":9,"output_tokens":2}}}

`)
	}))
	defer up.Close()
	o := OpenAI{up.URL}
	q := ToolRequest{Model: "m", System: "sys", Messages: []Message{{"user", "q"}}, Key: "tok",
		Tools: []ToolSpec{{Name: "list_applications", Title: "Lister", Schema: map[string]any{"type": "object"}}}}
	turn, _, err := o.CompleteWithTools(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	if len(turn.Calls) != 1 || turn.Calls[0].ID != "call_1" || string(turn.Calls[0].Args) != `{"server":"srv-1"}` {
		t.Fatalf("%+v", turn)
	}
	b := bodies[0]
	if b["store"] != false || b["stream"] != true || b["instructions"] != "sys" {
		t.Fatalf("%v", b)
	}
	for _, tool := range b["tools"].([]any) {
		if tool.(map[string]any)["type"] != "function" {
			t.Fatalf("hosted tool offered: %v", tool)
		}
	}
	q.Steps = []ToolStep{{Turn: turn, Results: []ToolResult{{CallID: "call_1", Text: "blog"}}}}
	if _, _, err := o.CompleteWithTools(context.Background(), q); err != nil {
		t.Fatal(err)
	}
	input := bodies[1]["input"].([]any)
	if len(input) != 3 {
		t.Fatalf("%v", input)
	}
	fc, out := input[1].(map[string]any), input[2].(map[string]any)
	if fc["type"] != "function_call" || fc["call_id"] != "call_1" || fc["arguments"] != `{"server":"srv-1"}` {
		t.Fatalf("%v", fc)
	}
	if out["type"] != "function_call_output" || out["call_id"] != "call_1" || out["output"] != "blog" {
		t.Fatalf("%v", out)
	}
}
