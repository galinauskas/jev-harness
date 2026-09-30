package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"jevharness/internal/config"
)

func settingsKey(code rune) tea.KeyPressMsg { return tea.KeyPressMsg{Code: code} }

func TestSettingsNavigationAndEditFocus(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	s := newSettings(nil, config.Default(), 120, 30)
	s, _ = s.Update(settingsKey(tea.KeyTab))
	if s.rowCursor != 1 {
		t.Fatal("Tab should jump to Status line")
	}
	before := s.cfg.StatusLine.HideModel
	s, _ = s.Update(settingsKey(tea.KeyEnter))
	if s.mode != sList || s.cfg.StatusLine.HideModel == before {
		t.Fatal("status should toggle directly on the list")
	}
	for range 3 {
		s, _ = s.Update(settingsKey(tea.KeyRight))
	}
	for range 3 {
		s, _ = s.Update(settingsKey(tea.KeyDown))
	}
	s, _ = s.Update(settingsKey(tea.KeyEnter))
	if s.mode != sValueForm || len(s.inputs) != 1 || s.editRow.action != "key3" || !s.inputs[0].Focused() {
		t.Fatal("Exa selection should open only its key field")
	}
	s.inputs[0].SetValue("cancelled-secret")
	s, _ = s.Update(settingsKey(tea.KeyEscape))
	if s.cfg.ExaAPIKey != "" || s.mode != sList || s.tab != settingsProviders || s.msg != "" {
		t.Fatal("cancel should keep the original value and selection")
	}
	s, _ = s.Update(settingsKey(tea.KeyLeft))
	s, _ = s.Update(settingsKey(tea.KeyRight))
	if s.rowCursor != 3 {
		t.Fatal("returning to a tab should remember its selection")
	}
	s, _ = s.Update(settingsKey(tea.KeyRight))
	s, _ = s.Update(settingsKey(tea.KeyTab))
	s, _ = s.Update(settingsKey(tea.KeyEnter))
	if s.mode != sRoleForm || s.editingIdx != 1 {
		t.Fatal("Tab in Roles should select the next role")
	}
	s.focus = 3
	s, _ = s.Update(settingsKey(tea.KeyRight))
	if s.inputs[3].Value() != "deepseek" {
		t.Fatal("provider should cycle through supported choices")
	}
}

func TestSettingsScopedEditsAndValidation(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	cfg := config.Default()
	cfg.APIKey = "keep-router"
	cfg.DeepSeekAPIKey = "keep-deepseek"
	s := newSettings(nil, cfg, 100, 24)
	s.tab = settingsProviders
	s, _ = s.openValueForm(s.rows()[3])
	s.inputs[0].SetValue("new-exa")
	s, _ = s.Update(settingsKey(tea.KeyEnter))
	if s.msgIsErr || s.cfg.ExaAPIKey != "new-exa" || s.cfg.APIKey != cfg.APIKey || s.cfg.DeepSeekAPIKey != cfg.DeepSeekAPIKey {
		t.Fatal("single-key save changed unrelated fields", s.msg)
	}
	s.tab = settingsRouting
	s, _ = s.openValueForm(s.rows()[1])
	s.inputs[0].SetValue("NaN")
	s, _ = s.Update(settingsKey(tea.KeyEnter))
	if !s.msgIsErr || s.mode != sValueForm || s.cfg.ConfidenceThreshold != cfg.ConfidenceThreshold {
		t.Fatal("invalid threshold should stay editable without changing config")
	}
	s, _ = s.Update(settingsKey(tea.KeyEscape))
	if s.msg != "" {
		t.Fatal("cancel should clear validation error")
	}
	s.tab = settingsRoles
	s.cfg.Roles[0].ContextWindow = 12345
	s, _ = s.openRoleForm(0)
	s.inputs[0].SetValue("renamed")
	s, _ = s.Update(settingsKey(tea.KeyEnter))
	if s.msgIsErr || s.cfg.DefaultRole != "renamed" || s.cfg.Roles[0].ContextWindow != 12345 {
		t.Fatal("editing a role should preserve default status and model limits", s.msg)
	}
	s, _ = s.openRoleForm(-1)
	s, _ = s.Update(settingsKey(tea.KeyEnter))
	if !s.msgIsErr || s.focus != 0 {
		t.Fatal("missing name should focus the name field")
	}
	s.inputs[0].SetValue("added")
	s.inputs[1].SetValue("openai/example")
	s.inputs[2].SetValue("Example role")
	s, _ = s.Update(settingsKey(tea.KeyEnter))
	if s.msgIsErr || s.cursor != len(s.cfg.Roles)-1 {
		t.Fatal("new role should remain selected", s.msg)
	}
}

