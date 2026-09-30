// Package tools provides workspace tools and optional web search exposed to the model.
package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"jevharness/internal/openrouter"
)

const maxOut = 20_000

// Tool pairs a wire definition with its executor. Run returns the content
// for the tool message; executor errors are already rendered as
// "error: <msg>" in the returned content so the turn never aborts.
type Tool struct {
	Def openrouter.ToolDef
	Run func(ctx context.Context, args json.RawMessage) (string, error)
}

// All returns defs and executors for every tool. Paths resolve relative to
// dir, inside an already scoped staged workspace.
func All(dir string, webSearch ...*Exa) []Tool {
	mk := func(name, desc string, params json.RawMessage,
		run func(dir string, ctx context.Context, args json.RawMessage) (string, error)) Tool {
		var def openrouter.ToolDef
		def.Type = "function"
		def.Function.Name = name
		def.Function.Description = desc
		def.Function.Parameters = params
		return Tool{
			Def: def,
			Run: func(ctx context.Context, args json.RawMessage) (string, error) {
				s, err := run(dir, ctx, args)
				if err != nil {
					return "error: " + err.Error(), nil
				}
				return truncate(s), nil
			},
		}
	}
	result := []Tool{
		mk("read_command_output", "Read a saved command log in bounded slices. Use the ID returned by bash; offset is a zero-based byte offset, limit defaults to 2000 bytes (max 4000).", json.RawMessage(`{"type":"object","properties":{"id":{"type":"string"},"offset":{"type":"integer"},"limit":{"type":"integer"}},"required":["id"]}`), func(_ string, _ context.Context, _ json.RawMessage) (string, error) {
			return "", fmt.Errorf("command logs require the session Executor")
		}),
		mk("search_files", "Find workspace files by glob or search literal text; limited to 100 matches.", json.RawMessage(`{"type":"object","properties":{"query":{"type":"string"},"glob":{"type":"string"}}}`), runSearch),
		mk("read_file",
			"Read a file. Returns lines prefixed with line numbers (N|). Use offset/limit for large files.",
			json.RawMessage(`{
				"type": "object",
				"properties": {
					"path":   {"type": "string", "description": "file path, relative to the staged workspace; absolute paths are denied"},
					"offset": {"type": "integer", "description": "1-based first line to return"},
					"limit":  {"type": "integer", "description": "max lines to return"}
				},
				"required": ["path"]
			}`), runReadFile),
		mk("write_file",
			"Write content to a file, creating parent directories. Overwrites existing files.",
			json.RawMessage(`{
				"type": "object",
				"properties": {
					"path":    {"type": "string"},
					"content": {"type": "string"},
 "expected_sha256": {"type":"string","description":"Hash returned by read_file, or missing for a new file"}
				},
				"required": ["path", "content"]
			}`), runWriteFile),
		mk("edit_file",
			"Replace old_string with new_string in a file. old_string must occur exactly once; include enough surrounding context to make it unique.",
			json.RawMessage(`{
				"type": "object",
				"properties": {
					"path":       {"type": "string"},
					"old_string": {"type": "string"},
					"new_string": {"type": "string"},
 "expected_sha256": {"type":"string","description":"Hash returned by read_file"}
				},
				"required": ["path", "old_string", "new_string"]
			}`), runEditFile),
		mk("list_dir",
			"List directory entries, one per line, directories suffixed with /.",
			json.RawMessage(`{
				"type": "object",
				"properties": {
					"path": {"type": "string", "description": "directory path, default ."}
				}
			}`), runListDir),
		mk("bash",
			"Run sh -c in the staged workspace using the configured shell backend. Local execution has normal host and network access; the optional experimental Docker sandbox is offline. Use relative paths for project changes.",
			json.RawMessage(`{
				"type": "object",
				"properties": {
					"command":         {"type": "string"},
					"timeout_seconds": {"type": "integer", "description": "default 60, max 600"}
				},
				"required": ["command"]
			}`), runBash),
	}
	for _, search := range webSearch {
		if search != nil {
			result = append(result, search.tool())
		}
	}
	return result
}

// Find returns the named tool among All(dir).
func Find(dir, name string, webSearch ...*Exa) (Tool, bool) {
	for _, t := range All(dir, webSearch...) {
		if t.Def.Function.Name == name {
			return t, true
		}
	}
	return Tool{}, false
}

func truncate(s string) string {
	if len(s) > maxOut {
		return s[:maxOut] + fmt.Sprintf("\n[truncated %d bytes]", len(s)-maxOut)
	}
	return s
}
