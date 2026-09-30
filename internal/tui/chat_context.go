package tui

import (
	tea "charm.land/bubbletea/v2"
	"context"
	"jevharness/internal/config"
	"time"
)

func (c *chatModel) activeRole() config.Role {
	if c.ag.Pinned != "" {
		if role, ok := c.cfg.RoleByName(c.ag.Pinned); ok {
			return role
		}
	}
	if c.lastDec != nil {
		return c.lastDec.Role
	}
	role, _ := c.cfg.RoleByName(c.cfg.DefaultRole)
	return role
}

func (c *chatModel) activeModel() string { return c.activeRole().Model }

func roleContextKey(role config.Role) string { return role.Backend() + ":" + role.Model }

func (c *chatModel) requestContext() tea.Cmd {
	role := c.activeRole()
	key := roleContextKey(role)
	if role.Model == "" || c.contextRequested[key] {
		return nil
	}
	c.contextRequested[key] = true
	lookup := c.ag.ContextLookup(role)
	generation := c.contextGeneration
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		length, _ := lookup(ctx)
		return modelContextMsg{model: key, length: length, generation: generation}
	}
}