func TestSettingsDefaultAndDeleteMenus(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	s := newSettings(nil, config.Default(), 100, 24)
	s.tab, s.rowCursor = settingsRouting, 2
	s, _ = s.Update(settingsKey(tea.KeyEnter))
	if s.mode != sDefaultRole {
		t.Fatal("default role should open a chooser")
	}
	s, _ = s.Update(settingsKey(tea.KeyDown))
	s, _ = s.Update(settingsKey(tea.KeyEnter))
	if s.mode != sList || s.cfg.DefaultRole != s.cfg.Roles[1].Name || s.tab != settingsRouting {
		t.Fatal("default chooser should save and return to Routing")
	}
	s.tab, s.cursor = settingsRoles, 0
	count := len(s.cfg.Roles)
	s, _ = s.Update(settingsKey('d'))
	if s.mode != sDeleteRole || len(s.cfg.Roles) != count {
		t.Fatal("delete should first show the selected role for confirmation")
	}
	s, _ = s.Update(settingsKey(tea.KeyEscape))
	if len(s.cfg.Roles) != count {
		t.Fatal("cancel deleted a role")
	}
	s, _ = s.Update(settingsKey('d'))
	s, _ = s.Update(settingsKey(tea.KeyEnter))
	if s.msgIsErr || s.mode != sList || len(s.cfg.Roles) != count-1 {
		t.Fatal("confirmed deletion did not save", s.msg)
	}
	s, _ = s.Update(settingsKey('d'))
	if !s.msgIsErr || s.mode != sList {
		t.Fatal("last/default role should be protected")
	}
}

func TestSettingsFrameFitsTerminal(t *testing.T) {
	cfg := config.Default()
	for i := range 40 {
		role := cfg.Roles[0]
		role.Name = fmt.Sprintf("Role %d", i)
		cfg.Roles = append(cfg.Roles, role)
	}
	for _, size := range [][2]int{{140, 50}, {80, 24}, {60, 16}, {30, 10}, {8, 4}} {
		for tab := range settingsTabs {
			for _, mode := range []sMode{sList, sHelp, sValueForm, sRoleForm, sDefaultRole, sDeleteRole} {
				s := newSettings(nil, cfg, size[0], size[1])
				s.tab, s.cursor = tab, len(cfg.Roles)-1
				switch mode {
				case sValueForm:
					s.tab = settingsProviders
					s, _ = s.openValueForm(s.rows()[3])
				case sRoleForm:
					s, _ = s.openRoleForm(s.cursor)
					s.focus = 3
				default:
					s.mode = mode
				}
				view := s.View()
				if len(strings.Split(view, "\n")) > size[1] {
					t.Fatalf("%v mode %d: too many rows", size, mode)
				}
				for _, row := range strings.Split(view, "\n") {
					if ansi.StringWidth(row) > size[0] {
						t.Fatalf("%v mode %d: row exceeds terminal width", size, mode)
					}
				}
			}
		}
	}
	s := newSettings(nil, cfg, 120, 24)
	s.tab, s.cursor = settingsRoles, len(cfg.Roles)-1
	view := ansi.Strip(s.View())
	if !strings.Contains(view, "Role 39") || !strings.Contains(view, "Esc close") {
		t.Fatal("selected role and controls should stay visible while scrolling")
	}
	s = newSettings(nil, cfg, 40, 16)
	s.mode = sHelp
	for range 100 {
		s, _ = s.Update(settingsKey(tea.KeyDown))
	}
	if !strings.Contains(ansi.Strip(s.View()), "command output") {
		t.Fatal("wrapped help should scroll to its final entry")
	}
}
