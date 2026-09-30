package openrouter

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"
)

type goUsage struct {
	Input      *int     `json:"input_tokens"`
	Output     *int     `json:"output_tokens"`
	CacheRead  int      `json:"cache_read_input_tokens"`
	CacheWrite int      `json:"cache_creation_input_tokens"`
	Cost       *float64 `json:"cost"`
}

type goBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	Thinking  string          `json:"thinking,omitempty"`
	Signature string          `json:"signature,omitempty"`
	Data      string          `json:"data,omitempty"`
	arguments string
	closed    bool
}

type goResponse struct {
	Status     string            `json:"status"`
	Output     []json.RawMessage `json:"output"`
	Usage      *goUsage          `json:"usage"`
	Error      *apiError         `json:"error"`
	Incomplete struct {
		Reason string `json:"reason"`
	} `json:"incomplete_details"`
}

// readGoStream translates native Messages/Responses events to harness events.
// Tool calls are delivered only after a complete, validated terminal event.
func readGoStream(ctx context.Context, body io.ReadCloser, started time.Time, protocol, nativeKey string, ch chan<- StreamEvent) {
	defer close(ch)
	defer body.Close()
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			body.Close()
		case <-done:
		}
	}()
	terminal := StreamEvent{Done: true, Started: started, NativeProvider: nativeKey}
	blocks := map[int]*goBlock{}
	complete := false
	textSeen := false
	var usage *Usage
	updateUsage := func(u *goUsage) {
		if u == nil {
			return
		}
		if usage == nil {
			usage = &Usage{}
		}
		if u.Input != nil {
			usage.PromptTokens = *u.Input + u.CacheRead + u.CacheWrite
		}
		if u.Output != nil {
			usage.CompletionTokens = *u.Output
		}
		usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
		if u.Cost != nil {
			usage.Cost = *u.Cost
			usage.CostKnown = true
		}
	}
	dispatch := func(data string) error {
		if data == "[DONE]" {
			return nil
		}
		// The event message field may be either an object or an error string.
		var chunk struct {
			Type         string          `json:"type"`
			Index        int             `json:"index"`
			ContentBlock goBlock         `json:"content_block"`
			Delta        json.RawMessage `json:"delta"`
			Message      json.RawMessage `json:"message"`
			Usage        *goUsage        `json:"usage"`
			Response     goResponse      `json:"response"`
			Error        *apiError       `json:"error"`
		}
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			return fmt.Errorf("decode Go stream: %w", err)
		}
		if chunk.Error != nil {
			return fmt.Errorf("Go stream: %s", chunk.Error.Message)
		}
		switch chunk.Type {
		case "error":
			return fmt.Errorf("Go stream error: %s", truncate(data, 200))
		case "message_start":
			var m struct {
				Usage *goUsage `json:"usage"`
			}
			if err := json.Unmarshal(chunk.Message, &m); err != nil {
				return err
			}
			updateUsage(m.Usage)
		case "content_block_start":
			if chunk.Index < 0 || chunk.Index >= 128 || blocks[chunk.Index] != nil {
				return fmt.Errorf("invalid content block index")
			}
			block := chunk.ContentBlock
			blocks[chunk.Index] = &block
			if block.Type == "text" && block.Text != "" {
				textSeen = true
				if !emit(ctx, ch, StreamEvent{TextDelta: block.Text}) {
					return ctx.Err()
				}
			}
		case "content_block_delta":
			b := blocks[chunk.Index]
			if b == nil || b.closed {
				return fmt.Errorf("delta for missing or closed content block")
			}
			var delta struct {
				Type        string `json:"type"`
				Text        string `json:"text"`
				PartialJSON string `json:"partial_json"`
				Thinking    string `json:"thinking"`
				Signature   string `json:"signature"`
			}
			if err := json.Unmarshal(chunk.Delta, &delta); err != nil {
				return err
			}
			switch delta.Type {
			case "text_delta":
				b.Text += delta.Text
				textSeen = true
				if !emit(ctx, ch, StreamEvent{TextDelta: delta.Text}) {
					return ctx.Err()
				}
			case "input_json_delta":
				if len(b.arguments)+len(delta.PartialJSON) > maxToolArguments {
					return fmt.Errorf("tool arguments exceed 3 MiB")
				}
				b.arguments += delta.PartialJSON
			case "thinking_delta":
				b.Thinking += delta.Thinking
			case "signature_delta":
				b.Signature += delta.Signature
			}
		case "content_block_stop":
			b := blocks[chunk.Index]
			if b == nil || b.closed {
				return fmt.Errorf("stop for missing or closed content block")
			}
			b.closed = true
			if b.arguments != "" {
				b.Input = json.RawMessage(b.arguments)
			}
		case "message_delta":
			var d struct {
				StopReason string `json:"stop_reason"`
			}
			if err := json.Unmarshal(chunk.Delta, &d); err != nil {
				return err
			}
			switch d.StopReason {
			case "tool_use":
				terminal.Finish = "tool_calls"
			case "max_tokens":
				terminal.Finish = "length"
			case "end_turn", "stop_sequence", "refusal":
				terminal.Finish = "stop"
			}
			updateUsage(chunk.Usage)
		case "message_stop":
			if terminal.Finish == "" {
				return fmt.Errorf("Messages stream ended without stop_reason")
			}
			indices := []int{}
			for i := range blocks {
				indices = append(indices, i)
			}
			sort.Ints(indices)
			for _, i := range indices {
				b := blocks[i]
				if !b.closed {
					return fmt.Errorf("incomplete content block")
				}
				if b.Type == "tool_use" {
					call := ToolCall{ID: b.ID, Type: "function"}
					call.Function.Name, call.Function.Arguments = b.Name, string(b.Input)
					terminal.ToolCalls = append(terminal.ToolCalls, call)
				}
				raw, err := json.Marshal(b)
				if err != nil {
					return err
				}
				terminal.NativeItems = append(terminal.NativeItems, raw)
			}
			complete = true
		case "response.output_text.delta", "response.refusal.delta":
			var text string
			if err := json.Unmarshal(chunk.Delta, &text); err != nil {
				return err
			}
			textSeen = true
			if !emit(ctx, ch, StreamEvent{TextDelta: text}) {
				return ctx.Err()
			}
		case "response.completed", "response.incomplete", "response.failed":
			updateUsage(chunk.Response.Usage)
			if chunk.Type == "response.failed" {
				return fmt.Errorf("Go response failed")
			}
			terminal.Finish = "stop"
			if chunk.Type == "response.incomplete" {
				if chunk.Response.Incomplete.Reason != "max_output_tokens" {
					return fmt.Errorf("Go response incomplete: %s", chunk.Response.Incomplete.Reason)
				}
				terminal.Finish = "length"
			}
			for _, raw := range chunk.Response.Output {
				var item struct {
					Type      string `json:"type"`
					CallID    string `json:"call_id"`
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
					Content   []struct {
						Text    string `json:"text"`
						Refusal string `json:"refusal"`
					} `json:"content"`
				}
				if err := json.Unmarshal(raw, &item); err != nil {
					return err
				}
				if item.Type == "function_call" {
					call := ToolCall{ID: item.CallID, Type: "function"}
					call.Function.Name, call.Function.Arguments = item.Name, item.Arguments
					terminal.ToolCalls = append(terminal.ToolCalls, call)
				}
				if item.Type == "message" && !textSeen {
					for _, part := range item.Content {
						if !emit(ctx, ch, StreamEvent{TextDelta: part.Text + part.Refusal}) {
							return ctx.Err()
						}
					}
				}
			}
			if len(terminal.ToolCalls) > 0 && terminal.Finish == "stop" {
				terminal.Finish = "tool_calls"
			}
			terminal.NativeItems = chunk.Response.Output
			complete = true
		}
		return nil
	}
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 64<<10), 1<<20)
	var data []string
	received := 0
	for scanner.Scan() {
		line := strings.TrimSuffix(scanner.Text(), "\r")
		received += len(line)
		if received > 8<<20 {
			terminal.Err = fmt.Errorf("Go stream exceeds 8 MiB")
			break
		}
		if line == "" {
			if len(data) > 0 {
				terminal.Err = dispatch(strings.Join(data, "\n"))
				data = nil
			}
			if terminal.Err != nil || complete {
				break
			}
		} else if strings.HasPrefix(line, "data:") {
			data = append(data, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}
	if terminal.Err == nil && !complete && len(data) > 0 {
		terminal.Err = dispatch(strings.Join(data, "\n"))
	}
	if terminal.Err == nil {
		terminal.Err = scanner.Err()
	}
	if ctx.Err() != nil {
		terminal.Err = ctx.Err()
	}
	if terminal.Err == nil && !complete {
		terminal.Err = fmt.Errorf("Go stream ended without a completion event")
	}
	if terminal.Err == nil {
		terminal.Err = validateToolCalls(terminal.ToolCalls, terminal.Finish)
	}
	if terminal.Err != nil {
		terminal.ToolCalls = nil
		terminal.NativeItems = nil
	}
	terminal.Usage = usage
	ch <- terminal
}
