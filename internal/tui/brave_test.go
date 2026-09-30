package tui

import (
	"strings"
	"testing"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"jevharness/internal/config"
)

func TestBraveSettingsAndProviderChoice(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	cfg := config.Default()
	cfg.BraveAPIKey = "saved-brave-secret"
	s := newSettings(nil, cfg, 140, 50)
	s.tab = settingsProviders
	s, _ = s.openValueForm(s.rows()[4])
	if s.inputs[0].EchoMode != textinput.EchoPassword || s.inputs[0].Value() != cfg.BraveAPIKey || strings.Contains(s.View(), cfg.BraveAPIKey) {
		t.Fatal("Brave credential is not masked")
	}
	s.inputs[0].SetValue("new-brave-secret")
	s, _ = s.Update(settingsKey(tea.KeyEnter))
	if s.msgIsErr || s.cfg.BraveAPIKey != "new-brave-secret" {
		t.Fatal(s.msg)
	}
	s.rowCursor = 5
	s, _ = s.Update(settingsKey(tea.KeyEnter))
	if s.mode != sSearchProvider || s.choiceCursor != 0 {
		t.Fatal("provider chooser missing")
	}
	s, _ = s.Update(settingsKey(tea.KeyDown))
	s, _ = s.Update(settingsKey(tea.KeyEscape))
	if s.cfg.WebSearchProvider() != "exa" {
		t.Fatal("cancel changed provider")
	}
	s, _ = s.Update(settingsKey(tea.KeyEnter))
	s, _ = s.Update(settingsKey(tea.KeyDown))
	s, _ = s.Update(settingsKey(tea.KeyEnter))
	loaded, err := config.Load()
	if err != nil || s.msgIsErr || s.mode != sList || loaded.SearchProvider != "brave" || loaded.BraveAPIKey != "new-brave-secret" {
		t.Fatal("selection not saved", err, s.msg)
	}
	s.cfg.Safety.AllowedProviders = []string{"openrouter", "exa"}
	if !strings.Contains(s.View(), "Disabled in settings") {
		t.Fatal("blocked Brave key not indicated")
	}
}
