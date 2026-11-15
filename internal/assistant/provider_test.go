package assistant

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// The ChatGPT plan token only opens /v1/responses: store false, a stream,
// the system prompt in instructions, the reasoning level passed through.
func TestOpenAIUsesResponsesStream(t *testing.T) {
	var got map[string]any
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/responses" || r.Header.Get("authorization") != "Bearer tok" {
			w.WriteHeader(404)
			return
		}
		json.NewDecoder(r.Body).Decode(&got)
		w.Header().Set("content-type", "text/event-stream")
		fmt.Fprint(w, "event: response.created\ndata: {\"type\":\"response.created\"}\n\n")
		fmt.Fprint(w, "event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"bon\"}\n\n")
		fmt.Fprint(w, `event: response.completed
data: {"type":"response.completed","response":{"output":[{"type":"reasoning","content":[]},{"type":"message","content":[{"type":"output_text","text":"bonjour"}]}],"usage":{"input_tokens":12,"output_tokens":3}}}

`)
	}))
	defer up.Close()
	resp, err := OpenAI{up.URL}.Complete(context.Background(), "gpt-6.1-sol", "sys", []Message{{"user", "salut"}}, "tok", "xhigh")
	if err != nil {
		t.Fatal(err)
	}
	if resp.Text != "bonjour" || resp.Usage.InputTokens != 12 || resp.Usage.OutputTokens != 3 {
		t.Fatalf("%+v", resp)
	}
	if got["store"] != false || got["stream"] != true || got["instructions"] != "sys" {
		t.Fatalf("body %v", got)
	}
	if r, _ := got["reasoning"].(map[string]any); r["effort"] != "xhigh" {
		t.Fatalf("reasoning %v", got["reasoning"])
	}
	for _, item := range got["input"].([]any) {
		if item.(map[string]any)["role"] == "system" {
			t.Fatal("system item in input")
		}
	}
}

// A plan's usage limit arrives after the stream started, as response.failed.
func TestOpenAIStreamFailureIsAnError(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `data: {"type":"response.failed","response":{"error":{"code":"subscription_sharing_usage_limit_exceeded","message":"limit"}}}

`)
	}))
	defer up.Close()
	_, err := OpenAI{up.URL}.Complete(context.Background(), "m", "", nil, "tok", "")
	if err == nil || !strings.Contains(err.Error(), "usage_limit_exceeded") {
		t.Fatalf("%v", err)
	}
}

// The same limit may come as an error event with its details in an object:
// its code reaches the person, not an empty "provider error".
func TestOpenAIStreamErrorEventKeepsItsDetails(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `data: {"type":"error","error":{"type":"invalid_request_error","code":"subscription_sharing_usage_limit_exceeded","message":"limit reached"}}

`)
	}))
	defer up.Close()
	_, err := OpenAI{up.URL}.Complete(context.Background(), "m", "", nil, "tok", "")
	if err == nil || !strings.Contains(err.Error(), "usage_limit_exceeded: limit reached") {
		t.Fatalf("%v", err)
	}
}

// With the plan token, /v1/models answers "models": ranked, some hidden,
// each with its own levels, which reach the pane as they are.
func TestOpenAIFetchModelsFromPlan(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"models":[
			{"slug":"b","display_name":"B","visibility":"list","priority":2,"default_reasoning_level":"medium","supported_reasoning_levels":[{"effort":"low"},{"effort":"medium"}]},
			{"slug":"hidden","visibility":"hide","priority":0},
			{"slug":"a","display_name":"A","visibility":"list","priority":1,"default_reasoning_level":"low","supported_reasoning_levels":[{"effort":"low"},{"effort":"xhigh"},{"effort":"ultra"}]}]}`)
	}))
	defer up.Close()
	models, err := OpenAI{up.URL}.FetchModels(context.Background(), "tok")
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 2 || models[0].Model != "a" || models[0].Name != "A" || models[1].Model != "b" {
		t.Fatalf("%+v", models)
	}
	if fmt.Sprint(models[0].SupportedReasons) != "[low xhigh ultra]" || models[0].DefaultReason != "low" {
		t.Fatalf("%+v", models[0])
	}
}

// With an API key, the usual "data": chat models only, standard levels.
func TestOpenAIFetchModelsFromAPIKey(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"data":[{"id":"whisper-1"},{"id":"gpt-5"},{"id":"o4-mini"},{"id":"text-embedding-3"}]}`)
	}))
	defer up.Close()
	models, err := OpenAI{up.URL}.FetchModels(context.Background(), "sk")
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 2 || models[0].Model != "o4-mini" || models[1].Model != "gpt-5" {
		t.Fatalf("%+v", models)
	}
}

