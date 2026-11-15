package assistant

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
)

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type Usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

type Response struct {
	Text  string
	Usage Usage
}

// ReasoningEffort represents the level of reasoning effort for a model.
type ReasoningEffort string

const (
	ReasoningLow    ReasoningEffort = "low"
	ReasoningMedium ReasoningEffort = "medium"
	ReasoningHigh   ReasoningEffort = "high"
	ReasoningMax    ReasoningEffort = "max"
)

// ModelCapability describes a model and its supported reasoning levels.
// A provider that lists its models says which levels each one takes
// (OpenAI also has xhigh and ultra); they are passed through as they are.
type ModelCapability struct {
	Model            string            `json:"model"`
	Name             string            `json:"name,omitempty"`
	SupportedReasons []ReasoningEffort `json:"supported_reasons"`
	DefaultReason    ReasoningEffort   `json:"default_reason"`
}

// Provider is the provider-agnostic interface used by the harness.
type Provider interface {
	Name() string
	Complete(ctx context.Context, model, system string, msgs []Message, apiKey string, reasoningEffort ReasoningEffort) (Response, error)
	SupportsSubscriptionAuth() bool
	ListModels() []ModelCapability
}

// ModelLister is a provider that asks its API which models the credential
// may use. ListModels stays the fallback when that call fails.
type ModelLister interface {
	FetchModels(ctx context.Context, key string) ([]ModelCapability, error)
}

var ErrNoKey = errors.New("no_key")

func scrub(err error, key string) error {
	if err == nil || key == "" {
		return err
	}
	return errors.New(strings.ReplaceAll(err.Error(), key, "[redacted]"))
}

func getJSON(ctx context.Context, url string, hdr map[string]string, out any, key string) error {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return scrub(err, key)
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return scrub(err, key)
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(res.Body, 8<<20))
	if res.StatusCode >= 300 {
		return scrub(fmt.Errorf("provider status %d: %s", res.StatusCode, data), key)
	}
	return json.Unmarshal(data, out)
}

func doJSON(ctx context.Context, url string, hdr map[string]string, body, out any, key string) error {
	b, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(b))
	if err != nil {
		return scrub(err, key)
	}
	req.Header.Set("content-type", "application/json")
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return scrub(err, key)
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	if res.StatusCode >= 300 {
		return scrub(fmt.Errorf("provider status %d: %s", res.StatusCode, data), key)
	}
	return json.Unmarshal(data, out)
}

// Claude adapter (Anthropic Messages API).
type Claude struct{ BaseURL string }

func (Claude) Name() string { return "claude" }

func (Claude) SupportsSubscriptionAuth() bool { return false }

// claudeReasons are the thinking levels of the adapter: low answers without
// extended thinking, the others give it a budget of tokens.
var claudeReasons = []ReasoningEffort{ReasoningLow, ReasoningMedium, ReasoningHigh, ReasoningMax}

var claudeBudget = map[ReasoningEffort]int{ReasoningMedium: 4000, ReasoningHigh: 16000, ReasoningMax: 32000}

func (Claude) ListModels() []ModelCapability {
	return []ModelCapability{
		{Model: "claude-opus", SupportedReasons: claudeReasons, DefaultReason: ReasoningMedium},
		{Model: "claude-sonnet", SupportedReasons: claudeReasons, DefaultReason: ReasoningMedium},
		{Model: "claude-haiku", SupportedReasons: claudeReasons, DefaultReason: ReasoningLow},
	}
}

func (c Claude) base() string {
	if c.BaseURL == "" {
		return "https://api.anthropic.com"
	}
	return c.BaseURL
}

