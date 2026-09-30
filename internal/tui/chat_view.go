package tui

import (
	"charm.land/lipgloss/v2"
	"fmt"
	"github.com/charmbracelet/x/ansi"
	"jevharness/internal/router"
	"strings"
)

// routeLine renders the routing decision for the transcript.
func routeLine(d router.Decision) string {
	var src string
	switch d.Source {
	case router.SourceDefault:
		src = "default: automatic routing disabled"
	case router.SourcePinned:
		src = "pinned"
	case router.SourceJev:
		src = fmt.Sprintf("%.2f confidence", d.Confidence)
	case router.SourceThreshold:
		src = fmt.Sprintf("default: low confidence %.2f", d.Confidence)
	case router.SourceError:
		src = fmt.Sprintf("default: jev error %v", d.Err)
	case router.SourceSingle:
		src = "single role"
	}
	model := d.Role.Model
	if model == "" {
		model = "?"
	}
	return fmt.Sprintf("→ %s (%s) %s", safeText(d.Role.Name), safeText(src), safeText(d.Role.Backend()+" · "+model))
}

func (c chatModel) View() string {
	if c.sessionPicker {
		return c.sessionsView()
	}
	return c.vp.View() + "\n" + c.thinkingLine() + "\n" + c.inputView() + "\n" + c.statsLine()
}

func (c chatModel) inputView() string {
	if c.cfg.ChatInputLines {
		width := max(1, c.w-2)
		rule := " " + lipgloss.NewStyle().Foreground(lipgloss.Color("180")).Render(strings.Repeat("─", width)) + " "
		rows := []string{rule}
		for _, row := range strings.Split(c.ta.View(), "\n") {
			rows = append(rows, "  "+row)
		}
		return strings.Join(append(rows, rule), "\n")
	}
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("244")).
		Padding(0, 1).
		Width(max(1, c.w)).
		Render(c.ta.View())
}

// thinkingLine sits above the input box, expanding for a pending approval.
func (c chatModel) thinkingLine() string {
	if c.approval != nil {
		return c.approvalView()
	}
	if !c.running() {
		return c.commandSuggestionsView()
	}
	frames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	return " " + accent.Render(frames[c.spin%len(frames)]+" thinking…")
}

func (c chatModel) approvalView() string {
	width := max(1, c.w-4) // border and horizontal padding
	yellow := lipgloss.Color("220")
	heading := lipgloss.NewStyle().Bold(true).Foreground(yellow)
	details := strings.Split(ansi.Wrap(c.approveText, width, ""), "\n")
	// Keep the input visible; the full request is also in the scrollable chat.
	limit := max(1, min(6, c.h-c.ta.Height()-9))
	if len(details) > limit {
		details = append(details[:limit], "… full request above (PgUp/PgDn)")
	}
	content := heading.Render("Approval required") + "\n" + strings.Join(details, "\n") +
		"\n" + heading.Render("Y approve · N/Enter deny · Esc abort")
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(yellow).
		Padding(0, 1).
		Width(max(1, c.w)).
		Render(content)
}

