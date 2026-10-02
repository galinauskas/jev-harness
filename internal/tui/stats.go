package tui

import (
	"fmt"
	"sort"
	"strings"

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
	b.WriteString("\n" + c.commandHeading("Session stats") + "\n")

	width := c.commandWidth()
	summary := [][]string{
		{"Title", c.sessionTitle},
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
	b.WriteString(renderCommandTable(width, []string{"Metric", "Value"}, summary, false) + "\n\n")
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
			b.WriteString(renderCommandTable(width, []string{"Model", "Requests", "Input", "Output", "Reported cost"}, rows, true) + "\n")
		} else {
			for _, row := range rows {
				b.WriteString(c.commandHeading(row[0]) + "\n")
				b.WriteString(renderCommandTable(width, []string{"Metric", "Value"}, [][]string{
					{"Reported requests", row[1]}, {"Input tokens", row[2]}, {"Output tokens", row[3]}, {"Reported cost", row[4]},
				}, true) + "\n")
			}
		}
	}
	notes := make([][]string, 0, len(costNotes)+2)
	for _, note := range costNotes {
		notes = append(notes, []string{note})
	}
	if len(names) == 0 {
		b.WriteString(renderCommandTable(width, []string{"Usage"}, [][]string{{"No model usage recorded."}}, false) + "\n")
	}
	if c.tokensIn > attributedIn || c.tokensOut > attributedOut || c.cost > attributedCost+0.000000001 {
		notes = append(notes, []string{"Older usage totals have no per-model breakdown."})
	}
	notes = append(notes, []string{"Includes reported routing, chat, and compaction usage; unavailable usage is excluded."})
	b.WriteString(renderCommandTable(width, []string{"Accounting notes"}, notes, false) + "\n\n")
	return b.String()
}
