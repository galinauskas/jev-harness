package openrouter

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

func NewOpenCodeGo(apiKey string) *Client {
	c := New(apiKey)
	c.base, c.chatPath, c.goProvider = "https://opencode.ai", "/zen/go/v1/chat/completions", true
	return c
}

func (c *Client) SetSessionID(id string) { c.mu.Lock(); defer c.mu.Unlock(); c.sessionID = id }

func goModelID(model string) string {
	return strings.TrimPrefix(strings.TrimSpace(model), "opencode-go/")
}

func (c *Client) goContextLength(model string) (int, error) {
	if info, ok := goModels[goModelID(model)]; ok {
		return info.Context, nil
	}
	return 0, fmt.Errorf("context window is not configured for OpenCode Go model %q", model)
}

func goProtocol(model string) string {
	if info, ok := goModels[model]; ok {
		return info.Protocol
	}
	switch {
	case strings.HasPrefix(model, "minimax-"), strings.HasPrefix(model, "qwen"):
		return "messages"
	case strings.HasPrefix(model, "gpt-"), strings.HasPrefix(model, "grok-"), strings.HasPrefix(model, "muse-"):
		return "responses"
	default:
		return "chat"
	}
}

func (c *Client) goChatStream(ctx context.Context, req ChatRequest) (<-chan StreamEvent, error) {
	c.mu.RLock()
	sessionID := c.sessionID
	c.mu.RUnlock()
	if sessionID == "" {
		return nil, fmt.Errorf("OpenCode Go requires a conversation session ID")
	}
	req.Model = goModelID(req.Model)
	protocol := goProtocol(req.Model)
	nativeKey := "opencode-go:" + protocol + ":" + req.Model
	path := "/zen/go/v1/" + protocol
	var body any
	switch protocol {
	case "messages":
		var err error
		body, err = goMessagesRequest(req, nativeKey)
		if err != nil {
			return nil, err
		}
	case "responses":
		body = goResponsesRequest(req, nativeKey)
	default:
		path = c.chatPath
		req.Stream = true
		req.StreamOptions = &StreamOptions{IncludeUsage: true}
		req.Messages = append([]Message(nil), req.Messages...)
		for i := range req.Messages {
			if req.Messages[i].NativeProvider != nativeKey {
				req.Messages[i].ReasoningContent = ""
			}
			req.Messages[i].NativeProvider, req.Messages[i].NativeItems = "", nil
		}
		body = req
	}
	r, err := c.newReq(ctx, c.base+path, body)
	if err != nil {
		return nil, err
	}
	if protocol == "messages" {
		r.Header.Set("x-api-key", c.apiKey())
		r.Header.Set("anthropic-version", "2023-06-01")
	}
	started := time.Now()
	resp, err := c.http.Do(r)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode/100 != 2 {
		return nil, decodeError(resp)
	}
	ch := make(chan StreamEvent, 64)
	if protocol == "chat" {
		raw := make(chan StreamEvent, 64)
		go readStream(ctx, resp.Body, started, raw)
		go func() {
			defer close(ch)
			for ev := range raw {
				if ev.Done {
					ev.NativeProvider = nativeKey
					ch <- ev
				} else if !emit(ctx, ch, ev) {
					// Drain through the terminal event after cancellation.
					for end := range raw {
						if end.Done {
							end.NativeProvider = nativeKey
							ch <- end
						}
					}
					return
				}
			}
		}()
	} else {
		go readGoStream(ctx, resp.Body, started, protocol, nativeKey, ch)
	}
	return ch, nil
}

func goMessagesRequest(req ChatRequest, nativeKey string) (any, error) {
	system := []string{}
	messages := []map[string]any{}
	for _, m := range req.Messages {
		if m.Role == "system" {
			system = append(system, m.Content)
			continue
		}
		role := m.Role
		blocks := []json.RawMessage{}
		if m.NativeProvider == nativeKey && len(m.NativeItems) > 0 {
			blocks = append(blocks, m.NativeItems...)
		} else {
			if m.Role == "tool" {
				role = "user"
				b, _ := json.Marshal(map[string]any{"type": "tool_result", "tool_use_id": m.ToolCallID, "content": m.Content})
				blocks = append(blocks, b)
			} else {
				if m.Content != "" {
					b, _ := json.Marshal(map[string]string{"type": "text", "text": m.Content})
					blocks = append(blocks, b)
				}
				for _, call := range m.ToolCalls {
					if !json.Valid([]byte(call.Function.Arguments)) {
						return nil, fmt.Errorf("invalid tool arguments in history")
					}
					b, _ := json.Marshal(map[string]any{"type": "tool_use", "id": call.ID, "name": call.Function.Name, "input": json.RawMessage(call.Function.Arguments)})
					blocks = append(blocks, b)
				}
			}
		}
		if len(blocks) == 0 {
			continue
		}
		// Multiple tool results belong in one user message, before any next text.
		if len(messages) > 0 && messages[len(messages)-1]["role"] == role {
			prior := messages[len(messages)-1]["content"].([]json.RawMessage)
			messages[len(messages)-1]["content"] = append(prior, blocks...)
		} else {
			messages = append(messages, map[string]any{"role": role, "content": blocks})
		}
	}
	tools := []map[string]any{}
	for _, t := range req.Tools {
		tools = append(tools, map[string]any{"name": t.Function.Name, "description": t.Function.Description, "input_schema": t.Function.Parameters})
	}
	maxTokens := req.MaxTokens
	if maxTokens == 0 {
		maxTokens = 8192
	}
	body := map[string]any{"model": req.Model, "system": strings.Join(system, "\n\n"), "messages": messages, "stream": true, "max_tokens": maxTokens}
	if len(tools) > 0 {
		body["tools"] = tools
	}
	return body, nil
}

func goResponsesRequest(req ChatRequest, nativeKey string) any {
	input := []any{}
	for _, m := range req.Messages {
		if m.NativeProvider == nativeKey && len(m.NativeItems) > 0 {
			for _, item := range m.NativeItems {
				input = append(input, item)
			}
			continue
		}
		if m.Role == "tool" {
			input = append(input, map[string]any{"type": "function_call_output", "call_id": m.ToolCallID, "output": m.Content})
			continue
		}
		if m.Content != "" {
			input = append(input, map[string]string{"role": m.Role, "content": m.Content})
		}
		for _, call := range m.ToolCalls {
			input = append(input, map[string]string{"type": "function_call", "call_id": call.ID, "name": call.Function.Name, "arguments": call.Function.Arguments})
		}
	}
	tools := []map[string]any{}
	for _, t := range req.Tools {
		tools = append(tools, map[string]any{"type": "function", "name": t.Function.Name, "description": t.Function.Description, "parameters": t.Function.Parameters, "strict": false})
	}
	body := map[string]any{"model": req.Model, "input": input, "stream": true, "store": false, "include": []string{"reasoning.encrypted_content"}}
	if len(tools) > 0 {
		body["tools"] = tools
	}
	if req.MaxTokens > 0 {
		body["max_output_tokens"] = req.MaxTokens
	}
	return body
}
