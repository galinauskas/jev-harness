package tui

import (
	"fmt"
	"sort"
	"strings"

	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/table"

	"jevharness/internal/openrouter"
	"jevharness/internal/session"
)

func (c *chatModel) recordUsage(model string, usage *openrouter.Usage) {
	if usage == nil {
		if c.models == nil {
			c.models = map[string]session.ModelUsage{}
		}
		u := c.models[model]
		u.Requests++
		u.UnknownCost++
		c.models[model] = u
		return
	}
	if model == "" {
		model = "Unknown model"
	}
	if c.models == nil {
		c.models = make(map[string]session.ModelUsage)
	}
	u := c.models[model]
	u.Requests++
	if !usage.CostKnown {
		u.UnknownCost++
	}
	u.TokensIn += usage.PromptTokens
	u.TokensOut += usage.CompletionTokens
	u.Cost += usage.Cost
	c.models[model] = u
	c.tokensIn += usage.PromptTokens
	c.tokensOut += usage.CompletionTokens
	c.cost += usage.Cost
}

func (c chatModel) sessionStats() string {
	var b strings.Builder
	b.WriteString("\n" + boldStyle.Render("Session stats") + "\n")
	if c.sessionTitle != "" {
		fmt.Fprintf(&b, "  %s\n", safeText(c.sessionTitle))
	}
	width := c.w - 4
	if c.w <= 0 {
		width = 100
	}
	width = max(1, width)
	summary := [][]string{
		{"Session ID", safeText(c.sessionID)},
		{"Completed turns", fmt.Sprint(c.turns)},
		{"Total tokens", fmt.Sprint(c.tokensIn + c.tokensOut)},
		{"Input tokens", fmt.Sprint(c.tokensIn)},
		{"Output tokens", fmt.Sprint(c.tokensOut)},
		{"Reported cost", fmt.Sprintf("$%.6f", c.cost)},
	}
	if c.contextModel != "" {
		summary = append(summary,
			[]string{"Last prompt tokens", fmt.Sprint(c.contextUsed)},
			[]string{"Last prompt model", safeText(c.contextModel)},
		)
		if window := c.contextLengths[roleContextKey(c.activeRole())]; window > 0 && c.activeModel() == c.contextModel {
			summary = append(summary, []string{"Context usage", fmt.Sprintf("%.1f%% of %d", 100*float64(c.contextUsed)/float64(window), window)})
		}
	}
	b.WriteString(renderStatsTable(width, []string{"Metric", "Value"}, summary, false) + "\n\n")
	b.WriteString("  " + boldStyle.Render("Models used") + "\n")

	names := make([]string, 0, len(c.models))
	for name := range c.models {
		names = append(names, name)
	}
	sort.Strings(names)
	var attributedIn, attributedOut int
	var attributedCost float64
	rows := make([][]string, 0, len(names))
	var costNotes []string
	for _, name := range names {
		u := c.models[name]
		rows = append(rows, []string{safeText(name), fmt.Sprint(u.Requests), fmt.Sprint(u.TokensIn), fmt.Sprint(u.TokensOut), fmt.Sprintf("$%.6f", u.Cost)})
		if u.UnknownCost > 0 {
			costNotes = append(costNotes, fmt.Sprintf("%s: cost unavailable for %d requests", safeText(name), u.UnknownCost))
		}
		attributedIn += u.TokensIn
		attributedOut += u.TokensOut
		attributedCost += u.Cost
	}
	if len(rows) > 0 {
		if width >= 70 {
			b.WriteString(renderStatsTable(width, []string{"Model", "Requests", "Input", "Output", "Reported cost"}, rows, true) + "\n")
		} else {
			for _, row := range rows {
				b.WriteString("  " + accent.Width(width).Render(row[0]) + "\n")
				b.WriteString(renderStatsTable(width, []string{"Metric", "Value"}, [][]string{
					{"Reported requests", row[1]}, {"Input tokens", row[2]}, {"Output tokens", row[3]}, {"Reported cost", row[4]},
				}, true) + "\n")
			}
		}
	}
	for _, note := range costNotes {
		b.WriteString("  " + dimStyle.Width(width).Render(note) + "\n")
	}
	if len(names) == 0 {
		b.WriteString("    No model usage recorded.\n")
	}
	if c.tokensIn > attributedIn || c.tokensOut > attributedOut || c.cost > attributedCost+0.000000001 {
		b.WriteString("  Older usage totals have no per-model breakdown.\n")
	}
	b.WriteString(lipgloss.NewStyle().MarginLeft(2).Render(dimStyle.Width(width).Render("Includes reported routing, chat, and compaction usage; unavailable usage is excluded.")) + "\n\n")
	return b.String()
}

// renderStatsTable fits cells to the transcript width and aligns numeric columns.
func renderStatsTable(width int, headers []string, rows [][]string, numeric bool) string {
	t := table.New().
		Headers(headers...).
		Rows(rows...).
		Border(lipgloss.RoundedBorder()).
		BorderStyle(dimStyle).
		StyleFunc(func(row, col int) lipgloss.Style {
			style := lipgloss.NewStyle().Padding(0, 1)
			if row == table.HeaderRow {
				return style.Bold(true).Foreground(lipgloss.Color("81"))
			}
			if numeric && col > 0 {
				return style.Align(lipgloss.Right)
			}
			return style
		})
	if lipgloss.Width(t.String()) > width {
		t.Width(width)
	}
	return lipgloss.NewStyle().MarginLeft(2).Render(t.String())
}
