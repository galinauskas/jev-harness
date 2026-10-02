package tui

import (
	tea "charm.land/bubbletea/v2"
	"fmt"
	"strings"
)

// slash handles commands before submit. Returns the model and cmd.
func (c chatModel) slash(text string) (chatModel, tea.Cmd) {
	if next, cmd, handled := c.safetyCommand(text); handled {
		return next, cmd
	}
	c.status, c.statusIsErr = "", false
	return c.slashResult(text)
}

func (c chatModel) slashResult(text string) (next chatModel, cmd tea.Cmd) {
	parts := strings.Fields(text)
	if len(parts) == 0 {
		return c, nil
	}
	defer func() { next.commandFeedback(parts[0]) }()
	switch parts[0] {
	case "/settings":
		return c, func() tea.Msg { return openSettingsMsg{} }
	case "/quit":
		return c.quit()
	case "/stats":
		if len(parts) != 1 {
			c.status, c.statusIsErr = "usage: /stats", true
			return c, nil
		}
		c.appendTranscript(c.sessionStats())
		return c, nil
	case "/session":
		if len(parts) == 2 && parts[1] == "stats" {
			c.appendTranscript(c.sessionStats())
			return c, nil
		}
		if len(parts) == 2 && parts[1] == "new" {
			return c.newSession()
		}
		if len(parts) != 1 {
			c.status, c.statusIsErr = "usage: /session [new|stats|search <text>|delete <id>|export <path>]", true
			return c, nil
		}
		if !c.saveSession() {
			return c, nil
		}
		list, err := c.store.List(c.cwd)
		if err != nil {
			c.status, c.statusIsErr = "list sessions: "+err.Error(), true
			return c, nil
		}
		c.sessionList, c.sessionCursor, c.sessionPicker = list, 0, true
		return c, nil
	case "/clear":
		return c.newSession()
	case "/role":
		if len(parts) < 2 {
			c.status, c.statusIsErr = "usage: /role <name>|auto", true
			return c, nil
		}
		if parts[1] == "auto" {
			c.ag.Pinned = ""
			c.lastDec = nil
			c.status = "Automatic role routing enabled"
			return c, c.requestContext()
		}
		if _, ok := c.cfg.RoleByName(parts[1]); !ok {
			c.status = fmt.Sprintf("unknown role %q", parts[1])
			c.statusIsErr = true
			return c, nil
		}
		c.ag.Pinned = parts[1]
		c.status = "Pinned role: " + parts[1]
		return c, c.requestContext()
	case "/roles":
		c.appendTranscript(c.rolesTable())
		return c, nil
	default:
		c.status = fmt.Sprintf("unknown command %q", parts[0])
		c.statusIsErr = true
		return c, nil
	}
}
