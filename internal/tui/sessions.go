package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"jevharness/internal/router"
	"jevharness/internal/session"
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

func (c chatModel) sessionKey(m tea.KeyPressMsg) (next chatModel, cmd tea.Cmd) {
	defer func() {
		if m.String() == "enter" && !next.sessionPicker {
			next.commandFeedback("/session")
		}
	}()
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
		c.restoreSession(v)
		return c, c.requestContext()
	}
	return c, nil
}

// restoreSession is shared by the picker and CLI resume.
func (c *chatModel) restoreSession(v session.Session) {
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
}

func (c chatModel) sessionsView() string {
	width := c.commandWidth()
	// Each picker row stays on one line so selection and scrolling agree.
	limit := max(1, c.h-9)
	errorOutput := ""
	if c.statusIsErr {
		errorOutput = renderCommandTable(width, []string{"Error"}, [][]string{{c.status}}, false)
		limit = max(1, limit-lipgloss.Height(errorOutput))
	}
	start := max(0, c.sessionCursor-limit+1)
	var rows [][]string
	for i := start; i <= len(c.sessionList) && i < start+limit; i++ {
		title, updated, state := "New session", "—", ""
		if i > 0 {
			v := c.sessionList[i-1]
			title = strings.Join(strings.Fields(safeText(v.Title)), " ")
			updated = v.Updated.Local().Format("Jan 02 15:04")
			if v.ID == c.sessionID {
				state = "Current"
			}
		} else if len(c.sessionList) == 0 {
			state = "No saved sessions"
		}
		marker := "  "
		if i == c.sessionCursor {
			marker = "› "
		}
		rows = append(rows, []string{marker + title, updated, state})
	}
	t := newCommandTable(width, []string{"Session", "Updated", "State"}, rows, false).
		Width(width).Wrap(false).
		StyleFunc(func(row, col int) lipgloss.Style {
			style := commandCellStyle(row, col, false)
			if row == c.sessionCursor-start {
				return style.Foreground(lipgloss.Color("255")).Background(lipgloss.Color("31")).Bold(true)
			}
			return style
		})
	var b strings.Builder
	b.WriteString("  " + boldStyle.Render("Sessions") + "\n")
	b.WriteString("  " + dimStyle.Render(ansi.Truncate(safeText(c.cwd), width, "…")) + "\n\n")
	b.WriteString(lipgloss.NewStyle().MarginLeft(2).Render(t.String()) + "\n")
	if errorOutput != "" {
		b.WriteString(errorOutput + "\n")
	}
	b.WriteString("\n  " + dimStyle.Render(ansi.Truncate("↑/↓ select · Enter open · Esc back", width, "…")))
	return b.String()
}