// FetchModels lists the models the key may use (Anthropic Models API).
func (c Claude) FetchModels(ctx context.Context, key string) ([]ModelCapability, error) {
	var out struct {
		Data []struct {
			ID          string `json:"id"`
			DisplayName string `json:"display_name"`
		} `json:"data"`
	}
	if err := getJSON(ctx, c.base()+"/v1/models?limit=100", map[string]string{"x-api-key": key, "anthropic-version": "2023-06-01"}, &out, key); err != nil {
		return nil, err
	}
	models := make([]ModelCapability, 0, len(out.Data))
	for _, m := range out.Data {
		models = append(models, ModelCapability{Model: m.ID, Name: m.DisplayName, SupportedReasons: claudeReasons, DefaultReason: ReasoningMedium})
	}
	if len(models) == 0 {
		return nil, errors.New("no model listed")
	}
	return models, nil
}

func (c Claude) Complete(ctx context.Context, model, system string, msgs []Message, key string, reasoningEffort ReasoningEffort) (Response, error) {
	if key == "" {
		return Response{}, ErrNoKey
	}
	var out struct {
		Content []struct{ Text string } `json:"content"`
		Usage   struct {
			In  int `json:"input_tokens"`
			Out int `json:"output_tokens"`
		} `json:"usage"`
	}
	// max_tokens counts the thinking too: the answer keeps 8192 on top of it.
	req := map[string]any{"model": model, "max_tokens": 8192, "system": system, "messages": msgs}
	if budget := claudeBudget[reasoningEffort]; budget > 0 {
		req["max_tokens"] = 8192 + budget
		req["thinking"] = map[string]any{"type": "enabled", "budget_tokens": budget}
	}
	err := doJSON(ctx, c.base()+"/v1/messages", map[string]string{"x-api-key": key, "anthropic-version": "2023-06-01"}, req, &out, key)
	if err != nil {
		return Response{}, err
	}
	var sb strings.Builder
	for _, p := range out.Content {
		sb.WriteString(p.Text)
	}
	return Response{sb.String(), Usage{out.Usage.In, out.Usage.Out}}, nil
}

// OpenAI adapter (Responses API), for an API key and for the ChatGPT plan
// token alike. The plan token only opens /v1/responses and /v1/models, and
// /v1/responses asks for store false and a stream.
type OpenAI struct{ BaseURL string }

func (OpenAI) Name() string { return "codex" }

func (OpenAI) SupportsSubscriptionAuth() bool { return true }

var openAIReasons = []ReasoningEffort{ReasoningLow, ReasoningMedium, ReasoningHigh}

func (OpenAI) ListModels() []ModelCapability {
	return []ModelCapability{
		{Model: "gpt-5-codex", SupportedReasons: openAIReasons, DefaultReason: ReasoningMedium},
		{Model: "gpt-5", SupportedReasons: openAIReasons, DefaultReason: ReasoningMedium},
	}
}

func (o OpenAI) base() string {
	if o.BaseURL == "" {
		return "https://api.openai.com"
	}
	return o.BaseURL
}

