package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBraveSelectionCredentials(t *testing.T) {
	t.Setenv("BRAVE_API_KEY", " env-brave-secret ")
	t.Setenv("EXA_API_KEY", "")
	cfg := Default()
	if cfg.WebSearchProvider() != "exa" {
		t.Fatal("legacy default changed")
	}
	cfg.SearchProvider = "brave"
	if cfg.ProviderKey("brave") != "env-brave-secret" || cfg.WebSearchStatus() != "available" {
		t.Fatal("Brave unavailable")
	}
	cfg.BraveAPIKey = " saved-brave-secret "
	if cfg.ProviderKey("brave") != "saved-brave-secret" {
		t.Fatal("saved key did not override environment")
	}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	cfg.Roles[0].Provider = "brave"
	if cfg.Validate() == nil {
		t.Fatal("Brave accepted as chat provider")
	}
	cfg.Roles[0].Provider = "openrouter"
	cfg.SearchProvider = "unknown"
	if cfg.Validate() == nil {
		t.Fatal("unknown search provider accepted")
	}
	cfg.SearchProvider = "brave"
	cfg.BraveAPIKey = ""
	t.Setenv("BRAVE_API_KEY", "")
	if !strings.Contains(cfg.WebSearchStatus(), "BRAVE_API_KEY") {
		t.Fatal("missing credential not explained")
	}
}

func TestSearchSelectionPersists(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	cfg := Default()
	cfg.SearchProvider, cfg.BraveAPIKey = "brave", "saved-brave-secret"
	if err := Save(cfg); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load()
	if err != nil || loaded.SearchProvider != "brave" || loaded.BraveAPIKey != cfg.BraveAPIKey {
		t.Fatal(loaded, err)
	}
}

func TestLegacyProviderRestrictionsRemovedOnSave(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	cfg := Default()
	cfg.SearchProvider, cfg.BraveAPIKey = "brave", "saved-brave-secret"
	cfg.Safety.Mode, cfg.Safety.DockerSandbox = "inspect", true
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	// Both global and project restrictions previously blocked every provider.
	data = []byte(strings.Replace(string(data), `"safety":{`, `"safety":{"allowed_providers":[],"projects":{"/project":[]},`, 1))
	if err := os.MkdirAll(filepath.Dir(Path()), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(Path(), data, 0600); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load()
	if err != nil || loaded.WebSearchStatus() != "available" || loaded.WebSearchProvider() != "brave" {
		t.Fatal("legacy restrictions blocked default search", err)
	}
	if loaded.Safety != cfg.Safety || loaded.DefaultRole != cfg.DefaultRole {
		t.Fatal("migration changed other settings")
	}
	if err := Save(loaded); err != nil {
		t.Fatal(err)
	}
	data, err = os.ReadFile(Path())
	if err != nil || strings.Contains(string(data), `"allowed_providers"`) || strings.Contains(string(data), `"projects"`) {
		t.Fatal("obsolete restrictions survived save", err)
	}
}
