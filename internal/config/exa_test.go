package config

import "testing"

func TestExaCredential(t *testing.T) {
	t.Setenv("EXA_API_KEY", " env-exa-secret ")
	cfg := Default()
	if cfg.ProviderKey("exa") != "env-exa-secret" {
		t.Fatal("Exa environment key unavailable")
	}
	cfg.ExaAPIKey = " saved-exa-secret "
	if cfg.ProviderKey("exa") != "saved-exa-secret" {
		t.Fatal("saved key did not override env")
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
	if cfg.WebSearchStatus() == "available" {
		t.Fatal("missing key reported available")
	}
	cfg.ExaAPIKey = "saved-exa-secret"
	if cfg.WebSearchStatus() != "available" {
		t.Fatal("configured search reported unavailable")
	}
}
