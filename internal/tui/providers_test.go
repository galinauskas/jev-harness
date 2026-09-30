package tui

import (
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"jevharness/internal/config"
	"jevharness/internal/router"
)

func providerSetting(s settingsModel, action string) int {
	for i, row := range s.rows() {
		if row.action == action {
			return i
		}
	}
	return -1
}

func TestProviderSettingsRepairProjectLockout(t *testing.T) {
	c := safetyChat(t)
	cfg := c.cfg
	cfg.Safety.Projects = map[string][]string{c.cwd: {"brave"}, "/other": {"deepseek"}}
	c.ag.SetConfig(cfg)
	s := newSettings(c.ag, cfg, 140, 50)
	s.tab = settingsProviders
	if s.cwd != c.cwd || !strings.Contains(s.View(), "Disabled in settings") {
		t.Fatal("project restrictions are hidden")
	}
	s.rowCursor = providerSetting(s, "provider:openrouter")
	s, _ = s.Update(settingsKey(tea.KeyEnter))
	if s.msgIsErr || !s.cfg.ProviderAllowed(c.cwd, "openrouter") || !c.ag.Config().ProviderAllowed(c.cwd, "openrouter") {
		t.Fatal("settings did not repair locked project", s.msg)
	}
	if cfg.ProviderAllowed(c.cwd, "openrouter") {
		t.Fatal("editing mutated original config")
	}
	loaded, err := config.Load()
	if err != nil || !loaded.ProviderAllowed(c.cwd, "openrouter") || !reflect.DeepEqual(loaded.Safety.Projects["/other"], []string{"deepseek"}) || !reflect.DeepEqual(loaded.Safety.AllowedProviders, cfg.Safety.AllowedProviders) {
		t.Fatal("project save changed other policies", err)
	}
	// A single toggle must never disable all configured chat roles.
	s, _ = s.Update(settingsKey(tea.KeyEnter))
	if !s.msgIsErr || !s.cfg.ProviderAllowed(c.cwd, "openrouter") {
		t.Fatal("settings allowed a routing lockout")
	}
	s.rowCursor = providerSetting(s, "restore-providers")
	s, _ = s.Update(settingsKey(tea.KeyEnter))
	if s.msgIsErr || !reflect.DeepEqual(s.cfg.Safety.Projects[c.cwd], config.Default().Safety.AllowedProviders) || !reflect.DeepEqual(s.cfg.Safety.Projects["/other"], []string{"deepseek"}) {
		t.Fatal("restore failed or changed another project", s.msg)
	}
}

func TestProviderSettingsToggleDoesNotReplaceOtherProviders(t *testing.T) {
	c := safetyChat(t)
	s := newSettings(c.ag, c.cfg, 140, 50)
	s.tab = settingsProviders
	s.rowCursor = providerSetting(s, "provider:exa")
	s, _ = s.Update(settingsKey(tea.KeyEnter))
	if s.msgIsErr || s.cfg.ProviderAllowed(c.cwd, "exa") || !s.cfg.ProviderAllowed(c.cwd, "openrouter") || !s.cfg.ProviderAllowed(c.cwd, "brave") {
		t.Fatal("search toggle changed chat or other search provider", s.msg)
	}
	s, _ = s.Update(settingsKey(tea.KeyEnter))
	if s.msgIsErr || !s.cfg.ProviderAllowed(c.cwd, "exa") {
		t.Fatal("toggle could not re-enable search", s.msg)
	}
}

func TestProvidersCommandRemoved(t *testing.T) {
	c := safetyChat(t)
	before := c.cfg
	c, _, handled := c.safetyCommand("/providers brave")
	if handled || !reflect.DeepEqual(before, c.cfg) {
		t.Fatal("removed command changed provider settings")
	}
	for _, command := range slashCommands {
		if command.name == "/providers" {
			t.Fatal("removed command still suggested")
		}
	}
}

func TestDisabledClassifierRouteLine(t *testing.T) {
	line := routeLine(router.Decision{Role: config.Role{Name: "direct", Provider: "deepseek", Model: "deepseek-chat"}, Source: router.SourceDefault})
	if !strings.Contains(line, "automatic routing disabled") || !strings.Contains(line, "deepseek-chat") || !strings.Contains(line, "direct") {
		t.Fatal("default route omitted role/model", line)
	}
}
