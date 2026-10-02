package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/table"
	"github.com/charmbracelet/x/ansi"
)

func (c chatModel) commandWidth() int {
	if c.w <= 0 {
		return 96
	}
	return max(1, c.w-4)
}

// Command tables wrap rather than discard data, including paths and diff lines.
func commandCellStyle(row, col int, numeric bool) lipgloss.Style {
	style := lipgloss.NewStyle().Padding(0, 1)
	if row == table.HeaderRow {
		return style.Bold(true).Foreground(lipgloss.Color("81"))
	}
	if numeric && col > 0 {
		return style.Align(lipgloss.Right)
	}
	return style
}

func newCommandTable(width int, headers []string, rows [][]string, numeric bool) *table.Table {
	clean := make([][]string, len(rows))
	for i, row := range rows {
		clean[i] = make([]string, len(row))
		for j, cell := range row {
			clean[i][j] = strings.ReplaceAll(safeText(cell), "\t", "    ")
		}
	}
	t := table.New().Headers(headers...).Rows(clean...).
		Border(lipgloss.RoundedBorder()).BorderStyle(dimStyle).
		StyleFunc(func(row, col int) lipgloss.Style { return commandCellStyle(row, col, numeric) })
	if lipgloss.Width(t.String()) > width {
		t.Width(max(1, width))
	}
	return t
}

func renderCommandTable(width int, headers []string, rows [][]string, numeric bool) string {
	return lipgloss.NewStyle().MarginLeft(2).Render(newCommandTable(width, headers, rows, numeric).String())
}

func (c chatModel) commandTable(title string, headers []string, rows [][]string) string {
	return "\n" + c.commandHeading(title) + "\n" +
		renderCommandTable(c.commandWidth(), headers, rows, false) + "\n"
}

func (c chatModel) commandHeading(title string) string {
	return lipgloss.NewStyle().MarginLeft(2).Render(boldStyle.Render(ansi.Wrap(safeText(title), c.commandWidth(), "")))
}

func (c *chatModel) commandFeedback(command string) {
	if c.status == "" {
		return
	}
	result := "Result"
	if c.statusIsErr {
		result = "Error"
	}
	c.appendTranscript(c.commandTable(command, []string{result}, [][]string{{c.status}}))
}

func (c chatModel) rolesTable() string {
	var rows [][]string
	for _, role := range c.cfg.Roles {
		var selection []string
		if role.Name == c.cfg.DefaultRole {
			selection = append(selection, "Default")
		}
		if role.Name == c.ag.Pinned {
			selection = append(selection, "Pinned")
		}
		state := strings.Join(selection, ", ")
		if state == "" {
			state = "—"
		}
		rows = append(rows, []string{role.Name, role.Backend(), role.Model, state, role.Description})
	}
	headers := []string{"Role", "Provider", "Model", "State", "Use for"}
	if c.commandWidth() >= 90 {
		stateWidth := len("State")
		for _, row := range rows {
			stateWidth = max(stateWidth, lipgloss.Width(row[3]))
		}
		t := newCommandTable(c.commandWidth(), headers, rows, false).
			Width(c.commandWidth()).StyleFunc(func(row, col int) lipgloss.Style {
			style := commandCellStyle(row, col, false)
			if col == 3 {
				return style.Width(stateWidth + 2)
			}
			return style
		})
		return "\n" + c.commandHeading("Available roles") + "\n" + lipgloss.NewStyle().MarginLeft(2).Render(t.String()) + "\n"
	}
	var b strings.Builder
	for _, row := range rows {
		fields := make([][]string, len(headers))
		for j, header := range headers {
			fields[j] = []string{header, row[j]}
		}
		b.WriteString(c.commandTable(fmt.Sprintf("Role: %s", row[0]), []string{"Field", "Value"}, fields))
	}
	return b.String()
}
