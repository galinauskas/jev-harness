package tui

import (
	"fmt"
	"os"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

var settingsSelected = lipgloss.NewStyle().Foreground(lipgloss.Color("255")).Background(lipgloss.Color("31")).Bold(true)
var settingsTabs = []string{"Appearance", "Routing", "Context", "Providers", "Roles"}

type settingRow struct{ section, label, value, hint, action string }

func (s settingsModel) rows() []settingRow {
	switch s.tab {
	case settingsRouting:
		return []settingRow{
			{"Model", "Routing model", settingsText(s.cfg.JevModel), "Model Jev uses to choose a role for each request.", "model"},
			{"Fallback", "Confidence threshold", fmt.Sprintf("%g", s.cfg.ConfidenceThreshold), "Use the default role below this confidence. Enter a number from 0 to 1.", "threshold"},
			{"Fallback", "Default role", settingsText(s.cfg.DefaultRole), "Role used when routing confidence is below the threshold. Enter to choose.", "default"},
		}
	case settingsContext:
		return []settingRow{{"Compaction", "Compaction threshold", s.compactionLabel(), "Summarise earlier context at this percentage; 0 disables compaction.", "c"}}
	case settingsProviders:
		rows := []settingRow{
			{"API keys", "OpenRouter API key", keyStatus(s.cfg.APIKey, "OPENROUTER_API_KEY"), "Saved key overrides OPENROUTER_API_KEY. Leave blank to use the environment key.", "key0"},
			{"API keys", "DeepSeek API key", keyStatus(s.cfg.DeepSeekAPIKey, "DEEPSEEK_API_KEY"), "Saved key overrides DEEPSEEK_API_KEY. Leave blank to use the environment key.", "key1"},
			{"API keys", "OpenCode Go API key", keyStatus(s.cfg.OpenCodeGoAPIKey, "OPENCODE_GO_API_KEY"), "Saved key overrides OPENCODE_GO_API_KEY. Leave blank to use the environment key.", "key2"},
			{"Web search", "Exa API key", s.searchPolicyLabel("exa") + keyStatus(s.cfg.ExaAPIKey, "EXA_API_KEY"), "Leave blank to use EXA_API_KEY. Enable Exa below to use it for search.", "key3"},
			{"Web search", "Brave API key", s.searchPolicyLabel("brave") + keyStatus(s.cfg.BraveAPIKey, "BRAVE_API_KEY"), "Leave blank to use BRAVE_API_KEY. Enable Brave below to use it for search.", "key4"},
			{"Web search", "Search provider", s.cfg.WebSearchProvider(), "Choose Exa or Brave for web_search. The selected provider needs a key and must be enabled below.", "search-provider"},
		}
		scope := "Enabled providers"
		if s.cwd != "" {
			scope += " (this project)"
		}
		for _, p := range []struct{ id, label, hint string }{
			{"openrouter", "OpenRouter", "Enable OpenRouter models and automatic routing. Off: use an enabled default role."},
			{"deepseek", "DeepSeek", "Enable roles that use DeepSeek."},
			{"opencode-go", "OpenCode Go", "Enable roles that use OpenCode Go."},
			{"exa", "Exa", "Enable Exa web search when selected above."},
			{"brave", "Brave", "Enable Brave web search when selected above."},
		} {
			rows = append(rows, settingRow{scope, p.label, settingOn(s.cfg.ProviderAllowed(s.cwd, p.id)), p.hint, "provider:" + p.id})
		}
		rows = append(rows, settingRow{scope, "Restore providers", "Enable all", "Restore all providers for this project, including restrictions saved by older versions.", "restore-providers"})
		return rows
	case settingsRoles:
		rows := make([]settingRow, 0, len(s.cfg.Roles))
		for _, role := range s.cfg.Roles {
			label := settingsText(role.Name)
			if role.Name == s.cfg.DefaultRole {
				label += " (default)"
			}
			rows = append(rows, settingRow{"Configured roles", label, settingsText(role.Backend() + " · " + role.Model), settingsText(role.Description), "role"})
		}
		return rows
	default:
		input := "Rounded box"
		if s.cfg.ChatInputLines {
			input = "Horizontal rules"
		}
		return []settingRow{
			{"Input", "Chat input style", input, "Change the border around the chat input.", "t"},
			{"Status line", "Model", settingOn(!s.cfg.StatusLine.HideModel), "Show the active mode and model below chat.", "status0"},
			{"Status line", "Tokens", settingOn(!s.cfg.StatusLine.HideTokens), "Show token counts below chat.", "status1"},
			{"Status line", "Context usage", settingOn(!s.cfg.StatusLine.HideContext), "Show how much of the model context is in use.", "status2"},
			{"Status line", "Cost", settingOn(!s.cfg.StatusLine.HideCost), "Show request cost below chat.", "status3"},
			{"Experimental", "Docker sandbox (experimental)", settingOn(s.cfg.Safety.DockerSandbox), "Off: local shell with host/network access. On: offline Docker sandbox; requires sandbox build.", "x"},
			{"Command output", "Command output", s.commandOutputLabel(), "Keep command results short and save full logs for later retrieval.", "b"},
		}
	}
}

func settingsText(value string) string  { return strings.Join(strings.Fields(safeText(value)), " ") }
func (s settingsModel) boxWidth() int   { return max(4, s.w-2) }
func (s settingsModel) inputWidth() int { return max(1, min(60, s.boxWidth()-s.sidebarWidth()-8)) }
func (s settingsModel) sidebarWidth() int {
	if s.w < 65 || s.tab == settingsContext || s.tab == settingsRoles || s.mode != sList {
		return 0
	}
	return min(24, max(16, s.w/5))
}
func settingsPad(value string, width int) string {
	value = ansi.Truncate(value, max(0, width), "…")
	return value + strings.Repeat(" ", max(0, width-lipgloss.Width(value)))
}

func (s settingsModel) View() string {
	w := s.boxWidth()
	inner := max(1, w-2)
	h := s.h
	if h <= 0 {
		h = 30
	}
	// The title, tabs, separators, description and footer share one frame.
	hintHeight := 1
	if h >= 16 {
		hintHeight = 2
	}
	bodyHeight := max(1, h-8-hintHeight)
	side := s.sidebarWidth()
	contentWidth := max(1, inner-side-4)
	rows := s.rows()
	selected := s.rowCursor
	if s.tab == settingsRoles {
		selected = s.cursor
	}
	selected = max(0, min(selected, len(rows)-1))
	section, hint := "", ""
	if len(rows) > 0 {
		section, hint = rows[selected].section, rows[selected].hint
	}
	footer := s.listFooter(inner)
	var content []string
	var sections []string
	switch s.mode {
	case sRoleForm, sValueForm:
		labels := []string{"Name", "Model ID", "When to use this role", "Provider"}
		hints := []string{
			"Unique name: 1–32 lowercase letters, digits, _ or -.",
			"OpenRouter: provider/model. Other providers: their model ID.",
			"Describe tasks this role handles; Jev uses this to pick a role.",
			"Use ←/→ or Space to choose OpenRouter, DeepSeek or OpenCode Go.",
		}
		section = "Edit role"
		if s.editingIdx < 0 {
			section = "Add role"
		} else if s.mode == sRoleForm {
			section += ": " + settingsText(s.cfg.Roles[s.editingIdx].Name)
		}
		footer = "Tab/Shift+Tab fields · Enter save · Esc cancel"
		if s.mode == sValueForm {
			labels = []string{s.editRow.label}
			hints = []string{s.editRow.hint}
			section = s.editRow.section
			footer = "Enter save · Esc cancel"
		}
		hint = hints[s.focus]
		for i := range s.inputs {
			label := dimStyle.Render(labels[i])
			if i == s.focus {
				label = accent.Bold(true).Render(labels[i])
			}
			value := s.inputs[i].View()
			if s.mode == sRoleForm && i == 3 {
				choices := make([]string, len(roleProviders))
				for j, provider := range roleProviders {
					choices[j] = dimStyle.Render(provider)
					if provider == s.inputs[3].Value() {
						choices[j] = accent.Bold(true).Render("[" + provider + "]")
					}
				}
				value = strings.Join(choices, "  ")
				if lipgloss.Width(value) > contentWidth {
					value = accent.Bold(true).Render("["+s.inputs[3].Value()+"]") + dimStyle.Render("  ←/→ choose")
				}
				if s.focus == 3 {
					footer = "←→ provider · Tab fields · Enter save · Esc cancel"
				}
			}
			content = append(content, label, value, "")
		}
	case sSearchProvider:
		section, hint = "Search provider", "Choose which provider handles web_search."
		footer = "↑↓ or Tab select · Enter save · Esc cancel"
		for i, provider := range []string{"exa", "brave"} {
			label := provider
			if provider == s.cfg.WebSearchProvider() {
				label += " (current)"
			}
			content = append(content, settingsRow(label, "", contentWidth, i == s.choiceCursor))
		}
	case sDefaultRole:
		section, hint = "Default role", "Use this role when routing confidence is below the threshold."
		footer = "↑↓ or Tab select · Enter save · Esc cancel"
		for i, role := range s.cfg.Roles {
			label := settingsText(role.Name)
			if role.Name == s.cfg.DefaultRole {
				label += " (current)"
			}
			content = append(content, settingsRow(label, settingsText(role.Model), contentWidth, i == s.choiceCursor))
		}
	case sDeleteRole:
		section = "Delete role"
		hint = "Enter confirms deletion. Esc keeps the role."
		footer = "Enter delete · Esc cancel"
		content = []string{"Delete " + settingsText(s.cfg.Roles[s.cursor].Name) + "?", "", "This removes the role from routing."}
	case sHelp:
		section = "Keyboard controls"
		footer, hint = "↑↓ scroll · Esc or ? back", "Toggles save now. Enter saves edits; Esc cancels."
		content = settingsHelpLines(contentWidth)
	default:
		for i, row := range rows {
			if i == 0 || row.section != rows[i-1].section {
				sections = append(sections, row.section)
				content = append(content, dimStyle.Underline(true).Bold(true).Render(row.section))
			}
			content = append(content, settingsRow(row.label, row.value, contentWidth, i == selected))
		}
	}
	if s.mode != sList {
		content = append([]string{dimStyle.Underline(true).Bold(true).Render(section)}, content...)
	}
	// Scroll the active row/field into view, keeping the footer fixed.
	active := 0
	if s.mode == sList {
		for i := 0; i <= selected && i < len(rows); i++ {
			if i == 0 || rows[i].section != rows[i-1].section {
				active++
			}
			if i < selected {
				active++
			}
		}
	} else if s.mode == sRoleForm || s.mode == sValueForm {
		active = s.focus*3 + 2
	} else if s.mode == sDefaultRole || s.mode == sSearchProvider {
		active = s.choiceCursor + 1
	} else if s.mode == sHelp {
		active = min(s.helpCursor+1, len(content)-1)
	}
	start := max(0, min(active-bodyHeight+1, len(content)-bodyHeight))
	border := func(left, right string) string { return dimStyle.Render(left + strings.Repeat("─", inner) + right) }
	frameRow := func(row string) string {
		return " " + dimStyle.Render("│") + settingsPad(row, inner) + dimStyle.Render("│")
	}
	title := " Settings "
	lines := []string{" " + dimStyle.Render("╭") + accent.Bold(true).Render(title) + dimStyle.Render(strings.Repeat("─", max(0, inner-lipgloss.Width(title)))) + "╮"}
	tabs := make([]string, len(settingsTabs))
	for i, tab := range settingsTabs {
		tabs[i] = dimStyle.Render(" " + tab + " ")
		if i == s.tab {
			tabs[i] = settingsSelected.Render(" " + tab + " ")
		}
	}
	tabLine := strings.Join(tabs, "  ")
	if lipgloss.Width(tabLine) > inner {
		tabLine = tabs[s.tab] + dimStyle.Render(fmt.Sprintf("  %d/%d", s.tab+1, len(settingsTabs)))
		if s.mode == sList {
			tabLine += dimStyle.Render("  ←→ tabs")
		}
	}
	lines = append(lines, frameRow(tabLine), " "+border("├", "┤"))
	for i := 0; i < bodyHeight; i++ {
		right := ""
		if start+i < len(content) {
			right = content[start+i]
		}
		row := " "
		if side > 0 {
			left := ""
			if i < len(sections) {
				left = dimStyle.Render(sections[i])
				if sections[i] == section {
					left = accent.Bold(true).Render(sections[i])
				}
			}
			row += settingsPad(left, side-1) + dimStyle.Render("│") + "  "
		} else {
			row += " "
		}
		row += settingsPad(right, contentWidth)
		scroll := " "
		if len(content) > bodyHeight {
			scroll = dimStyle.Render("│")
			if i == start*(bodyHeight-1)/max(1, len(content)-bodyHeight) {
				scroll = accent.Render("┃")
			}
		}
		lines = append(lines, frameRow(settingsPad(row, inner-1)+scroll))
	}
	if s.msg != "" {
		hint = settingsText(s.msg)
		if s.msgIsErr {
			hint = errStyle.Render(hint)
		} else {
			hint = okStyle.Render(hint)
		}
	} else {
		hint = dimStyle.Render(hint)
	}
	if inner < 52 && s.mode != sList {
		footer = "Enter save · Esc cancel"
		if s.mode == sDeleteRole {
			footer = "Enter delete · Esc cancel"
		}
		if s.mode == sHelp {
			footer = "↑↓ scroll · Esc back"
		}
	}
	lines = append(lines, frameRow(""))
	hintLines := strings.Split(ansi.Wrap(hint, max(1, inner-2), " "), "\n")
	for i := 0; i < hintHeight; i++ {
		line := ""
		if i < len(hintLines) {
			line = hintLines[i]
		}
		lines = append(lines, frameRow(" "+line))
	}
	lines = append(lines, " "+border("├", "┤"), frameRow(" "+dimStyle.Render(footer)), " "+border("╰", "╯"))
	for i := range lines {
		lines[i] = ansi.Truncate(lines[i], max(1, s.w), "")
	}
	if len(lines) > h {
		lines = lines[:h]
	}
	return strings.Join(lines, "\n")
}

func settingsRow(label, value string, width int, selected bool) string {
	labelWidth := min(38, max(10, width/2))
	row := settingsPad(settingsText(label), labelWidth) + "  " + dimStyle.Render(value)
	row = settingsPad(row, width)
	if selected {
		return settingsSelected.Render(ansi.Strip(row))
	}
	return row
}

func (s settingsModel) compactionLabel() string {
	if s.cfg.CompactionThreshold == 0 {
		return dimStyle.Render("Off")
	}
	return accent.Render(fmt.Sprintf("%d%% of model context", s.cfg.CompactionThreshold))
}

func (s settingsModel) commandOutputLabel() string {
	if s.cfg.CompactCommandOutput {
		return okStyle.Render("Compact")
	}
	return dimStyle.Render("Standard")
}

func keyStatus(saved, env string) string {
	if strings.TrimSpace(saved) != "" {
		return okStyle.Render("Saved key")
	}
	if strings.TrimSpace(os.Getenv(env)) != "" {
		return okStyle.Render("Environment")
	}
	return dimStyle.Render("Not configured")
}

func (s settingsModel) searchPolicyLabel(provider string) string {
	allowed := s.cfg.ProviderAllowed(s.cwd, provider)
	if !allowed {
		return warnStyle.Render("Disabled in settings · ")
	}
	return ""
}

func settingOn(on bool) string {
	if on {
		return "On"
	}
	return "Off"
}

var settingsHelp = []string{
	"↑/↓ or j/k   Select a setting or role",
	"←/→          Switch tabs (remembers selection)",
	"Tab/Shift+Tab Jump sections; move rows on single-section pages",
	"Enter/Space  Edit a value or toggle a setting",
	"n / d / D    Add / delete / set default in Roles",
	"Tab/Shift+Tab Move between fields in an edit form",
	"←/→ or Space Choose a role provider in an edit form",
	"Enter        Save an edit or confirm a choice",
	"Esc          Cancel an edit; close settings from the list",
	"g / c / s    Routing model / compaction / status line",
	"t / b / x    Toggle input style / command output / experimental Docker sandbox",
}

func settingsHelpLines(width int) []string {
	var lines []string
	for _, line := range settingsHelp {
		lines = append(lines, strings.Split(ansi.Wrap(line, width, " "), "\n")...)
	}
	return lines
}

func (s settingsModel) listFooter(width int) string {
	if s.tab == settingsRoles {
		if width < 36 {
			return "Enter edit · Esc"
		}
		if width < 52 {
			return "Enter edit · n add · ? help · Esc"
		}
		if width < 90 {
			return "Enter edit · n add · d delete · D default · ? help · Esc close"
		}
		return "↑↓ select · Enter edit · n add · d delete · D default · ←→ tabs · ? help · Esc close"
	}
	action := "edit"
	rows := s.rows()
	if len(rows) > 0 {
		row := rows[s.selectedRow()]
		if row.action == "restore-providers" {
			action = "restore"
		}
		if row.action == "default" || row.action == "search-provider" {
			action = "choose"
		}
		if row.action == "t" || row.action == "b" || row.action == "x" || strings.HasPrefix(row.action, "status") || strings.HasPrefix(row.action, "provider:") {
			action = "toggle"
		}
	}
	if width < 36 {
		return "Enter " + action + " · Esc"
	}
	if width < 52 {
		return "Enter " + action + " · ? help · Esc close"
	}
	if width < 90 {
		return "↑↓ select · Enter " + action + " · ←→ tabs · ? help · Esc close"
	}
	footer := "↑↓ select · Enter/Space " + action
	if s.tab != settingsContext {
		footer += " · Tab section"
	}
	return footer + " · ←→ tabs · ? help · Esc close"
}
