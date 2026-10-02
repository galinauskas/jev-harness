package tui

import (
	tea "charm.land/bubbletea/v2"
	"fmt"
	"github.com/charmbracelet/x/ansi"
	"jevharness/internal/agent"
	"jevharness/internal/session"
)

func (c chatModel) handleEvent(ev agent.Event) (chatModel, tea.Cmd) {
	next := waitEvent(c.events)
	switch ev.Kind {
	case agent.Routed:
		if c.models == nil {
			c.models = make(map[string]session.ModelUsage)
		}
		if model := ev.Decision.Role.Backend() + ":" + ev.Decision.Role.Model; model != "" {
			if _, ok := c.models[model]; !ok {
				c.models[model] = session.ModelUsage{}
			}
		}
		c.lastDec = ev.Decision
		c.appendTranscript(padText(routeStyle.Render(routeLine(*ev.Decision))+"\n", c.w) + "\n")
		return c, tea.Batch(next, c.requestContext())
	case agent.UsageRecorded:
		c.recordUsage(ev.Model, ev.Usage)
	case agent.Compacting:
		c.flushPending()
		c.status = "Compacting context…"
	case agent.Compacted:
		c.status = ""
		if c.commandRunning != "" {
			c.status, c.statusIsErr = ev.Text, false
			c.commandFeedback(c.commandRunning)
			c.commandReported = true
		} else {
			c.appendTranscript("\n" + padText(dimStyle.Render(safeText(ev.Text)), c.w) + "\n")
		}
	case agent.CompactionWarning:
		if c.commandRunning != "" {
			c.status, c.statusIsErr = ev.Text, false
			c.commandFeedback(c.commandRunning)
			c.commandReported = true
		} else {
			c.appendTranscript("\n" + padText(warnStyle.Render(safeText(ev.Text)), c.w) + "\n")
		}
	case agent.TextDelta:
		c.pending.WriteString(ev.Text)
	case agent.ToolOutput:
		c.status = truncateRunes(safeText(ev.Text), 120)
		c.appendTranscript(dimStyle.Render(safeText(ev.Text)))
	case agent.ToolCall:
		c.flushPending()
		// hold the header; the box is rendered when the result arrives
		c.pendingTool = toolHeader(safeText(ev.ToolName), truncateRunes(safeText(ev.ToolArgs), 300))

		c.approval = ev.Approve
		c.approveText = ansi.Strip(toolHeader(safeText(ev.ToolName), safeText(ev.ToolArgs)))
		if c.approval != nil {
			c.appendTranscript(padText(warnStyle.Render("Review tool request · PgUp/PgDn to scroll\n"+
				safeText(ev.ToolName)+" "+safeText(ev.ToolArgs)), c.w))
		}
	case agent.ToolResult:
		c.approval = nil
		c.approveText = ""
		header := c.pendingTool
		if header == "" {
			header = toolHeader(ev.ToolName, "")
		}
		c.pendingTool = ""
		c.appendTranscript("\n" + c.toolBox(header, safeText(ev.Text)) + "\n")
	case agent.TurnDone:
		c.flushPending()
		if u := ev.Usage; u != nil {
			tok := u.TotalTokens
			line := fmt.Sprintf("%d tokens", tok)
			if ev.Duration > 0 {
				line += fmt.Sprintf(" · %.1fs", ev.Duration.Seconds())
				if u.CompletionTokens > 0 {
					line += fmt.Sprintf(" · %.1f tok/s", float64(u.CompletionTokens)/ev.Duration.Seconds())
				}
			}
			c.appendTranscript("\n" + padText(dimStyle.Render(line), c.w))
		} else {
			c.appendTranscript("\n")
		}
		c.status = ""
		c.contextUsed = ev.ContextTokens
		if c.lastDec != nil {
			c.contextModel = c.lastDec.Role.Model
		}
		c.turns++
	case agent.Error:
		c.approval = nil
		c.approveText = ""
		c.flushPending()
		if c.pendingTool != "" {
			c.appendTranscript(padText(dimStyle.Render(c.pendingTool), c.w))
			c.pendingTool = ""
		}
		if c.commandRunning != "" {
			c.status, c.statusIsErr = ev.Text, true
			c.commandFeedback(c.commandRunning)
			c.commandReported = true
		} else {
			c.appendTranscript(padText(errStyle.Render("✗ "+safeText(ev.Text)), c.w))
		}
		c.status = ""
	}
	return c, next
}
