package session

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"jevharness/internal/privatefile"
)

type Record struct {
	At     time.Time `json:"at"`
	Kind   string    `json:"kind"`
	ToolID string    `json:"tool_id,omitempty"`
	Tool   string    `json:"tool,omitempty"`
	Model  string    `json:"model,omitempty"`
	Source string    `json:"source,omitempty"`
}

func (s Store) WorkspaceDir(id string) string { return filepath.Join(s.Dir, "workspaces", id) }
func (s Store) Append(id string, r Record) error {
	if !validID(id) {
		return errors.New("invalid session ID")
	}
	if err := os.MkdirAll(s.Dir, 0700); err != nil {
		return err
	}
	r.At = time.Now()
	data, err := json.Marshal(r)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(s.Dir, id+".events.jsonl"), os.O_WRONLY|os.O_APPEND|os.O_CREATE|privateAppendFlags, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() > maxSize {
		return errors.New("event journal exceeds limit or is not regular")
	}
	if _, err = f.Write(append(data, '\n')); err != nil {
		return err
	}
	return f.Sync()
}
func (s Store) Delete(id string) error {
	if !validID(id) {
		return errors.New("invalid session ID")
	}
	for _, suffix := range []string{".json", ".events.jsonl"} {
		if err := os.Remove(filepath.Join(s.Dir, id+suffix)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return os.RemoveAll(s.WorkspaceDir(id))
}
func (s Store) Prune(days int) error {
	if days <= 0 {
		return nil
	}
	entries, err := os.ReadDir(s.Dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	cutoff := time.Now().AddDate(0, 0, -days)
	for _, e := range entries {
		if filepath.Ext(e.Name()) != ".json" {
			continue
		}
		id := e.Name()[:len(e.Name())-5]
		if !validID(id) {
			continue
		}
		v, err := s.Load(id)
		if err != nil {
			continue
		}
		if v.Updated.Before(cutoff) && !v.Running && v.PendingTool == "" {
			if err = s.Delete(id); err != nil {
				return err
			}
		}
	}
	return nil
}
func (s Store) Rename(id, title string) error {
	if title == "" {
		return errors.New("session title is required")
	}
	v, err := s.Load(id)
	if err != nil {
		return err
	}
	v.Title = title
	return s.Save(v)
}
func (s Store) Export(id, path string) error {
	v, err := s.Load(id)
	if err != nil {
		return err
	}
	v.Transcript = ""
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(data)
	return err
}
func (s Store) Events(id string) ([]Record, error) {
	if !validID(id) {
		return nil, fmt.Errorf("invalid session ID")
	}
	data, err := privatefile.Read(filepath.Join(s.Dir, id+".events.jsonl"), maxSize)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var records []Record
	for len(data) > 0 {
		var r Record
		end := 0
		for end < len(data) && data[end] != '\n' {
			end++
		}
		if end == len(data) {
			break
		}
		if err = json.Unmarshal(data[:end], &r); err != nil {
			return nil, err
		}
		records = append(records, r)
		data = data[end+1:]
	}
	return records, nil
}
