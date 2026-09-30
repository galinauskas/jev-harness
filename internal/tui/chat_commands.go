package tui

import (
	tea "charm.land/bubbletea/v2"
	"fmt"
	"github.com/charmbracelet/x/ansi"
	"strings"
)

// slash handles commands before submit. Returns the model and cmd.
func (c chatModel) slash(text string) (chatModel, tea.Cmd) {
	parts := strings.Fields(text)
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
			c.status, c.statusIsErr = "usage: /session [new|stats]", true
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
			c.status = "usage: /role <name>|auto"
			return c, nil
		}
		if parts[1] == "auto" {
			c.ag.Pinned = ""
			c.lastDec = nil
			return c, c.requestContext()
		}
		if _, ok := c.cfg.RoleByName(parts[1]); !ok {
			c.status = fmt.Sprintf("unknown role %q", parts[1])
			c.statusIsErr = true
			return c, nil
		}
		c.ag.Pinned = parts[1]
		return c, c.requestContext()
	case "/yolo":
		c.yolo = !c.yolo
		return c, nil
	case "/roles":
		var b strings.Builder
		b.WriteString(dimStyle.Render("roles:") + "\n")
		width := max(12, c.vp.Width()-4)
		for _, r := range c.cfg.Roles {
			star := "  "
			if r.Name == c.cfg.DefaultRole {
				star = "★ "
			}
			fmt.Fprintf(&b, "  %s%s  %s\n", star, safeText(r.Name),
				accent.Render(ansi.Truncate(safeText(r.Model), max(8, width-len(r.Name)-6), "…")))
			fmt.Fprintf(&b, "    %s\n", dimStyle.Render(ansi.Truncate(safeText(r.Description), width, "…")))
		}
		c.appendTranscript(b.String())
		return c, nil
	default:
		c.status = fmt.Sprintf("unknown command %q", parts[0])
		c.statusIsErr = true
		return c, nil
	}
}
