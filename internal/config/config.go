// Package config loads, validates and saves jev-harness configuration.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"jevharness/internal/privatefile"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Role maps a Jev criteria name to a provider and model.
type Role struct {
	ContextWindow int    `json:"context_window,omitempty"`
	OutputLimit   int    `json:"output_limit,omitempty"`
	DisableTools  bool   `json:"disable_tools,omitempty"`
	Provider      string `json:"provider,omitempty"` // empty means openrouter
	Name          string `json:"name"`               // ^[a-z0-9_-]{1,32}$, unique; used verbatim as Jev criteria key
	Model         string `json:"model"`              // provider model id
	Description   string `json:"description"`        // Jev criteria text: when this role should win
}

// Config is the on-disk application configuration.
type Config struct {
	CompactCommandOutput bool             `json:"compact_command_output,omitempty"`
	SearchProvider       string           `json:"search_provider,omitempty"`
	BraveAPIKey          string           `json:"brave_api_key,omitempty"`
	ExaAPIKey            string           `json:"exa_api_key,omitempty"`
	Safety               SafetyConfig     `json:"safety"`
	Limits               Limits           `json:"limits"`
	OpenCodeGoAPIKey     string           `json:"opencode_go_api_key,omitempty"`
	DeepSeekAPIKey       string           `json:"deepseek_api_key,omitempty"`
	APIKey               string           `json:"api_key,omitempty"`
	JevModel             string           `json:"jev_model"`                  // default "typesafe/jev-1.13"
	ConfidenceThreshold  float64          `json:"confidence_threshold"`       // default 0.5, range [0,1]
	CompactionThreshold  int              `json:"compaction_threshold"`       // percent of model context; 0 disables
	ChatInputLines       bool             `json:"chat_input_lines,omitempty"` // show horizontal rules around the chat input
	StatusLine           StatusLineConfig `json:"status_line"`
	DefaultRole          string           `json:"default_role"` // must name an existing role
	Roles                []Role           `json:"roles"`
}

// Safety settings are user-owned. Repository instructions cannot broaden them.
type SafetyConfig struct {
	DockerSandbox    bool                `json:"docker_sandbox,omitempty"` // experimental, opt-in
	Mode             string              `json:"mode"`
	SandboxImage     string              `json:"sandbox_image"`
	AllowedProviders []string            `json:"allowed_providers"`
	Projects         map[string][]string `json:"projects,omitempty"`
	Ephemeral        bool                `json:"ephemeral,omitempty"`
	RetentionDays    int                 `json:"retention_days,omitempty"`
}

type Limits struct {
	OutputTokens int     `json:"output_tokens"`
	TotalTokens  int     `json:"total_tokens"`
	Seconds      int     `json:"seconds"`
	Cost         float64 `json:"cost_usd"`
}

func (c *Config) ApplyDefaults() {
	if c.Safety.Mode == "" {
		c.Safety.Mode = "develop"
	}
	if c.Safety.SandboxImage == "" {
		c.Safety.SandboxImage = "jev-harness-sandbox:1"
	}
	if c.Safety.AllowedProviders == nil {
		c.Safety.AllowedProviders = []string{"openrouter", "deepseek", "opencode-go", "exa", "brave"}
	}
	if c.Limits.OutputTokens == 0 {
		c.Limits.OutputTokens = 8192
	}
	if c.Limits.TotalTokens == 0 {
		c.Limits.TotalTokens = 200000
	}
	if c.Limits.Seconds == 0 {
		c.Limits.Seconds = 600
	}
}

