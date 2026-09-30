package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"jevharness/internal/workspace"
)

type Executor struct {
	DockerSandbox        bool
	CompactCommandOutput bool
	WebSearch            *WebSearch
	Workspace            *workspace.Workspace
	Mode, Image          string
	Output               func(string)
}
type Prepared struct {
	Name          string
	Args          json.RawMessage
	Review        string
	NeedsApproval bool
}

func (e *Executor) Prepare(name string, args json.RawMessage) (Prepared, error) {
	p := Prepared{Name: name, Args: args}
	if e.Workspace == nil {
		return p, errors.New("workspace is unavailable")
	}
	if _, ok := Find(e.Workspace.Stage, name, e.WebSearch); !ok {
		return p, fmt.Errorf("unknown tool %q", name)
	}
	switch name {
	case "web_search":
		a, err := parseWebSearch(args)
		if err != nil {
			return p, err
		}
		p.Review = "Search web via " + e.WebSearch.provider + ": " + a.Query
	case "search_files":
		p.Review = "Search staged workspace"
	case "read_command_output":
		if _, err := parseCommandOutput(args); err != nil {
			return p, err
		}
		p.Review = "Read saved command output"
	case "read_file", "list_dir":
		var a struct {
			Path string `json:"path"`
		}
		if err := json.Unmarshal(args, &a); err != nil {
			return p, err
		}
		path, err := workspace.Relative(a.Path)
		if err != nil {
			return p, err
		}
		p.Review = "Read staged workspace: " + filepath.Join(e.Workspace.Source, path)
	case "write_file", "edit_file":
		if e.Mode == "inspect" {
			return p, errors.New("inspect mode disables file changes")
		}
		c, hash, err := candidate(e.Workspace.Stage, args, name == "edit_file")
		if err != nil {
			return p, err
		}
		var a map[string]json.RawMessage
		if err = json.Unmarshal(args, &a); err != nil {
			return p, err
		}
		a["expected_sha256"], _ = json.Marshal(hash)
		p.Args, _ = json.Marshal(a)
		p.Review = "Stage change for " + filepath.Join(e.Workspace.Source, c.Path) + "\n" + workspace.Preview(c)
		p.NeedsApproval = e.Mode != "autonomous"
	case "bash":
		if e.Mode == "inspect" {
			return p, errors.New("inspect mode disables command execution")
		}
		var a struct {
			Command string `json:"command"`
		}
		if err := json.Unmarshal(args, &a); err != nil {
			return p, err
		}
		if strings.TrimSpace(a.Command) == "" {
			return p, errors.New("command is required")
		}
		p.Review = "Run local shell in staged workspace (normal host and network access):\n" + a.Command
		if e.DockerSandbox {
			p.Review = "Run in experimental Docker sandbox (no network or host credentials):\n" + a.Command
		}
		p.NeedsApproval = e.Mode != "autonomous"
	}
	return p, nil
}
func (e *Executor) Run(ctx context.Context, p Prepared) (string, error) {
	// Recheck policy at execution. UI approvals never grant a broader capability.
	if e.Mode == "inspect" && p.Name != "read_file" && p.Name != "list_dir" && p.Name != "search_files" && p.Name != "web_search" && p.Name != "read_command_output" {
		return "", errors.New("inspect mode disables this tool")
	}
	if p.Name == "bash" {
		out, err := e.bash(ctx, p.Args)
		if e.CompactCommandOutput {
			return e.saveCommandResult(out, err)
		}
		return out, err
	}
	if p.Name == "read_command_output" {
		return e.readCommandOutput(p.Args)
	}
	t, ok := Find(e.Workspace.Stage, p.Name, e.WebSearch)
	if !ok {
		return "", errors.New("unknown tool")
	}
	return t.Run(ctx, p.Args)
}
func (e *Executor) Attach(path string) (string, error) {
	path, err := workspace.Relative(path)
	if err != nil {
		return "", err
	}
	root, err := os.OpenRoot(e.Workspace.Stage)
	if err != nil {
		return "", err
	}
	defer root.Close()
	f, err := workspace.Read(root, path)
	if err != nil {
		return "", err
	}
	if len(f.Data) > 64<<10 {
		return "", errors.New("attachment exceeds 64 KiB; use read_file with an offset")
	}
	return "[Attached file: " + path + "]\n" + string(f.Data), nil
}
