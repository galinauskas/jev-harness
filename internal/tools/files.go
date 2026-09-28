package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func runReadFile(dir string, _ context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Path   string `json:"path"`
		Offset int    `json:"offset"`
		Limit  int    `json:"limit"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", err
	}
	f, err := openRegular(resolve(dir, p.Path))
	if err != nil {
		return "", err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, (2<<20)+1))
	if err != nil {
		return "", err
	}
	fileTruncated := len(data) > 2<<20
	if fileTruncated {
		data = data[:2<<20]
	}
	lines := strings.Split(string(data), "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	start := 0
	if p.Offset > 1 {
		start = p.Offset - 1
	}
	if start > len(lines) {
		start = len(lines)
	}
	end := len(lines)
	if p.Limit > 0 && p.Limit < end-start {
		end = start + p.Limit
	}
	var b strings.Builder
	for i := start; i < end; i++ {
		fmt.Fprintf(&b, "%d│%s\n", i+1, lines[i])
	}
	if end < len(lines) {
		fmt.Fprintf(&b, "[%d more lines]", len(lines)-end)
	}
	if fileTruncated {
		b.WriteString("\n[file exceeds 2 MiB; read is limited to its first 2 MiB]")
	}
	return b.String(), nil
}

func runWriteFile(dir string, _ context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", err
	}
	if p.Path == "" {
		return "", errors.New("path is required")
	}
	if len(p.Content) > 2<<20 {
		return "", errors.New("content exceeds 2 MiB")
	}
	full := resolve(dir, p.Path)
	if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
		return "", err
	}
	if err := writeRegular(full, []byte(p.Content)); err != nil {
		return "", err
	}
	return fmt.Sprintf("wrote %d bytes", len(p.Content)), nil
}

func runEditFile(dir string, _ context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Path string `json:"path"`
		Old  string `json:"old_string"`
		New  string `json:"new_string"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", err
	}
	if p.Path == "" || p.Old == "" {
		return "", errors.New("path and old_string are required")
	}
	full := resolve(dir, p.Path)
	info, err := os.Stat(full)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("path must be a regular file")
	}
	if info.Size() > 2<<20 {
		return "", errors.New("file exceeds 2 MiB edit limit")
	}
	f, err := openRegular(full)
	if err != nil {
		return "", err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, (2<<20)+1))
	if err != nil {
		return "", err
	}
	if len(data) > 2<<20 {
		return "", errors.New("file exceeds 2 MiB edit limit")
	}
	n := strings.Count(string(data), p.Old)
	switch {
	case n == 0:
		return "", fmt.Errorf("old_string not found")
	case n > 1:
		return "", fmt.Errorf("old_string matches %d times; include more context", n)
	}
	if len(data)-len(p.Old)+len(p.New) > 2<<20 {
		return "", errors.New("edited content exceeds 2 MiB")
	}
	out := strings.Replace(string(data), p.Old, p.New, 1)
	if err := writeRegular(full, []byte(out)); err != nil {
		return "", err
	}
	return fmt.Sprintf("edited %s", p.Path), nil
}

func runListDir(dir string, _ context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", err
	}
	full := p.Path
	if full == "" {
		full = "."
	}
	ents, err := os.ReadDir(resolve(dir, full))
	if err != nil {
		return "", err
	}
	names := make([]string, 0, len(ents))
	for _, e := range ents {
		n := e.Name()
		if e.IsDir() {
			n += "/"
		}
		names = append(names, n)
	}
	sort.Strings(names)
	return strings.Join(names, "\n"), nil
}

// openRegular rejects devices, directories and FIFOs without blocking on open.
func openRegular(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|nonblockFlag, 0)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	if !info.Mode().IsRegular() {
		f.Close()
		return nil, errors.New("path must be a regular file")
	}
	return f, nil
}

func writeRegular(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|nonblockFlag, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("path must be a regular file")
	}
	if err := f.Truncate(0); err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		return err
	}
	return f.Close()
}
