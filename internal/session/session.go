// Package session persists local conversations independently of configuration.
package session

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"jevharness/internal/config"
	"jevharness/internal/openrouter"
	"jevharness/internal/privatefile"
)

const maxSize = 32 << 20

// ModelUsage stores reported usage across routing, chat, and compaction requests.
type ModelUsage struct {
	UnknownCost int     `json:"unknown_cost_requests,omitempty"`
	Requests    int     `json:"requests"`
	TokensIn    int     `json:"tokens_in"`
	TokensOut   int     `json:"tokens_out"`
	Cost        float64 `json:"cost"`
}

type Session struct {
	Version      int                   `json:"version,omitempty"`
	Running      bool                  `json:"running,omitempty"`
	PendingTool  string                `json:"pending_tool,omitempty"`
	Mode         string                `json:"mode,omitempty"`
	Models       map[string]ModelUsage `json:"models,omitempty"`
	ID           string                `json:"id"`
	Title        string                `json:"title"`
	CWD          string                `json:"cwd"`
	Updated      time.Time             `json:"updated"`
	Messages     []openrouter.Message  `json:"messages"`
	Transcript   string                `json:"transcript"`
	Pinned       string                `json:"pinned,omitempty"`
	LastRole     config.Role           `json:"last_role"`
	TokensIn     int                   `json:"tokens_in"`
	TokensOut    int                   `json:"tokens_out"`
	Cost         float64               `json:"cost"`
	Turns        int                   `json:"turns"`
	ContextUsed  int                   `json:"context_used"`
	ContextModel string                `json:"context_model"`
}

type Store struct{ Dir string }

func DefaultStore() Store {
	return Store{Dir: filepath.Join(filepath.Dir(config.Path()), "sessions")}
}

func NewID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

func validID(id string) bool {
	b, err := hex.DecodeString(id)
	return err == nil && len(b) == 16 && hex.EncodeToString(b) == id
}

// Save atomically replaces a private file; incomplete writes leave the old session intact.
func (s Store) Save(v Session) error {
	if !validID(v.ID) {
		return fmt.Errorf("invalid session ID")
	}
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if len(data) > maxSize {
		return fmt.Errorf("session exceeds 32 MiB")
	}
	if err := os.MkdirAll(s.Dir, 0700); err != nil {
		return err
	}
	if err := os.Chmod(s.Dir, 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(s.Dir, ".session-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err := f.Chmod(0600); err != nil {
		f.Close()
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), filepath.Join(s.Dir, v.ID+".json"))
}

func (s Store) Load(id string) (Session, error) {
	var v Session
	if !validID(id) {
		return v, fmt.Errorf("invalid session ID")
	}
	data, err := privatefile.Read(filepath.Join(s.Dir, id+".json"), maxSize)
	if err != nil {
		return v, err
	}
	if err := json.Unmarshal(data, &v); err != nil {
		return v, err
	}
	if v.ID != id {
		return v, fmt.Errorf("session ID mismatch")
	}
	return v, nil
}

// List returns newest first, scoped to the working directory. Unreadable files
// are reported instead of silently presenting an incomplete list.
func (s Store) List(cwd string) ([]Session, error) {
	entries, err := os.ReadDir(s.Dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var all []Session
	for _, e := range entries {
		id := e.Name()[:len(e.Name())-len(filepath.Ext(e.Name()))]
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" || !validID(id) {
			continue
		}
		v, err := s.Load(id)
		if err != nil {
			continue // One corrupt session must not hide every healthy session.
		}
		if v.CWD == cwd {
			v.Messages = nil
			v.Transcript = ""
			all = append(all, v)
		}
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].Updated.Equal(all[j].Updated) {
			return all[i].ID < all[j].ID
		}
		return all[i].Updated.After(all[j].Updated)
	})
	return all, nil
}