// Anthropic counts thinking inside max_tokens: it must stay above the budget.
func TestClaudeThinkingFitsInMaxTokens(t *testing.T) {
	var got map[string]any
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&got)
		fmt.Fprint(w, `{"content":[{"text":"ok"}],"usage":{"input_tokens":1,"output_tokens":1}}`)
	}))
	defer up.Close()
	for _, effort := range []ReasoningEffort{ReasoningLow, ReasoningMedium, ReasoningHigh, ReasoningMax} {
		if _, err := (Claude{up.URL}).Complete(context.Background(), "m", "", nil, "k", effort); err != nil {
			t.Fatal(err)
		}
		thinking, _ := got["thinking"].(map[string]any)
		if effort == ReasoningLow {
			if thinking != nil {
				t.Fatal("low must not think")
			}
			continue
		}
		if budget := thinking["budget_tokens"].(float64); got["max_tokens"].(float64) <= budget {
			t.Fatalf("%s: max_tokens %v <= budget %v", effort, got["max_tokens"], budget)
		}
	}
}

// The status lists what the provider's API offers, once per TTL, and a new
// key asks again.
func TestStatusListsFetchedModelsAndCaches(t *testing.T) {
	var calls atomic.Int32
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		fmt.Fprint(w, `{"models":[{"slug":"gpt-x","display_name":"GPT X","visibility":"list","supported_reasoning_levels":[{"effort":"max"}],"default_reasoning_level":"max"}]}`)
	}))
	defer up.Close()
	s := newSvc(t, "", up.URL)
	s.Keys.Set("codex", "k")
	mux := http.NewServeMux()
	s.Register(mux, nil)
	status := func() string {
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, httptest.NewRequest("GET", "/api/assistant/status", nil))
		return rr.Body.String()
	}
	if b := status(); !strings.Contains(b, `"model":"gpt-x"`) || !strings.Contains(b, `"name":"GPT X"`) {
		t.Fatal(b)
	}
	status()
	if calls.Load() != 1 {
		t.Fatalf("%d calls, want 1 (cached)", calls.Load())
	}
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest("PUT", "/api/assistant/keys/codex", strings.NewReader(`{"key":"k2"}`)))
	status()
	if calls.Load() != 2 {
		t.Fatalf("%d calls, want 2 after a new key", calls.Load())
	}
}

type promptProvider struct{ system string }

func (p *promptProvider) Name() string { return "fake" }
func (p *promptProvider) Complete(_ context.Context, _, system string, _ []Message, _ string, _ ReasoningEffort) (Response, error) {
	p.system = system
	return Response{Text: "ok"}, nil
}
func (p *promptProvider) SupportsSubscriptionAuth() bool { return false }
func (p *promptProvider) ListModels() []ModelCapability  { return nil }

// The model is told where it runs: the profile, the page open, the role,
// the language and the infrastructure as it is now.
func TestChatGivesTheModelItsContext(t *testing.T) {
	s := newSvc(t, "", "")
	fake := &promptProvider{}
	s.Providers["fake"] = fake
	s.Keys.Set("fake", "k")
	s.Snapshot = func(context.Context) string { return "Servers (1):\n- web-1: ready" }
	mux := http.NewServeMux()
	s.Register(mux, AsRole("deploy"))
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest("POST", "/api/assistant/chat", strings.NewReader(
		`{"session":"s","provider":"fake","model":"m","path":"/applications/42","lang":"fr","messages":[{"role":"user","content":"x"}]}`)))
	if rr.Code != 200 {
		t.Fatal(rr.Code, rr.Body.String())
	}
	for _, want := range []string{"sys", "/applications/42", "deploy", "French", "web-1: ready"} {
		if !strings.Contains(fake.system, want) {
			t.Fatalf("system prompt lacks %q:\n%s", want, fake.system)
		}
	}
}
