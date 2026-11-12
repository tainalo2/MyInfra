package assistant

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
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
)

// ModelCapability describes a model and its supported reasoning levels.
type ModelCapability struct {
	Model              string             `json:"model"`
	SupportedReasons   []ReasoningEffort  `json:"supported_reasons"`
	DefaultReason      ReasoningEffort    `json:"default_reason"`
}

// Provider is the provider-agnostic interface used by the harness.
type Provider interface {
	Name() string
	Complete(ctx context.Context, model, system string, msgs []Message, apiKey string, reasoningEffort ReasoningEffort) (Response, error)
	SupportsSubscriptionAuth() bool
	ListModels() []ModelCapability
}

var ErrNoKey = errors.New("no_key")

func scrub(err error, key string) error {
	if err == nil || key == "" {
		return err
	}
	return errors.New(strings.ReplaceAll(err.Error(), key, "[redacted]"))
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

func (Claude) ListModels() []ModelCapability {
	return []ModelCapability{
		{
			Model:            "claude-opus",
			SupportedReasons: []ReasoningEffort{ReasoningLow, ReasoningMedium, ReasoningHigh},
			DefaultReason:    ReasoningMedium,
		},
		{
			Model:            "claude-sonnet",
			SupportedReasons: []ReasoningEffort{ReasoningLow, ReasoningMedium, ReasoningHigh},
			DefaultReason:    ReasoningMedium,
		},
		{
			Model:            "claude-haiku",
			SupportedReasons: []ReasoningEffort{ReasoningLow},
			DefaultReason:    ReasoningLow,
		},
	}
}

func (c Claude) Complete(ctx context.Context, model, system string, msgs []Message, key string, reasoningEffort ReasoningEffort) (Response, error) {
	if key == "" {
		return Response{}, ErrNoKey
	}
	base := c.BaseURL
	if base == "" {
		base = "https://api.anthropic.com"
	}
	var out struct {
		Content []struct{ Text string } `json:"content"`
		Usage   struct {
			In  int `json:"input_tokens"`
			Out int `json:"output_tokens"`
		} `json:"usage"`
	}
	req := map[string]any{"model": model, "max_tokens": 4096, "system": system, "messages": msgs}
	// Add thinking if reasoning is not low
	if reasoningEffort != ReasoningLow {
		req["thinking"] = map[string]any{
			"type":          "enabled",
			"budget_tokens": map[string]int{"low": 1000, "medium": 5000, "high": 10000}[string(reasoningEffort)],
		}
	}
	err := doJSON(ctx, base+"/v1/messages", map[string]string{"x-api-key": key, "anthropic-version": "2023-06-01"}, req, &out, key)
	if err != nil {
		return Response{}, err
	}
	var sb strings.Builder
	for _, p := range out.Content {
		sb.WriteString(p.Text)
	}
	return Response{sb.String(), Usage{out.Usage.In, out.Usage.Out}}, nil
}

// OpenAI adapter (Chat Completions), used for Codex/OpenAI-compatible endpoints.
type OpenAI struct{ BaseURL string }

func (OpenAI) Name() string { return "codex" }

func (OpenAI) SupportsSubscriptionAuth() bool { return true }

func (OpenAI) ListModels() []ModelCapability {
	return []ModelCapability{
		{
			Model:            "gpt-5-codex",
			SupportedReasons: []ReasoningEffort{ReasoningLow},
			DefaultReason:    ReasoningLow,
		},
		{
			Model:            "gpt-4-turbo",
			SupportedReasons: []ReasoningEffort{ReasoningLow},
			DefaultReason:    ReasoningLow,
		},
	}
}

func (o OpenAI) Complete(ctx context.Context, model, system string, msgs []Message, key string, reasoningEffort ReasoningEffort) (Response, error) {
	if key == "" {
		return Response{}, ErrNoKey
	}
	base := o.BaseURL
	if base == "" {
		base = "https://api.openai.com"
	}
	all := append([]Message{{"system", system}}, msgs...)
	var out struct {
		Choices []struct {
			Message Message `json:"message"`
		} `json:"choices"`
		Usage struct {
			In  int `json:"prompt_tokens"`
			Out int `json:"completion_tokens"`
		} `json:"usage"`
	}
	err := doJSON(ctx, base+"/v1/chat/completions", map[string]string{"authorization": "Bearer " + key},
		map[string]any{"model": model, "messages": all}, &out, key)
	if err != nil {
		return Response{}, err
	}
	text := ""
	if len(out.Choices) > 0 {
		text = out.Choices[0].Message.Content
	}
	return Response{text, Usage{out.Usage.In, out.Usage.Out}}, nil
}
