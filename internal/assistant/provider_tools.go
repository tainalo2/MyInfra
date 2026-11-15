package assistant

import (
	"context"
	"encoding/json"
	"strings"
)

func toolDescription(t ToolSpec) string {
	if t.Title == "" || strings.HasPrefix(t.Description, t.Title) {
		return t.Description
	}
	if t.Description == "" {
		return t.Title
	}
	return t.Title + ". " + t.Description
}

func toolSchema(t ToolSpec) map[string]any {
	if t.Schema == nil {
		return map[string]any{"type": "object", "properties": map[string]any{}}
	}
	return t.Schema
}

// CompleteWithTools calls the Messages API with tools: each step goes back
// as the assistant's own content, then a user message of tool_result blocks.
func (c Claude) CompleteWithTools(ctx context.Context, q ToolRequest) (ToolTurn, Usage, error) {
	if q.Key == "" {
		return ToolTurn{}, Usage{}, ErrNoKey
	}
	msgs := make([]any, 0, len(q.Messages)+2*len(q.Steps))
	for _, m := range q.Messages {
		msgs = append(msgs, m)
	}
	for _, st := range q.Steps {
		var content any = st.Turn.Raw
		if len(st.Turn.Raw) == 0 {
			content = claudeBlocks(st.Turn)
		}
		msgs = append(msgs, map[string]any{"role": "assistant", "content": content})
		results := make([]map[string]any, 0, len(st.Results))
		for _, r := range st.Results {
			text := r.Text
			if text == "" {
				text = "(empty)"
			}
			results = append(results, map[string]any{"type": "tool_result", "tool_use_id": r.CallID, "content": text, "is_error": r.IsError})
		}
		msgs = append(msgs, map[string]any{"role": "user", "content": results})
	}
	tools := make([]map[string]any, 0, len(q.Tools))
	for _, t := range q.Tools {
		tools = append(tools, map[string]any{"name": t.Name, "description": toolDescription(t), "input_schema": toolSchema(t)})
	}
	req := map[string]any{"model": q.Model, "max_tokens": 8192, "system": q.System, "messages": msgs, "tools": tools}
	if budget := claudeBudget[q.Effort]; budget > 0 {
		req["max_tokens"] = 8192 + budget
		req["thinking"] = map[string]any{"type": "enabled", "budget_tokens": budget}
	}
	var out struct {
		Content []json.RawMessage `json:"content"`
		Usage   struct {
			In  int `json:"input_tokens"`
			Out int `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := doJSON(ctx, c.base()+"/v1/messages", map[string]string{"x-api-key": q.Key, "anthropic-version": "2023-06-01"}, req, &out, q.Key); err != nil {
		return ToolTurn{}, Usage{}, err
	}
	var turn ToolTurn
	var sb strings.Builder
	for _, raw := range out.Content {
		var block struct {
			Type  string          `json:"type"`
			Text  string          `json:"text"`
			ID    string          `json:"id"`
			Name  string          `json:"name"`
			Input json.RawMessage `json:"input"`
		}
		if json.Unmarshal(raw, &block) != nil {
			continue
		}
		switch block.Type {
		case "text":
			sb.WriteString(block.Text)
		case "tool_use":
			turn.Calls = append(turn.Calls, ToolCall{ID: block.ID, Name: block.Name, Args: block.Input})
		}
	}
	turn.Text = sb.String()
	turn.Raw, _ = json.Marshal(out.Content)
	return turn, Usage{out.Usage.In, out.Usage.Out}, nil
}

// claudeBlocks rebuilds a turn's content when the raw answer is missing.
func claudeBlocks(t ToolTurn) []map[string]any {
	var blocks []map[string]any
	if t.Text != "" {
		blocks = append(blocks, map[string]any{"type": "text", "text": t.Text})
	}
	for _, c := range t.Calls {
		blocks = append(blocks, map[string]any{"type": "tool_use", "id": c.ID, "name": c.Name, "input": c.Args})
	}
	return blocks
}

// CompleteWithTools calls the Responses API with function tools. With
// store false nothing stays on OpenAI's side, so every step goes back in
// input: the function_call items and their function_call_output. Only
// function tools are offered: the ChatGPT plan token refuses hosted ones.
func (o OpenAI) CompleteWithTools(ctx context.Context, q ToolRequest) (ToolTurn, Usage, error) {
	if q.Key == "" {
		return ToolTurn{}, Usage{}, ErrNoKey
	}
	input := make([]map[string]any, 0, len(q.Messages)+3*len(q.Steps))
	for _, m := range q.Messages {
		input = append(input, map[string]any{"role": m.Role, "content": m.Content})
	}
	for _, st := range q.Steps {
		if st.Turn.Text != "" {
			input = append(input, map[string]any{"role": "assistant", "content": st.Turn.Text})
		}
		for _, c := range st.Turn.Calls {
			args := string(c.Args)
			if args == "" {
				args = "{}"
			}
			input = append(input, map[string]any{"type": "function_call", "call_id": c.ID, "name": c.Name, "arguments": args})
		}
		for _, r := range st.Results {
			input = append(input, map[string]any{"type": "function_call_output", "call_id": r.CallID, "output": r.Text})
		}
	}
	tools := make([]map[string]any, 0, len(q.Tools))
	for _, t := range q.Tools {
		tools = append(tools, map[string]any{"type": "function", "name": t.Name, "description": toolDescription(t), "parameters": toolSchema(t), "strict": false})
	}
	body := map[string]any{"model": q.Model, "instructions": q.System, "input": input, "tools": tools, "store": false, "stream": true}
	if q.Effort != "" {
		body["reasoning"] = map[string]any{"effort": q.Effort}
	}
	return o.respond(ctx, body, q.Key)
}
