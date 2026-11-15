package assistant

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	harness "myinfra/internal/harness-router"
)

// ToolSpec is one tool offered to the model: the MCP tools of MyInfra,
// as the caller's role may use them.
type ToolSpec struct {
	Name        string
	Title       string
	Description string
	Schema      map[string]any
	// Write marks a tool that changes something: the assistant never runs
	// it before the person agrees in the interface.
	Write bool
	// Destructive marks a purchase or a destruction: the tool itself gives
	// the recap and a confirmation code, which stays on the server.
	Destructive bool
}

// ToolOutcome is what one call gave.
type ToolOutcome struct {
	Text    string
	IsError bool
	// Pending is a recap waiting for the person's agreement: nothing was
	// done, and Code is what the second call must carry.
	Pending bool
	Code    string
}

// Toolbox runs the tools for one request, as the signed-in person.
type Toolbox interface {
	// Tools lists the tools the caller's role may use.
	Tools(ctx context.Context) []ToolSpec
	// Call runs one tool; confirmation is the code of a recap, or empty.
	Call(ctx context.Context, name string, args json.RawMessage, confirmation string) ToolOutcome
}

// ToolCall is a tool the model asks for.
type ToolCall struct {
	ID   string          `json:"id"`
	Name string          `json:"name"`
	Args json.RawMessage `json:"args"`
}

// ToolResult answers one ToolCall.
type ToolResult struct {
	CallID  string
	Name    string
	Text    string
	IsError bool
}

// ToolTurn is one answer of the model: its text, the tools it calls, and
// the answer as the provider gave it, sent back as is on the next call
// (Claude asks its thinking blocks back, signatures included).
type ToolTurn struct {
	Text  string
	Calls []ToolCall
	Raw   json.RawMessage
}

// ToolStep is a turn of the model followed by the results of its calls.
type ToolStep struct {
	Turn    ToolTurn
	Results []ToolResult
}

// ToolRequest is one call to a provider that may use tools.
type ToolRequest struct {
	Model    string
	System   string
	Messages []Message
	Steps    []ToolStep
	Tools    []ToolSpec
	Key      string
	Effort   ReasoningEffort
}

// ToolUser is a provider that can call tools. One that cannot answers
// with Complete, without tools.
type ToolUser interface {
	CompleteWithTools(ctx context.Context, req ToolRequest) (ToolTurn, Usage, error)
}

// toolResultMax bounds what one result gives the model: a long history or
// a log is cut, the model can ask again more narrowly.
const toolResultMax = 30000

// pendingLife matches the life of an MCP confirmation code.
const pendingLife = 10 * time.Minute

// Action is a change the model asked for, waiting for the person: the
// interface shows the recap with a button to confirm and one to cancel.
type Action struct {
	ID    string `json:"id"`
	Tool  string `json:"tool"`
	Title string `json:"title"`
	Recap string `json:"recap"`
}

type pendingAction struct {
	Action
	user    string
	args    json.RawMessage
	code    string
	expires time.Time
}

// actions keeps the changes waiting for agreement. The confirmation code
// lives here only: the model never sees it, so it cannot agree for the
// person.
type actions struct {
	mu   sync.Mutex
	byID map[string]pendingAction
}

func (p *actions) put(a pendingAction) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.byID == nil {
		p.byID = map[string]pendingAction{}
	}
	now := time.Now()
	for id, old := range p.byID {
		if now.After(old.expires) {
			delete(p.byID, id)
		}
	}
	p.byID[a.ID] = a
}

// take hands the action over once, to the person who was asked.
func (p *actions) take(id, user string) (pendingAction, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	a, ok := p.byID[id]
	if !ok || a.user != user {
		return pendingAction{}, false
	}
	delete(p.byID, id)
	if time.Now().After(a.expires) {
		return pendingAction{}, false
	}
	return a, true
}

func newActionID() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// toolsFor is what the model may use here: the tools of the person's role,
// kept to the profile's allowlist.
func toolsFor(ctx context.Context, box Toolbox, guard *harness.ToolGuard) []ToolSpec {
	if box == nil {
		return nil
	}
	var out []ToolSpec
	for _, t := range box.Tools(ctx) {
		if guard.Check(t.Name) == nil {
			out = append(out, t)
		}
	}
	return out
}

// usedTool tells the interface which tools a reply rests on.
type usedTool struct {
	Name    string `json:"name"`
	Title   string `json:"title"`
	IsError bool   `json:"is_error,omitempty"`
}

type loopResult struct {
	Text    string
	Usage   Usage
	Used    []usedTool
	Actions []Action
}

const waitingText = "Nothing was done yet. The interface now shows the user this recap with a button to confirm and one to cancel; the action runs only if they confirm there. Tell them in one or two sentences what it will do, and do not ask them to type a code or to answer yes."

