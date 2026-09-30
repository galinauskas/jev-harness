package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"jevharness/internal/router"
)

// saveSession reads agent state only while idle, after channel closure has
// synchronized with the worker. Empty chats never produce files.
func (c *chatModel) saveSession() bool {
	if c.running() {
		return false
	}
	if c.sessionID == "" {
		return true
	}
	if c.cfg.Safety.Ephemeral {
		return true
	}
	v := c.snapshot()
	if c.lastDec != nil {
		v.LastRole = c.lastDec.Role
	}
	if err := c.store.Save(v); err != nil {
		c.status, c.statusIsErr = "save session: "+err.Error(), true
		return false
	}
	return true
}

func (c chatModel) quit() (chatModel, tea.Cmd) {
	if c.running() {
		c.quitting = true
		if c.cancel != nil {
			c.cancel()
		}
		c.status, c.statusIsErr = "saving session before exit…", false
		return c, nil
	}
	if !c.saveSession() {
		return c, nil
	}
	return c, tea.Quit
}

func (c chatModel) newSession() (chatModel, tea.Cmd) {
	if !c.saveSession() {
		return c, nil
	}
	c.ag.Clear()
	c.yolo = false
	c.reviewed = ""
	c.recovery = false
	c.followups = nil
	c.sessionID, c.sessionTitle = "", ""
	c.sessionPicker, c.sessionList = false, nil
	c.transcript.Reset()
	c.pending.Reset()
	c.lastDec = nil
	c.pendingTool = ""
	c.tokensIn, c.tokensOut, c.cost, c.turns = 0, 0, 0, 0
	c.contextUsed, c.contextModel = 0, ""
	c.models = nil
	c.status, c.statusIsErr = "new session", false
	c.refreshVP()
	return c, c.requestContext()
}

func (c chatModel) sessionKey(m tea.KeyPressMsg) (chatModel, tea.Cmd) {
	switch m.String() {
	case "esc":
		c.sessionPicker, c.sessionList = false, nil
	case "up", "k":
		c.sessionCursor = max(0, c.sessionCursor-1)
	case "down", "j":
		c.sessionCursor = min(len(c.sessionList), c.sessionCursor+1)
	case "home":
		c.sessionCursor = 0
	case "end":
		c.sessionCursor = len(c.sessionList)
	case "enter":
		if c.sessionCursor == 0 {
			return c.newSession()
		}
		v, err := c.store.Load(c.sessionList[c.sessionCursor-1].ID)
		if err != nil {
			c.status, c.statusIsErr = "open session: "+err.Error(), true
			return c, nil
		}
		if v.CWD != c.cwd {
			c.status, c.statusIsErr = "session belongs to another working directory", true
			return c, nil
		}
		c.ag.Restore(v.Messages, v.Pinned)
		c.ag.SetSessionID(v.ID)
		c.ag.ResetPermissions()
		c.yolo = false
		c.reviewed = ""
		c.recovery = v.Running || v.PendingTool != ""
		c.ag.SetPersistence(v)
		c.sessionID, c.sessionTitle, c.sessionUpdated = v.ID, v.Title, v.Updated
		c.transcript.Reset()
		if v.Transcript != "" {
			c.transcript.WriteString(safeTranscript(v.Transcript))
		} else {
			for _, m := range v.Messages {
				c.transcript.WriteString(safeText(m.Role+": "+m.Content) + "\n")
			}
		}
		if c.recovery {
			c.transcript.WriteString("Interrupted session: inspect /changes and acknowledge with /recover. Pending tools will not be replayed.\n")
		}
		c.pending.Reset()
		c.lastDec = nil
		if v.LastRole.Model != "" {
			c.lastDec = &router.Decision{Role: v.LastRole}
		}
		c.tokensIn, c.tokensOut, c.cost, c.turns = v.TokensIn, v.TokensOut, v.Cost, v.Turns
		c.contextUsed, c.contextModel = v.ContextUsed, v.ContextModel
		c.models = v.Models
		c.sessionPicker, c.sessionList = false, nil
		c.status, c.statusIsErr = "resumed: "+v.Title, false
		c.refreshVP()
		c.vp.GotoBottom()
		return c, c.requestContext()
	}
	return c, nil
}

func (c chatModel) sessionsView() string {
	width := max(1, c.w-2)
	rows := max(1, c.h-6)
	start := max(0, c.sessionCursor-rows+1)
	var b strings.Builder
	b.WriteString(" " + boldStyle.Render("Sessions") + "\n")
	b.WriteString(" " + dimStyle.Render(ansi.Truncate(safeText(c.cwd), width, "…")) + "\n\n")
	for i := start; i <= len(c.sessionList) && i < start+rows; i++ {
		label := "New session"
		if i > 0 {
			v := c.sessionList[i-1]
			marker := ""
			if v.ID == c.sessionID {
				marker = " · current"
			}
			label = fmt.Sprintf("%s · %s%s", v.Updated.Local().Format("Jan 02 15:04"), safeText(v.Title), marker)
		}
		prefix := "  "
		if i == c.sessionCursor {
			prefix = "› "
		}
		line := ansi.Truncate(prefix+label, width, "…")
		if i == c.sessionCursor {
			line = accent.Render(line)
		}
		b.WriteString(" " + line + "\n")
	}
	if len(c.sessionList) == 0 {
		b.WriteString(" " + dimStyle.Render("No saved sessions in this directory yet.") + "\n")
	}
	if c.statusIsErr {
		b.WriteString(" " + errStyle.Render(ansi.Truncate(safeText(c.status), width, "…")) + "\n")
	}
	b.WriteString("\n " + dimStyle.Render(ansi.Truncate("↑/↓ select · Enter open · Esc back", width, "…")))
	return b.String()
}
