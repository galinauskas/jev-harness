package tui

import (
	"charm.land/lipgloss/v2"
	"fmt"
	"github.com/charmbracelet/x/ansi"
	"os"
	"strings"
)

// ---- view ----

func (s settingsModel) View() string {
	w := s.boxWidth()
	lines := s.headingLines(w)
	if s.mode == sList {
		lines = append(lines, s.listLines(w)...)
	} else if s.mode == sStatusForm {
		lines = append(lines, s.statusLines(w)...)
	} else if s.mode == sHelp {
		lines = append(lines, s.helpLines(w)...)
	} else {
		lines = append(lines, s.formLines(w)...)
	}
	if s.msg != "" {
		style := okStyle
		if s.msgIsErr {
			style = errStyle
		}
		if s.h > 0 && len(lines)+2 > s.h {
			inner := max(1, w-4)
			message := ansi.Truncate(style.Render(settingsText(s.msg)), inner, "…")
			lines[1] = "  " + dimStyle.Render("│") + " " + message + strings.Repeat(" ", max(0, inner-lipgloss.Width(message))) + " " + dimStyle.Render("│")
		} else {
			lines = append(lines, "", "  "+style.Render(settingsText(s.msg)))
		}
	}
	// Keep the final frame inside the terminal, including unusually small ones.
	for i := range lines {
		lines[i] = ansi.Truncate(lines[i], max(1, s.w), "…")
	}
	if s.h > 0 && len(lines) > s.h {
		lines = lines[:s.h]
	}
	return strings.Join(lines, "\n")
}

func (s settingsModel) headingLines(w int) []string {
	section := "Settings"
	switch s.mode {
	case sStatusForm:
		section = "Settings  /  Status line"
	case sHelp:
		section = "Settings  /  Help"
	case sGlobalsForm:
		section = "Settings  /  Routing"
	case sRoleForm:
		section = "Settings  /  Roles"
	}
	subtitle := "Configure routing, chat, and roles"
	if s.mode == sStatusForm {
		subtitle = "Choose what appears in the chat status line"
	}
	lines := settingsBox(w, section, []string{dimStyle.Render(subtitle)})
	if s.h <= 0 || s.h >= 20 {
		lines = append(lines, "")
	}
	return lines
}

func (s settingsModel) boxWidth() int {
	return max(8, min(132, s.w-4))
}

func (s settingsModel) inputWidth() int {
	return max(1, min(60, s.boxWidth()-4))
}

// settingsText makes config values safe to place on a single terminal row.
func settingsText(value string) string {
	return strings.Join(strings.Fields(safeText(value)), " ")
}

// settingsBox draws a heading into the top edge and pads every content row.
// width includes the border and the one-cell padding on each side.
func settingsBox(width int, heading string, content []string) []string {
	inner := max(1, width-4)
	title := "─ " + settingsText(heading) + " "
	title = ansi.Truncate(title, max(1, width-2), "…")
	top := "╭" + accent.Render(title) + dimStyle.Render(strings.Repeat("─", max(0, width-2-lipgloss.Width(title)))) + "╮"
	lines := []string{"  " + top}
	for _, row := range content {
		row = ansi.Truncate(row, inner, "…")
		lines = append(lines, "  "+dimStyle.Render("│")+" "+row+strings.Repeat(" ", max(0, inner-lipgloss.Width(row)))+" "+dimStyle.Render("│"))
	}
	lines = append(lines, "  "+dimStyle.Render("╰"+strings.Repeat("─", max(0, width-2))+"╯"))
	return lines
}

func (s settingsModel) listLines(w int) []string {
	if s.h > 0 && s.h < 24 {
		return s.compactListLines(w)
	}
	inner := w - 4
	labelWidth := min(20, max(10, inner/3))
	row := func(label, value string) string {
		return dimStyle.Render(fmt.Sprintf("%-*s", labelWidth, label)) + ansi.Truncate(value, max(1, inner-labelWidth), "…")
	}
	keyState := errStyle.Render("not set")
	if os.Getenv("OPENROUTER_API_KEY") != "" {
		keyState = okStyle.Render("env")
	} else if s.cfg.APIKey != "" {
		keyState = okStyle.Render("saved")
	}
	lines := settingsBox(w, "ROUTING  [g] edit", []string{
		row("API key", keyState),
		row("Jev model", accent.Render(settingsText(s.cfg.JevModel))),
		row("Threshold", fmt.Sprintf("%g  %s", s.cfg.ConfidenceThreshold, dimStyle.Render("below → default role"))),
	})
	lines = append(lines, "")
	inputStyle := "Rounded box"
	if s.cfg.ChatInputLines {
		inputStyle = "Horizontal rules"
	}
	lines = append(lines, settingsBox(w, "CHAT", []string{
		row("[t] Input", accent.Render(inputStyle)),
		row("[s] Status line", dimStyle.Render("Choose visible items")),
	})...)
	lines = append(lines, "")
	// Reserve room for the panels and shortcut footer. Keep the selected role
	// visible when a long list does not fit on screen.
	visible := len(s.cfg.Roles)
	if s.h > 0 {
		if s.h >= 24 {
			visible = min(visible, max(1, (s.h-20)/2))
		} else {
			visible = min(visible, max(1, (s.h-18)/2))
		}
	}
	start := max(0, min(s.cursor-visible/2, len(s.cfg.Roles)-visible))
	roles := make([]string, 0, visible*2+1)
	for i := start; i < start+visible; i++ {
		r := s.cfg.Roles[i]
		marker := "  "
		nameStyle := boldStyle
		if i == s.cursor {
			marker = okStyle.Render("▸ ")
			nameStyle = okStyle.Bold(true)
		}
		star := "  "
		if r.Name == s.cfg.DefaultRole {
			star = warnStyle.Render("★ ")
		}
		name := ansi.Truncate(settingsText(r.Name), max(1, inner/2-4), "…")
		left := marker + star + nameStyle.Render(name)
		modelWidth := min(max(8, inner/2), max(1, inner-lipgloss.Width(left)-1))
		model := accent.Render(ansi.Truncate(settingsText(r.Model), modelWidth, "…"))
		gap := max(1, inner-lipgloss.Width(left)-lipgloss.Width(model))
		roles = append(roles, left+strings.Repeat(" ", gap)+model)
		roles = append(roles, "    "+dimStyle.Render(ansi.Truncate(settingsText(r.Description), max(1, inner-4), "…")))
	}
	if visible < len(s.cfg.Roles) && s.h < 24 {
		roles = append(roles, dimStyle.Render(fmt.Sprintf("    Showing %d–%d of %d roles", start+1, start+visible, len(s.cfg.Roles))))
	}
	lines = append(lines, settingsBox(w, "ROLES", roles)...)
	lines = append(lines, "", "  "+dimStyle.Render("↑↓ select role    Enter edit    ? help    Esc back"), "")
	return lines
}