// converse runs the turn: the model may call tools, reads their results
// and goes on, up to maxTurns calls. A change is never run here: it waits
// for the person as an Action.
func (s *Service) converse(ctx context.Context, prov ToolUser, req ToolRequest, box Toolbox, guard *harness.ToolGuard, maxTurns int) (loopResult, error) {
	if maxTurns <= 0 {
		maxTurns = 10
	}
	byName := map[string]ToolSpec{}
	for _, t := range req.Tools {
		byName[t.Name] = t
	}
	var res loopResult
	user := UserOf(ctx)
	for i := 0; i < maxTurns; i++ {
		turn, usage, err := prov.CompleteWithTools(ctx, req)
		res.Usage.InputTokens += usage.InputTokens
		res.Usage.OutputTokens += usage.OutputTokens
		if err != nil {
			return res, err
		}
		if len(turn.Calls) == 0 {
			res.Text = turn.Text
			return res, nil
		}
		step := ToolStep{Turn: turn}
		for _, call := range turn.Calls {
			out := s.runCall(ctx, call, byName, box, guard, user, &res)
			step.Results = append(step.Results, out)
		}
		req.Steps = append(req.Steps, step)
		res.Text = turn.Text
	}
	if strings.TrimSpace(res.Text) == "" {
		res.Text = fmt.Sprintf("(stopped after %d steps without a final answer)", maxTurns)
	}
	return res, nil
}

func (s *Service) runCall(ctx context.Context, call ToolCall, byName map[string]ToolSpec, box Toolbox, guard *harness.ToolGuard, user string, res *loopResult) ToolResult {
	result := ToolResult{CallID: call.ID, Name: call.Name}
	spec, known := byName[call.Name]
	// The allowlist holds at call time too: a name the model makes up, or
	// one of another profile, is refused before anything runs.
	if err := guard.Check(call.Name); err != nil || !known {
		result.Text, result.IsError = "Tool not available here: "+call.Name, true
		res.Used = append(res.Used, usedTool{Name: call.Name, Title: call.Name, IsError: true})
		return result
	}
	args := call.Args
	if len(args) == 0 || string(args) == "null" {
		args = json.RawMessage("{}")
	}
	title := spec.Title
	if title == "" {
		title = spec.Name
	}
	if spec.Write {
		recap := describeCall(spec, args)
		code := ""
		if spec.Destructive {
			// The tool gives its own recap (prices, what goes) and a code.
			out := box.Call(ctx, spec.Name, args, "")
			if !out.Pending {
				result.Text, result.IsError = clip(out.Text), true
				if !out.IsError {
					result.Text = "Refused: this action needs the user's confirmation and the tool gave none."
				}
				res.Used = append(res.Used, usedTool{Name: spec.Name, Title: title, IsError: true})
				return result
			}
			recap, code = out.Text, out.Code
		}
		a := pendingAction{
			Action: Action{ID: newActionID(), Tool: spec.Name, Title: title, Recap: recap},
			user:   user, args: args, code: code, expires: time.Now().Add(pendingLife),
		}
		s.pending.put(a)
		res.Actions = append(res.Actions, a.Action)
		result.Text = "Recap:\n" + recap + "\n\n" + waitingText
		return result
	}
	out := box.Call(ctx, spec.Name, args, "")
	result.Text, result.IsError = clip(out.Text), out.IsError
	res.Used = append(res.Used, usedTool{Name: spec.Name, Title: title, IsError: out.IsError})
	return result
}

// describeCall is the recap of a change the tool does not recap itself:
// its title and the arguments, as the person reads them.
func describeCall(spec ToolSpec, args json.RawMessage) string {
	var m map[string]any
	_ = json.Unmarshal(args, &m)
	var b strings.Builder
	b.WriteString(spec.Title)
	if len(m) > 0 {
		pretty, _ := json.MarshalIndent(m, "", "  ")
		b.WriteString("\n")
		b.Write(pretty)
	}
	return b.String()
}

func clip(s string) string {
	if len(s) <= toolResultMax {
		return s
	}
	return s[:toolResultMax] + "\n… (cut)"
}

// decideAction answers POST /api/assistant/actions/{id}: confirm runs the
// change, as the person, with the code the recap gave; cancel drops it.
func (s *Service) decideAction(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Confirm bool `json:"confirm"`
	}
	if json.NewDecoder(r.Body).Decode(&b) != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid request"})
		return
	}
	if !CanChat(RoleOf(r.Context())) {
		writeJSON(w, 403, map[string]string{"error": "Droits insuffisants."})
		return
	}
	a, ok := s.pending.take(r.PathValue("id"), UserOf(r.Context()))
	if !ok {
		writeJSON(w, 404, map[string]string{"error": "action_expired"})
		return
	}
	if !b.Confirm {
		writeJSON(w, 200, map[string]any{"done": false})
		return
	}
	var box Toolbox
	if s.Toolbox != nil {
		box = s.Toolbox(r)
	}
	if box == nil {
		writeJSON(w, 503, map[string]string{"error": "tools unavailable"})
		return
	}
	out := box.Call(r.Context(), a.Tool, a.args, a.code)
	if out.Pending {
		// What the recap showed changed meanwhile: a new recap to agree to.
		next := pendingAction{
			Action: Action{ID: newActionID(), Tool: a.Tool, Title: a.Title, Recap: out.Text},
			user:   a.user, args: a.args, code: out.Code, expires: time.Now().Add(pendingLife),
		}
		s.pending.put(next)
		writeJSON(w, 200, map[string]any{"done": false, "action": next.Action})
		return
	}
	writeJSON(w, 200, map[string]any{"done": !out.IsError, "text": out.Text, "is_error": out.IsError})
}
