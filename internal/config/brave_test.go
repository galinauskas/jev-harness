package config

import (
	"strings"
	"testing"
)

func TestBraveSelectionCredentialsAndPolicy(t *testing.T) {
	t.Setenv("BRAVE_API_KEY", " env-brave-secret ")
	t.Setenv("EXA_API_KEY", "")
	cfg := Default()
	if cfg.WebSearchProvider() != "exa" {
		t.Fatal("legacy default changed")
	}
	cfg.SearchProvider = "brave"
	if cfg.ProviderKey("brave") != "env-brave-secret" || cfg.WebSearchStatus("/project") != "available" {
		t.Fatal("Brave unavailable")
	}
	cfg.BraveAPIKey = " saved-brave-secret "
	if cfg.ProviderKey("brave") != "saved-brave-secret" {
		t.Fatal("saved key did not override environment")
	}
	cfg.Safety.Projects = map[string][]string{"/project": {"openrouter", "exa"}}
	if !strings.Contains(cfg.WebSearchStatus("/project"), "enable brave") {
		t.Fatal("Brave bypassed project policy")
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
	if !strings.Contains(cfg.WebSearchStatus("/other"), "BRAVE_API_KEY") {
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