func (s settingsModel) compactListLines(w int) []string {
	inputStyle := "Rounded box"
	if s.cfg.ChatInputLines {
		inputStyle = "Horizontal rules"
	}
	lines := []string{"  " + dimStyle.Render("[g] Routing  ·  [t] Input: ") + accent.Render(inputStyle) + dimStyle.Render("  ·  [s] Status"), ""}
	if len(s.cfg.Roles) > 0 {
		i := max(0, min(s.cursor, len(s.cfg.Roles)-1))
		role := s.cfg.Roles[i]
		marker := ""
		if role.Name == s.cfg.DefaultRole {
			marker = "★ "
		}
		rows := []string{
			okStyle.Render("▸ ") + marker + settingsText(role.Name) + "  " + dimStyle.Render(settingsText(role.Model)),
			"  " + dimStyle.Render(settingsText(role.Description)),
		}
		lines = append(lines, settingsBox(w, "ROLES", rows)...)
	}
	lines = append(lines, "  "+dimStyle.Render("↑↓ role   Enter edit   ? help   Esc back"))
	return lines
}

func (s settingsModel) statusLines(w int) []string {
	hidden := []bool{
		s.cfg.StatusLine.HideModel,
		s.cfg.StatusLine.HideTokens,
		s.cfg.StatusLine.HideContext,
		s.cfg.StatusLine.HideCost,
	}
	labels := []string{"Mode and model", "Token counts", "Context usage", "Cost"}
	rows := make([]string, len(labels))
	for i, label := range labels {
		marker := "  "
		if i == s.statusCursor {
			marker = okStyle.Render("▸ ")
		}
		state := okStyle.Render("On")
		if hidden[i] {
			state = dimStyle.Render("Off")
		}
		rows[i] = marker + label + "  " + state
	}
	lines := settingsBox(w, "STATUS LINE", rows)
	if s.h <= 0 || s.h >= 20 {
		lines = append(lines, "", "  "+dimStyle.Render("↑↓ select    Enter toggle    Esc settings"), "")
	}
	return lines
}

func (s settingsModel) helpLines(w int) []string {
	lines := settingsBox(w, "ROLE ACTIONS", []string{
		"↑ / ↓    Select a role",
		"Enter    Edit selected role",
		"n        Add a role",
		"d        Delete selected role",
		"D        Make selected role the default",
	})
	lines = append(lines, "")
	lines = append(lines, settingsBox(w, "OTHER SETTINGS", []string{
		"g        Routing and API key",
		"t        Toggle chat input style",
		"s        Choose status line items",
	})...)
	lines = append(lines, "", "  "+dimStyle.Render("Esc back"))
	return lines
}

func (s settingsModel) formLines(w int) []string {
	title := "NEW ROLE"
	labels := []string{"Name", "Model ID", "Routing description"}
	if s.mode == sGlobalsForm {
		title = "ROUTING SETTINGS"
		labels = []string{"Jev model ID", "Confidence threshold (0–1)", "OpenRouter API key"}
	} else if s.editingIdx >= 0 {
		title = "EDIT ROLE · " + settingsText(s.cfg.Roles[s.editingIdx].Name)
	}
	content := make([]string, 0, len(s.inputs)*3)
	for i := range s.inputs {
		label := dimStyle.Render(labels[i])
		if i == s.focus {
			label = okStyle.Render("▸ " + labels[i])
		}
		content = append(content, label, s.inputs[i].View())
		if i < len(s.inputs)-1 {
			content = append(content, "")
		}
	}
	lines := settingsBox(w, title, content)
	lines = append(lines, "", "  "+dimStyle.Render("tab / shift+tab move  ·  enter save  ·  esc cancel"))
	return lines
}
