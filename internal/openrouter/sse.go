package openrouter

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

// readStream consumes a chat-completions SSE body and sends StreamEvents on ch.
// It always sends exactly one terminal event (Done: true) and then closes ch.
//
// Quirks handled:
//   - ": OPENROUTER PROCESSING" keep-alive comments and blank lines are skipped.
//   - a top-level "error" field mid-stream (HTTP is already 200) ends the
//     stream with Err set.
//   - tool-call deltas accumulate by index: the first delta carries id/name,
//     later deltas append argument fragments.
//   - OpenRouter's final usage chunk repeats finish_reason with an empty
//     delta: usage is recorded, not treated as a second terminal event.
//   - ctx cancellation closes the body and yields Err = ctx.Err().
func readStream(ctx context.Context, body io.ReadCloser, started time.Time, ch chan<- StreamEvent) {
	defer close(ch)
	defer body.Close()

	// close body when ctx dies so a blocked Scan returns
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			body.Close()
		case <-done:
		}
	}()

	var (
		calls    []ToolCall
		finish   string
		usage    *Usage
		retErr   error
		received int
	)

	sc := bufio.NewScanner(body)
	sc.Buffer(make([]byte, 64*1024), 1<<20) // 1 MiB max line

scanLoop:
	for sc.Scan() {
		received += len(sc.Bytes())
		if received > 8<<20 {
			retErr = errors.New("stream exceeds 8 MiB")
			break
		}
		line := strings.TrimRight(sc.Text(), "\r")
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			break
		}

		var chunk streamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			retErr = fmt.Errorf("decode stream chunk: %w", err)
			break
		}
		if chunk.Err != nil {
			retErr = fmt.Errorf("stream error %v: %s", chunk.Err.Code, chunk.Err.Message)
			break
		}
		if chunk.Usage != nil {
			usage = chunk.Usage
		}
		for _, c := range chunk.Choices {
			if c.FinishReason != "" {
				finish = c.FinishReason
			}
			d := c.Delta
			if d.Content != "" {
				if !emit(ctx, ch, StreamEvent{TextDelta: d.Content}) {
					retErr = ctx.Err()
					break scanLoop
				}
			}
			for _, tc := range d.ToolCalls {
				if tc.Index < 0 || tc.Index >= 128 {
					retErr = fmt.Errorf("invalid tool call index %d", tc.Index)
					break
				}
				for len(calls) <= tc.Index {
					calls = append(calls, ToolCall{Type: "function"})
				}
				dst := &calls[tc.Index]
				if tc.ID != "" {
					dst.ID = tc.ID
				}
				if tc.Type != "" {
					dst.Type = tc.Type
				}
				if tc.Function.Name != "" {
					dst.Function.Name = tc.Function.Name
				}
				if len(dst.Function.Arguments)+len(tc.Function.Arguments) > 3<<20 {
					retErr = errors.New("tool arguments exceed 3 MiB")
					break
				}
				dst.Function.Arguments += tc.Function.Arguments
			}
		}
		if retErr != nil {
			break
		}
	}

	if retErr == nil {
		if err := sc.Err(); err != nil {
			if ctx.Err() != nil {
				retErr = ctx.Err()
			} else {
				retErr = fmt.Errorf("read stream: %w", err)
			}
		}
	}
	if ctx.Err() != nil && retErr == nil {
		retErr = ctx.Err()
	}
	if retErr == nil && finish == "" {
		retErr = errors.New("stream ended without finish_reason")
	}

	if retErr == nil && len(calls) > 0 {
		if finish != "tool_calls" {
			retErr = errors.New("incomplete tool calls")
		}
		seen := make(map[string]bool)
		for _, call := range calls {
			if call.ID == "" || seen[call.ID] || call.Type != "function" || call.Function.Name == "" || !json.Valid([]byte(call.Function.Arguments)) {
				retErr = errors.New("invalid or incomplete tool call")
				break
			}
			seen[call.ID] = true
		}
	}
	if retErr == nil && finish == "tool_calls" && len(calls) == 0 {
		retErr = errors.New("missing tool calls")
	}

	// Terminal event: unconditional send. The consumer drains until close
	// even after ctx cancellation, so "aborted" can surface in the UI.
	ch <- StreamEvent{
		Done:      true,
		ToolCalls: calls,
		Finish:    finish,
		Usage:     usage,
		Err:       retErr,
		Started:   started,
	}
}

func emit(ctx context.Context, ch chan<- StreamEvent, ev StreamEvent) bool {
	select {
	case ch <- ev:
		return true
	case <-ctx.Done():
		return false
	}
}
