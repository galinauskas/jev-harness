package config

import "testing"

func TestExaCredentialAndPolicy(t *testing.T) {
	t.Setenv("EXA_API_KEY", " env-exa-secret ")
	cfg := Default()
	if cfg.ProviderKey("exa") != "env-exa-secret" || !cfg.ProviderAllowed("/project", "exa") {
		t.Fatal("Exa env or default policy unavailable")
	}
	cfg.ExaAPIKey = " saved-exa-secret "
	if cfg.ProviderKey("exa") != "saved-exa-secret" {
		t.Fatal("saved key did not override env")
	}
	cfg.Safety.Projects = map[string][]string{"/project": {"deepseek"}}
	if cfg.ProviderAllowed("/project", "exa") {
		t.Fatal("project policy bypassed")
	}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	cfg.Roles[0].Provider = "exa"
	if cfg.Validate() == nil {
		t.Fatal("Exa accepted as chat provider")
	}
}

func TestExaSearchStatus(t *testing.T) {
	t.Setenv("EXA_API_KEY", "")
	cfg := Default()
	if cfg.ExaSearchStatus("/project") == "available" {
		t.Fatal("missing key reported available")
	}
	cfg.ExaAPIKey = "saved-exa-secret"
	if cfg.ExaSearchStatus("/project") != "available" {
		t.Fatal("configured search reported unavailable")
	}
	cfg.Safety.Projects = map[string][]string{"/project": {"openrouter"}}
	if cfg.ExaSearchStatus("/project") != "disabled in settings (enable exa in /settings → Providers)" {
		t.Fatal("project restriction not explained")
	}
}