// FetchModels reads /v1/models. With the ChatGPT plan token it answers
// "models", each with its levels and its rank; with an API key, the usual
// "data", which says nothing of reasoning.
func (o OpenAI) FetchModels(ctx context.Context, key string) ([]ModelCapability, error) {
	var out struct {
		Models []struct {
			Slug        string `json:"slug"`
			DisplayName string `json:"display_name"`
			Visibility  string `json:"visibility"`
			Priority    int    `json:"priority"`
			Default     string `json:"default_reasoning_level"`
			Levels      []struct {
				Effort string `json:"effort"`
			} `json:"supported_reasoning_levels"`
		} `json:"models"`
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := getJSON(ctx, o.base()+"/v1/models", map[string]string{"authorization": "Bearer " + key}, &out, key); err != nil {
		return nil, err
	}
	models := []ModelCapability{}
	if len(out.Models) > 0 {
		sort.SliceStable(out.Models, func(i, j int) bool { return out.Models[i].Priority < out.Models[j].Priority })
		for _, m := range out.Models {
			// Hidden ones are for internal use (reviews, reserve).
			if m.Visibility != "" && m.Visibility != "list" {
				continue
			}
			reasons := make([]ReasoningEffort, 0, len(m.Levels))
			for _, l := range m.Levels {
				reasons = append(reasons, ReasoningEffort(l.Effort))
			}
			def := ReasoningEffort(m.Default)
			if len(reasons) == 0 {
				reasons, def = openAIReasons, ReasoningMedium
			}
			models = append(models, ModelCapability{Model: m.Slug, Name: m.DisplayName, SupportedReasons: reasons, DefaultReason: def})
		}
	} else {
		ids := []string{}
		for _, m := range out.Data {
			if strings.HasPrefix(m.ID, "gpt-") || strings.HasPrefix(m.ID, "codex") || (len(m.ID) > 1 && m.ID[0] == 'o' && m.ID[1] >= '0' && m.ID[1] <= '9') {
				ids = append(ids, m.ID)
			}
		}
		sort.Sort(sort.Reverse(sort.StringSlice(ids)))
		for _, id := range ids {
			models = append(models, ModelCapability{Model: id, SupportedReasons: openAIReasons, DefaultReason: ReasoningMedium})
		}
	}
	if len(models) == 0 {
		return nil, errors.New("no model listed")
	}
	return models, nil
}

func (o OpenAI) Complete(ctx context.Context, model, system string, msgs []Message, key string, reasoningEffort ReasoningEffort) (Response, error) {
	if key == "" {
		return Response{}, ErrNoKey
	}
	// The system prompt goes in instructions: a system item in input is refused.
	input := make([]map[string]string, 0, len(msgs))
	for _, m := range msgs {
		input = append(input, map[string]string{"role": m.Role, "content": m.Content})
	}
	body := map[string]any{"model": model, "instructions": system, "input": input, "store": false, "stream": true}
	if reasoningEffort != "" {
		body["reasoning"] = map[string]any{"effort": reasoningEffort}
	}
	b, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, "POST", o.base()+"/v1/responses", bytes.NewReader(b))
	if err != nil {
		return Response{}, scrub(err, key)
	}
	req.Header.Set("content-type", "application/json")
	req.Header.Set("accept", "text/event-stream")
	req.Header.Set("authorization", "Bearer "+key)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return Response{}, scrub(err, key)
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		data, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
		return Response{}, scrub(fmt.Errorf("provider status %d: %s", res.StatusCode, data), key)
	}
	return readResponseStream(res.Body, key)
}

// readResponseStream reads the events until response.completed, whose
// response carries the whole output and the usage. A failure can come after
// the stream started (a plan's usage limit, for one).
func readResponseStream(r io.Reader, key string) (Response, error) {
	type result struct {
		Output []struct {
			Type    string `json:"type"`
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"output"`
		Usage struct {
			In  int `json:"input_tokens"`
			Out int `json:"output_tokens"`
		} `json:"usage"`
		Error *struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64<<10), 16<<20)
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		var ev struct {
			Type     string `json:"type"`
			Response result `json:"response"`
			Code     string `json:"code"`
			Message  string `json:"message"`
		}
		if json.Unmarshal([]byte(strings.TrimSpace(line[5:])), &ev) != nil {
			continue
		}
		switch ev.Type {
		case "response.completed", "response.incomplete":
			var sb strings.Builder
			for _, item := range ev.Response.Output {
				if item.Type != "message" {
					continue
				}
				for _, part := range item.Content {
					if part.Type == "output_text" {
						sb.WriteString(part.Text)
					}
				}
			}
			return Response{sb.String(), Usage{ev.Response.Usage.In, ev.Response.Usage.Out}}, nil
		case "response.failed":
			if e := ev.Response.Error; e != nil {
				return Response{}, scrub(fmt.Errorf("provider error %s: %s", e.Code, e.Message), key)
			}
			return Response{}, errors.New("provider error: response failed")
		case "error":
			return Response{}, scrub(fmt.Errorf("provider error %s: %s", ev.Code, ev.Message), key)
		}
	}
	if err := sc.Err(); err != nil {
		return Response{}, scrub(err, key)
	}
	return Response{}, errors.New("provider stream ended before the response completed")
}
