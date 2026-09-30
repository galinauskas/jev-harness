package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"jevharness/internal/workspace"
	"path/filepath"
	"sort"
	"strings"
)

func runSearch(dir string, ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Query string `json:"query"`
		Glob  string `json:"glob"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", err
	}
	if p.Query == "" && p.Glob == "" {
		return "", fmt.Errorf("query or glob is required")
	}
	files, err := workspace.Snapshot(dir)
	if err != nil {
		return "", err
	}
	paths := make([]string, 0, len(files))
	for p := range files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	var b strings.Builder
	count := 0
	for _, path := range paths {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		if p.Glob != "" {
			match, err := filepath.Match(p.Glob, path)
			if err != nil {
				return "", err
			}
			if !match {
				match, err = filepath.Match(p.Glob, filepath.Base(path))
				if err != nil {
					return "", err
				}
			}
			if !match {
				continue
			}
		}
		if p.Query == "" {
			fmt.Fprintln(&b, path)
			count++
		} else {
			for i, line := range strings.Split(string(files[path].Data), "\n") {
				if strings.Contains(line, p.Query) {
					fmt.Fprintf(&b, "%s:%d:%s\n", path, i+1, line)
					count++
					if count >= 100 {
						break
					}
				}
			}
		}
		if count >= 100 {
			b.WriteString("[limited to 100 matches]\n")
			break
		}
	}
	return b.String(), nil
}
