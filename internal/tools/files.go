package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"jevharness/internal/workspace"
)

type fileArgs struct {
	Path     string  `json:"path"`
	Content  *string `json:"content"`
	Old      string  `json:"old_string"`
	New      *string `json:"new_string"`
	Offset   int     `json:"offset"`
	Limit    int     `json:"limit"`
	Expected string  `json:"expected_sha256"`
}

func parseFile(dir string, args json.RawMessage) (*os.Root, fileArgs, error) {
	var p fileArgs
	if err := json.Unmarshal(args, &p); err != nil {
		return nil, p, err
	}
	path, err := workspace.Relative(p.Path)
	if err != nil {
		return nil, p, err
	}
	p.Path = path
	root, err := os.OpenRoot(dir)
	return root, p, err
}
func runReadFile(dir string, _ context.Context, args json.RawMessage) (string, error) {
	root, p, err := parseFile(dir, args)
	if err != nil {
		return "", err
	}
	defer root.Close()
	f, err := workspace.Read(root, p.Path)
	if err != nil {
		return "", err
	}
	lines := strings.Split(string(f.Data), "\n")
	start := max(0, p.Offset-1)
	start = min(start, len(lines))
	end := len(lines)
	if p.Limit > 0 {
		end = min(end, start+p.Limit)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "sha256: %s\n", workspace.Hash(f))
	for i := start; i < end; i++ {
		fmt.Fprintf(&b, "%d│%s\n", i+1, lines[i])
	}
	if end < len(lines) {
		fmt.Fprintf(&b, "[%d more lines]", len(lines)-end)
	}
	return b.String(), nil
}
func candidate(dir string, args json.RawMessage, edit bool) (workspace.Change, string, error) {
	root, p, err := parseFile(dir, args)
	if err != nil {
		return workspace.Change{}, "", err
	}
	defer root.Close()
	if p.Path == "." {
		return workspace.Change{}, "", errors.New("file path is required")
	}
	before, err := workspace.Current(root, p.Path)
	if err != nil {
		return workspace.Change{}, "", err
	}
	expected := workspace.Hash(before)
	if p.Expected != "" && p.Expected != expected {
		return workspace.Change{}, "", errors.New("file changed since it was inspected")
	}
	var data []byte
	mode := os.FileMode(0644)
	if before != nil {
		mode = before.Mode
	}
	if edit {
		if before == nil || p.Old == "" || p.New == nil {
			return workspace.Change{}, "", errors.New("existing path, old_string and new_string are required")
		}
		if n := strings.Count(string(before.Data), p.Old); n != 1 {
			return workspace.Change{}, "", fmt.Errorf("old_string must match exactly once (matched %d)", n)
		}
		data = []byte(strings.Replace(string(before.Data), p.Old, *p.New, 1))
	} else {
		if p.Content == nil {
			return workspace.Change{}, "", errors.New("content is required")
		}
		data = []byte(*p.Content)
	}
	if len(data) > workspace.MaxFile {
		return workspace.Change{}, "", errors.New("content exceeds 2 MiB")
	}
	return workspace.Change{Path: p.Path, Before: before, After: &workspace.File{Data: data, Mode: mode}}, expected, nil
}
func runWriteFile(dir string, _ context.Context, args json.RawMessage) (string, error) {
	return mutate(dir, args, false)
}
func runEditFile(dir string, _ context.Context, args json.RawMessage) (string, error) {
	return mutate(dir, args, true)
}
func mutate(dir string, args json.RawMessage, edit bool) (string, error) {
	c, hash, err := candidate(dir, args, edit)
	if err != nil {
		return "", err
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return "", err
	}
	defer root.Close()
	if err = workspace.AtomicWrite(root, c.Path, c.After.Data, c.After.Mode, hash); err != nil {
		return "", err
	}
	return fmt.Sprintf("staged %s (%d bytes); original project unchanged until /apply", c.Path, len(c.After.Data)), nil
}
func runListDir(dir string, _ context.Context, args json.RawMessage) (string, error) {
	root, p, err := parseFile(dir, args)
	if err != nil {
		return "", err
	}
	defer root.Close()
	if err = workspace.NoLinks(root, p.Path); err != nil {
		return "", err
	}
	f, err := root.Open(p.Path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	ents, err := f.ReadDir(10001)
	if err != nil && len(ents) == 0 {
		return "", err
	}
	if len(ents) > 10000 {
		return "", errors.New("directory exceeds 10,000 entries")
	}
	var b strings.Builder
	for _, e := range ents {
		if workspace.Protected(e.Name()) || e.Type()&os.ModeSymlink != 0 {
			continue
		}
		name := e.Name()
		if e.IsDir() {
			name += "/"
		}
		b.WriteString(name + "\n")
	}
	return b.String(), nil
}
