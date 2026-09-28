// Package tools provides the local file/shell tools exposed to the model.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"

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
// dir; absolute paths pass through.
func All(dir string) []Tool {
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
	return []Tool{
		mk("read_file",
			"Read a file. Returns lines prefixed with line numbers (N|). Use offset/limit for large files.",
			json.RawMessage(`{
				"type": "object",
				"properties": {
					"path":   {"type": "string", "description": "file path, relative to cwd or absolute"},
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
					"content": {"type": "string"}
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
					"new_string": {"type": "string"}
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
			"Run a shell command (sh -c) in the working directory. Returns combined stdout+stderr.",
			json.RawMessage(`{
				"type": "object",
				"properties": {
					"command":         {"type": "string"},
					"timeout_seconds": {"type": "integer", "description": "default 60, max 600"}
				},
				"required": ["command"]
			}`), runBash),
	}
}

// Find returns the named tool among All(dir).
func Find(dir, name string) (Tool, bool) {
	for _, t := range All(dir) {
		if t.Def.Function.Name == name {
			return t, true
		}
	}
	return Tool{}, false
}

func resolve(dir, path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(dir, path)
}

func truncate(s string) string {
	if len(s) > maxOut {
		return s[:maxOut] + fmt.Sprintf("\n[truncated %d bytes]", len(s)-maxOut)
	}
	return s
}