func (c Config) ProviderAllowed(cwd, provider string) bool {
	allowed := c.Safety.AllowedProviders
	if project, ok := c.Safety.Projects[cwd]; ok {
		allowed = project
	}
	for _, p := range allowed {
		if p == provider {
			return true
		}
	}
	return false
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
	cfg := Config{
		JevModel:            "typesafe/jev-1.13",
		ConfidenceThreshold: 0.5,
		CompactionThreshold: 80,
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
	cfg.ApplyDefaults()
	return cfg
}

// Load reads the config file. A missing file returns Default() and no error.
// A present-but-unreadable or unparseable file returns an error.
func Load() (Config, error) {
	data, err := privatefile.Read(Path(), 1<<20)
	if errors.Is(err, os.ErrNotExist) {
		return Default(), nil
	}
	if err != nil {
		return Config{}, fmt.Errorf("read config: %w", err)
	}
	cfg := Config{CompactionThreshold: 80}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse config %s: %w", Path(), err)
	}
	cfg.ApplyDefaults()
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
	if c.SearchProvider != "" && c.SearchProvider != "exa" && c.SearchProvider != "brave" {
		errs = append(errs, "search_provider must be exa or brave")
	}
	if c.Safety.Mode != "" && c.Safety.Mode != "inspect" && c.Safety.Mode != "develop" && c.Safety.Mode != "autonomous" {
		errs = append(errs, "mode must be inspect, develop or autonomous")
	}
	if c.Limits.OutputTokens < 0 || c.Limits.TotalTokens < 0 || c.Limits.Seconds < 0 || c.Limits.Cost < 0 || math.IsNaN(c.Limits.Cost) || math.IsInf(c.Limits.Cost, 0) {
		errs = append(errs, "limits must be finite nonnegative values")
	}
	if strings.HasPrefix(c.Safety.SandboxImage, "-") || strings.ContainsAny(c.Safety.SandboxImage, " \t\r\n") {
		errs = append(errs, "sandbox_image must be a Docker image reference")
	}
	if c.Safety.RetentionDays < 0 {
		errs = append(errs, "retention_days must be nonnegative")
	}
	for _, list := range append([][]string{c.Safety.AllowedProviders}, projectProviderLists(c.Safety.Projects)...) {
		for _, provider := range list {
			if provider != "openrouter" && provider != "deepseek" && provider != "opencode-go" && provider != "exa" && provider != "brave" {
				errs = append(errs, "unknown allowed provider: "+provider)
			}
		}
	}
	if len(c.Roles) == 0 {
		errs = append(errs, "at least one role is required")
	}
	seen := map[string]bool{}
	for _, r := range c.Roles {
		if r.ContextWindow < 0 || r.OutputLimit < 0 {
			errs = append(errs, "model limits must be nonnegative")
		}
		if !nameRe.MatchString(r.Name) {
			errs = append(errs, fmt.Sprintf("role name %q must match ^[a-z0-9_-]{1,32}$", r.Name))
		}
		if seen[r.Name] {
			errs = append(errs, fmt.Sprintf("duplicate role name %q", r.Name))
		}
		seen[r.Name] = true
		if r.Backend() != "openrouter" && r.Backend() != "deepseek" && r.Backend() != "opencode-go" {
			errs = append(errs, fmt.Sprintf("role %q has unknown provider %q (use openrouter, deepseek, or opencode-go)", r.Name, r.Provider))
		}
		if strings.TrimSpace(r.Model) == "" || strings.HasPrefix(r.Model, "-") {
			errs = append(errs, fmt.Sprintf("role %q needs a valid model ID", r.Name))
		}
		if r.Backend() == "openrouter" && !strings.Contains(r.Model, "/") {
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
	if c.CompactionThreshold < 0 || c.CompactionThreshold > 100 {
		errs = append(errs, "compaction_threshold must be in [0,100] (0 disables)")
	}
	if c.JevModel == "" {
		errs = append(errs, "jev_model is required")
	}
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

// Key uses the explicitly saved key, falling back to the environment.
func (c Config) Key() string {
	if k := strings.TrimSpace(c.APIKey); k != "" {
		return k
	}
	return strings.TrimSpace(os.Getenv("OPENROUTER_API_KEY"))
}

// Backend preserves the OpenRouter default for older role configurations.
func (r Role) Backend() string {
	if r.Provider == "" {
		return "openrouter"
	}
	return r.Provider
}

// ProviderKey resolves each service's credential independently.
func (c Config) ProviderKey(provider string) string {
	var saved, env string
	switch provider {
	case "openrouter", "":
		return c.Key()
	case "brave":
		saved, env = c.BraveAPIKey, "BRAVE_API_KEY"
	case "exa":
		saved, env = c.ExaAPIKey, "EXA_API_KEY"
	case "deepseek":
		saved, env = c.DeepSeekAPIKey, "DEEPSEEK_API_KEY"
	case "opencode-go":
		saved, env = c.OpenCodeGoAPIKey, "OPENCODE_GO_API_KEY"
	default:
		return ""
	}
	if key := strings.TrimSpace(saved); key != "" {
		return key
	}
	return strings.TrimSpace(os.Getenv(env))
}

func projectProviderLists(projects map[string][]string) [][]string {
	var lists [][]string
	for _, p := range projects {
		lists = append(lists, p)
	}
	return lists
}

// ExaSearchStatus explains the effective project capability without revealing keys.
func (c Config) ExaSearchStatus(cwd string) string {
	if !c.ProviderAllowed(cwd, "exa") {
		return "disabled in settings (enable exa in /settings → Providers)"
	}
	if c.ProviderKey("exa") == "" {
		return "unavailable: set the Exa key in /settings or EXA_API_KEY"
	}
	return "available"
}

// WebSearchProvider preserves Exa for older configurations.
func (c Config) WebSearchProvider() string {
	if c.SearchProvider == "" {
		return "exa"
	}
	return c.SearchProvider
}

func (c Config) WebSearchStatus(cwd string) string {
	provider := c.WebSearchProvider()
	if !c.ProviderAllowed(cwd, provider) {
		return "disabled in settings (enable " + provider + " in /settings → Providers)"
	}
	if c.ProviderKey(provider) == "" {
		if provider == "brave" {
			return "unavailable: set the Brave key in /settings or BRAVE_API_KEY"
		}
		return "unavailable: set the Exa key in /settings or EXA_API_KEY"
	}
	return "available"
}