func (c chatModel) statsLine() string {
	var items []string
	if !c.cfg.StatusLine.HideModel {
		if c.ag.Pinned != "" {
			if role, ok := c.cfg.RoleByName(c.ag.Pinned); ok {
				items = append(items, accent.Render(safeText(role.Name))+dimStyle.Render(" · "+safeText(role.Model)))
			}
		} else {
			role, ok := c.cfg.RoleByName(c.cfg.DefaultRole)
			if c.lastDec != nil {
				role, ok = c.lastDec.Role, true
			}
			if ok {
				items = append(items, accent.Render("auto")+dimStyle.Render(" · "+safeText(role.Model)+" ("+safeText(role.Name)+")"))
			}
		}
	}
	if !c.cfg.StatusLine.HideTokens {
		items = append(items, dimStyle.Render(fmt.Sprintf("↑%s ↓%s", fmtTok(c.tokensIn), fmtTok(c.tokensOut))))
	}
	if !c.cfg.StatusLine.HideContext {
		model := c.activeModel()
		if model != "" {
			percent := "0%"
			limit := "?"
			if n := c.contextLengths[roleContextKey(c.activeRole())]; n > 0 {
				limit = fmtTok(n)
				if c.contextModel == model && c.contextUsed > 0 {
					percent = fmt.Sprintf("%.0f%%", 100*float64(c.contextUsed)/float64(n))
				}
			} else if c.contextModel == model && c.contextUsed > 0 {
				percent = "?%"
			}
			items = append(items, dimStyle.Render(percent+"/"+limit))
		}
	}
	if !c.cfg.StatusLine.HideCost {
		cost := fmt.Sprintf("$%.3f", c.cost)
		if c.cost > 0 && c.cost < 0.001 {
			cost = fmt.Sprintf("$%.4f", c.cost)
		}
		items = append(items, dimStyle.Render(cost))
	}
	left := strings.Join(items, dimStyle.Render(" | "))
	if c.ag.Mode() == "autonomous" {
		badge := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("16")).Background(lipgloss.Color("196")).Render(" AUTONOMOUS ")
		if left == "" {
			left = badge
		} else {
			left = badge + " " + left
		}
	}
	avail := c.w - 2 // 1-cell margin each side
	if c.status != "" {
		right := safeText(c.status)
		if c.statusIsErr {
			right = errStyle.Render(right)
		}
		right = ansi.Truncate(right, max(1, avail), "…")
		leftAvail := avail - lipgloss.Width(right) - 1
		if leftAvail < lipgloss.Width(left) {
			if leftAvail > 0 {
				left = ansi.Truncate(left, leftAvail, "…")
			} else {
				left = ""
			}
		}
		return " " + left + strings.Repeat(" ", max(0, avail-lipgloss.Width(left)-lipgloss.Width(right))) + right + " "
	}
	if lipgloss.Width(left) > avail {
		return " " + ansi.Truncate(left, max(1, avail), "…")
	}
	return " " + left + strings.Repeat(" ", max(0, avail-lipgloss.Width(left))) + " "
}

func fmtTok(n int) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	case n >= 1000:
		return fmt.Sprintf("%.1fk", float64(n)/1000)
	default:
		return fmt.Sprintf("%d", n)
	}
}

// flushPending moves the in-flight assistant text into the transcript,
// running markdown/code highlighting on the completed response.
func (c *chatModel) flushPending() {
	if c.pending.Len() == 0 {
		return
	}
	c.appendTranscript(padText(mdHighlight(safeText(c.pending.String())), c.w))
	c.pending.Reset()
}

// userBlock renders a submitted message as a full-width band with a distinct
// background, separating it from assistant output.
func (c chatModel) userBlock(text string) string {
	w := c.w
	if w <= 0 {
		w = 80
	}
	return userStyle.Width(w).Padding(1, 2).Render(safeText(text))
}

// toolBox renders a tool call plus its output as a bordered block.
func (c chatModel) toolBox(header, output string) string {
	var b strings.Builder
	b.WriteString(dimStyle.Render(header))
	if out := strings.TrimRight(output, "\n"); out != "" {
		inner := c.w - 6 // inside border(2)+padding(2), small slack
		if inner < 1 {
			inner = 1
		}
		b.WriteString("\n" + dimStyle.Render(divider("Output", inner)))
		lines := strings.Split(out, "\n")
		if len(lines) > 8 {
			lines = append(lines[:8], fmt.Sprintf("… %d more lines", len(lines)-8))
		}
		for _, l := range lines {
			b.WriteString("\n" + l)
		}
	}
	w := c.w
	if w < 1 {
		w = 1
	}
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("240")).
		Padding(0, 1).
		Width(w).
		Render(b.String())
}

// divider renders "── label " padded with ─ to width w.
func divider(label string, w int) string {
	pre := "── "
	if label != "" {
		pre += label + " "
	}
	if n := w - lipgloss.Width(pre); n > 0 {
		pre += strings.Repeat("─", n)
	}
	return pre
}

func truncateRunes(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

// padText indents loose transcript text 1 cell on each side, wrapping to
// the reduced width so continuation lines keep the margin. Boxes and
// slash-command output bypass it.
func padText(s string, w int) string {
	if w <= 0 {
		w = 80
	}
	limit := max(1, w-2)
	var b strings.Builder
	for _, ln := range strings.Split(s, "\n") {
		if lipgloss.Width(ln) > limit {
			ln = ansi.Wrap(ln, limit, "")
		}
		for _, sub := range strings.Split(ln, "\n") {
			if lipgloss.Width(sub) == 0 {
				b.WriteString("\n")
				continue
			}
			b.WriteString(" " + sub + "\n")
		}
	}
	return strings.TrimRight(b.String(), "\n") + "\n"
}
