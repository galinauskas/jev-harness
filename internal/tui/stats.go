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
	fmt.Fprintf(&b, "  Completed turns: %d\n", c.turns)
	fmt.Fprintf(&b, "  Tokens: %d total · %d input · %d output\n", c.tokensIn+c.tokensOut, c.tokensIn, c.tokensOut)
	fmt.Fprintf(&b, "  Reported cost: $%.6f\n", c.cost)
	if c.contextModel != "" {
		fmt.Fprintf(&b, "  Last prompt: %d tokens · %s", c.contextUsed, safeText(c.contextModel))
		if window := c.contextLengths[roleContextKey(c.activeRole())]; window > 0 && c.activeModel() == c.contextModel {
			fmt.Fprintf(&b, " · %.1f%% of %d", 100*float64(c.contextUsed)/float64(window), window)
		}
		b.WriteString("\n")
	}
	b.WriteString("  Models used:\n")
	names := make([]string, 0, len(c.models))
	for name := range c.models {
		names = append(names, name)
	}
	sort.Strings(names)
	var attributedIn, attributedOut int
	var attributedCost float64
	for _, name := range names {
		u := c.models[name]
		fmt.Fprintf(&b, "    %s\n      %d reported requests · %d input · %d output · $%.6f\n", safeText(name), u.Requests, u.TokensIn, u.TokensOut, u.Cost)
		attributedIn += u.TokensIn
		attributedOut += u.TokensOut
		attributedCost += u.Cost
	}
	if len(names) == 0 {
		b.WriteString("    No model usage recorded.\n")
	}
	if c.tokensIn > attributedIn || c.tokensOut > attributedOut || c.cost > attributedCost+0.000000001 {
		b.WriteString("  Older usage totals have no per-model breakdown.\n")
	}
	b.WriteString(dimStyle.Render("  Includes reported routing, chat, and compaction usage; unavailable usage is excluded.") + "\n\n")
	return b.String()
}
