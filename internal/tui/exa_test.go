package tui

import (
	"strings"
	"testing"

	"charm.land/bubbles/v2/textinput"
	"jevharness/internal/config"
)

func TestExaSettingsMask(t *testing.T) {
	cfg := config.Default()
	cfg.ExaAPIKey = "saved-exa-secret"
	s := newSettings(nil, cfg, 140, 50)
	s.tab = settingsProviders
	s, _ = s.openValueForm(s.rows()[3])
	if len(s.inputs) != 1 || s.inputs[0].Value() != cfg.ExaAPIKey || s.inputs[0].EchoMode != textinput.EchoPassword {
		t.Fatal("Exa masked settings field missing")
	}
	if view := s.View(); !strings.Contains(view, "Exa API key") || strings.Contains(view, cfg.ExaAPIKey) {
		t.Fatal("Exa settings not masked")
	}
}
