// Package config loads, validates and saves jev-harness configuration.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Role maps a Jev criteria name to an OpenRouter model.
type Role struct {
	Name        string `json:"name"`        // ^[a-z0-9_-]{1,32}$, unique; used verbatim as Jev criteria key
	Model       string `json:"model"`       // OpenRouter model id, must contain "/"
	Description string `json:"description"` // Jev criteria text: when this role should win
}

// Config is the on-disk application configuration.
type Config struct {
	APIKey              string           `json:"api_key,omitempty"`
	JevModel            string           `json:"jev_model"`                  // default "typesafe/jev-1.13"
	ConfidenceThreshold float64          `json:"confidence_threshold"`       // default 0.5, range [0,1]
	ChatInputLines      bool             `json:"chat_input_lines,omitempty"` // show horizontal rules around the chat input
	StatusLine          StatusLineConfig `json:"status_line"`
	DefaultRole         string           `json:"default_role"` // must name an existing role
	Roles               []Role           `json:"roles"`
}

// StatusLineConfig stores hidden items so older configs keep all items visible.
type StatusLineConfig struct {
	HideModel   bool `json:"hide_model,omitempty"`
	HideTokens  bool `json:"hide_tokens,omitempty"`
	HideContext bool `json:"hide_context,omitempty"`
	HideCost    bool `json:"hide_cost,omitempty"`
}

var nameRe = regexp.MustCompile(`^[a-z0-9_-]{1,32}$`)

// Path returns the config file path:
// $XDG_CONFIG_HOME/jev-harness/config.json, else ~/.config/jev-harness/config.json.
func Path() string {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		if home, err := os.UserHomeDir(); err == nil {
			base = filepath.Join(home, ".config")
		}
	}
	return filepath.Join(base, "jev-harness", "config.json")
}

// Default returns a working starter config with two roles.
func Default() Config {
	return Config{
		JevModel:            "typesafe/jev-1.13",
		ConfidenceThreshold: 0.5,
		DefaultRole:         "basic",
		Roles: []Role{
			{
				Name:        "basic",
				Model:       "openai/gpt-4o-mini",
				Description: "Short factual questions, small single-file edits, formatting, renames, explaining a snippet.",
			},
			{
				Name:        "complex",
				Model:       "anthropic/claude-sonnet-4.5",
				Description: "Multi-step tasks, debugging across files, architecture or design decisions, ambiguous requirements.",
			},
		},
	}
}

// Load reads the config file. A missing file returns Default() and no error.
// A present-but-unreadable or unparseable file returns an error.
func Load() (Config, error) {
	f, err := os.Open(Path())
	if errors.Is(err, os.ErrNotExist) {
		return Default(), nil
	}
	if err != nil {
		return Config{}, fmt.Errorf("read config: %w", err)
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, (1<<20)+1))
	if err != nil {
		return Config{}, fmt.Errorf("read config: %w", err)
	}
	if len(data) > 1<<20 {
		return Config{}, errors.New("config exceeds 1 MiB")
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse config %s: %w", Path(), err)
	}
	return cfg, nil
}

// Save writes the config file, creating the directory (0700) and file (0600).
func Save(cfg Config) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	p := Path()
	if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	if err := os.Chmod(filepath.Dir(p), 0700); err != nil {
		return fmt.Errorf("protect config dir: %w", err)
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	if len(data) > 1<<20 {
		return errors.New("config exceeds 1 MiB")
	}
	f, err := os.CreateTemp(filepath.Dir(p), ".config-*")
	if err != nil {
		return fmt.Errorf("create config: %w", err)
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
	return os.Rename(f.Name(), p)
}

// RoleByName returns the named role, or false.
func (c Config) RoleByName(name string) (Role, bool) {
	for _, r := range c.Roles {
		if r.Name == name {
			return r, true
		}
	}
	return Role{}, false
}

// Validate enforces the field rules. It returns one error per violation
// joined by "; " so the settings form can display it verbatim.
func (c Config) Validate() error {
	var errs []string
	if len(c.Roles) == 0 {
		errs = append(errs, "at least one role is required")
	}
	seen := map[string]bool{}
	for _, r := range c.Roles {
		if !nameRe.MatchString(r.Name) {
			errs = append(errs, fmt.Sprintf("role name %q must match ^[a-z0-9_-]{1,32}$", r.Name))
		}
		if seen[r.Name] {
			errs = append(errs, fmt.Sprintf("duplicate role name %q", r.Name))
		}
		seen[r.Name] = true
		if !strings.Contains(r.Model, "/") {
			errs = append(errs, fmt.Sprintf("role %q model %q must be an OpenRouter id like provider/model", r.Name, r.Model))
		}
		if strings.TrimSpace(r.Description) == "" {
			errs = append(errs, fmt.Sprintf("role %q needs a description (it is the Jev criteria text)", r.Name))
		}
	}
	if _, ok := c.RoleByName(c.DefaultRole); !ok {
		errs = append(errs, fmt.Sprintf("default_role %q does not match any role", c.DefaultRole))
	}
	if math.IsNaN(c.ConfidenceThreshold) || math.IsInf(c.ConfidenceThreshold, 0) || c.ConfidenceThreshold < 0 || c.ConfidenceThreshold > 1 {
		errs = append(errs, "confidence_threshold must be in [0,1]")
	}
	if c.JevModel == "" {
		errs = append(errs, "jev_model is required")
	}
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

// Key resolves the OpenRouter API key: env first, then config value.
func (c Config) Key() string {
	if k := os.Getenv("OPENROUTER_API_KEY"); k != "" {
		return k
	}
	return c.APIKey
}
