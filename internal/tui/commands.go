package tui

import (
	"strings"
	"unicode"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

type commandSuggestion struct {
	name, description string
}

var slashCommands = []commandSuggestion{
	{"/fork", "Fork the conversation and staged files into a new session"},
	{"/mode", "inspect, develop or autonomous; original files change only with /apply"},
	{"/changes", "Review staged file changes"},
	{"/apply", "Apply reviewed changes; optional path"},
	{"/undo", "Restore an applied file without overwriting later edits"},
	{"/discard", "Discard staged changes and refresh from project"},
	{"/attach", "Attach a workspace file to your draft"},
	{"/compact", "Summarise context now"},
	{"/recover", "Acknowledge an interrupted session after review"},
	{"/providers", "View or set permitted providers for this project"},
	{"/name", "Rename the current session"},
	{"/doctor", "Check sandbox availability"},
	{"/settings", "Edit configuration"},
	{"/session", "Open saved sessions; /session new starts a new one"},
	{"/stats", "Show session tokens, cost, models, and context usage"},
	{"/clear", "Start a new session"},
	{"/role", "Pin a role: /role <name>|auto"},
	{"/roles", "List available roles"},
	{"/yolo", "Toggle automatic tool approval"},
	{"/quit", "Save and quit"},
}

func (c chatModel) commandSuggestions() []commandSuggestion {
	if c.running() || c.approval != nil || c.sessionPicker || c.commandDismissed {
		return nil
	}
	prefix := strings.TrimLeftFunc(c.ta.Value(), unicode.IsSpace)
	if !strings.HasPrefix(prefix, "/") || strings.ContainsFunc(prefix, unicode.IsSpace) {
		return nil
	}
	var matches []commandSuggestion
	for _, command := range slashCommands {
		if strings.HasPrefix(command.name, prefix) {
			matches = append(matches, command)
		}
	}
	return matches
}

func (c *chatModel) syncCommandInput(oldValue string) {
	if c.ta.Value() != oldValue {
		c.commandCursor = 0
		c.commandDismissed = false
	}
}

func (c chatModel) commandSuggestionsView() string {
	matches := c.commandSuggestions()
	if len(matches) == 0 {
		return ""
	}
	// Leave room for the input, status, and at least one transcript row.
	limit := min(len(matches), max(1, c.h-c.ta.Height()-8))
	cursor := min(c.commandCursor, len(matches)-1)
	start := max(0, cursor-limit+1)
	width := max(1, c.w-4) // border and horizontal padding
	background := lipgloss.Color("236")
	rows := make([]string, 0, limit+1)
	for i := start; i < start+limit; i++ {
		command := matches[i]
		marker := "  "
		if i == cursor {
			marker = "› "
		}
		row := ansi.Truncate(marker+command.name+"  "+command.description, width, "…")
		if i == cursor {
			row = accent.Background(background).Render(row)
		} else {
			row = dimStyle.Background(background).Render(row)
		}
		rows = append(rows, row)
	}
	rows = append(rows, dimStyle.Background(background).Render(ansi.Truncate("↑/↓ select · Tab complete · Enter run/complete · Esc dismiss", width, "…")))
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("244")).
		Background(background).
		Padding(0, 1).
		Width(max(1, c.w)).
		Render(strings.Join(rows, "\n"))
}
