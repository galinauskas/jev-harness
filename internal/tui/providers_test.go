package tui

import (
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"jevharness/internal/config"
)

func providerSetting(s settingsModel, action string) int {
	for i, row := range s.rows() {
		if row.action == action {
			return i
		}
	}
	return -1
}

func TestProviderSettingsDefaultSearchOnly(t *testing.T) {
	c := safetyChat(t)
	s := newSettings(c.ag, c.cfg, 140, 50)
	s.tab = settingsProviders
	for _, row := range s.rows() {
		if strings.HasPrefix(row.action, "provider:") || row.action == "restore-providers" {
			t.Fatal("per-project provider controls still present")
		}
	}
	s.rowCursor = providerSetting(s, "search-provider")
	if s.rowCursor < 0 || !strings.Contains(s.View(), "Default web_search provider") {
		t.Fatal("default search picker missing")
	}
	s, _ = s.Update(settingsKey(tea.KeyEnter))
	s, _ = s.Update(settingsKey(tea.KeyDown))
	s, _ = s.Update(settingsKey(tea.KeyEnter))
	loaded, err := config.Load()
	if err != nil || s.msgIsErr || loaded.WebSearchProvider() != "brave" || c.ag.Config().WebSearchProvider() != "brave" {
		t.Fatal("default selection not saved or applied to agent", err, s.msg)
	}
	if !reflect.DeepEqual(loaded.Roles, c.cfg.Roles) || loaded.DefaultRole != c.cfg.DefaultRole {
		t.Fatal("search selection changed chat routing")
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
