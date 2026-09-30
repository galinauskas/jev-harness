package openrouter

import (
	"encoding/json"
	"fmt"
)

const maxToolArguments = 3 << 20

// All stream protocols must finish and validate calls before approval or execution.
func validateToolCalls(calls []ToolCall, finish string) error {
	if len(calls) > 128 {
		return fmt.Errorf("too many tool calls")
	}
	if len(calls) == 0 {
		if finish == "tool_calls" {
			return fmt.Errorf("missing tool calls")
		}
		return nil
	}
	if finish != "tool_calls" {
		return fmt.Errorf("incomplete tool calls")
	}
	seen := make(map[string]bool, len(calls))
	for _, call := range calls {
		if len(call.Function.Arguments) > maxToolArguments {
			return fmt.Errorf("tool arguments exceed 3 MiB")
		}
		if call.ID == "" || seen[call.ID] || call.Type != "function" || call.Function.Name == "" || !json.Valid([]byte(call.Function.Arguments)) {
			return fmt.Errorf("invalid or incomplete tool call")
		}
		seen[call.ID] = true
	}
	return nil
}
