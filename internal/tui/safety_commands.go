package tui

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"jevharness/internal/redact"
	"jevharness/internal/session"
	"jevharness/internal/tools"
	"jevharness/internal/workspace"
)

type diagnosticMsg struct {
	Text string
	Err  error
}

func (c *chatModel) ensureSession() error {
	if c.sessionID == "" {
		id, err := session.NewID()
		if err != nil {
			return err
		}
		c.sessionID = id
		c.sessionTitle = "Untitled session"
		c.sessionUpdated = time.Now()
	}
	c.ag.SetSessionID(c.sessionID)
	return nil
}
func changeDigest(changes []workspace.Change) string {
	h := sha256.New()
	for _, change := range changes {
		fmt.Fprintf(h, "%s:%s:%s:%d\n", change.Path, workspace.Hash(change.Before), workspace.Hash(change.After), func() uint32 {
			if change.After != nil {
				return uint32(change.After.Mode)
			}
			return 0
		}())
	}
	return hex.EncodeToString(h.Sum(nil))
}
func (c chatModel) safetyCommand(text string) (chatModel, tea.Cmd, bool) {
	parts := strings.Fields(text)
	if len(parts) == 0 {
		return c, nil, false
	}
	fail := func(err error) (chatModel, tea.Cmd, bool) {
		c.status, c.statusIsErr = err.Error(), true
		return c, nil, true
	}
	switch parts[0] {
	case "/mode", "/yolo":
		mode := "develop"
		if parts[0] == "/yolo" {
			if c.ag.Mode() != "autonomous" {
				mode = "autonomous"
			}
		} else {
			if len(parts) != 2 {
				return fail(fmt.Errorf("usage: /mode inspect|develop|autonomous"))
			}
			mode = parts[1]
		}
		if err := c.ag.SetMode(mode); err != nil {
			return fail(err)
		}
		c.yolo = mode == "autonomous"
		c.status = "Mode: " + mode + "; changes stay staged until /apply"
		return c, nil, true
	case "/fork":
		if err := c.ensureSession(); err != nil {
			return fail(err)
		}
		if !c.saveSession() {
			return c, nil, true
		}
		id, err := session.NewID()
		if err != nil {
			return fail(err)
		}
		if err = c.ag.ForkWorkspace(id); err != nil {
			return fail(err)
		}
		c.sessionID = id
		c.sessionTitle += " (fork)"
		c.sessionUpdated = time.Now()
		c.yolo = false
		c.reviewed = ""
		c.ag.SetPersistence(c.snapshot())
		if !c.saveSession() {
			return c, nil, true
		}
		c.status = "Forked conversation and staged files; project files unchanged"
		return c, nil, true
	case "/recover":
		c.recovery = false
		c.ag.Recover()
		c.status = "Recovery acknowledged; review /changes before continuing"
		return c, nil, true
	case "/changes", "/apply", "/undo", "/discard":
		if err := c.ensureSession(); err != nil {
			return fail(err)
		}
		w, err := c.ag.Workspace()
		if err != nil {
			return fail(err)
		}
		if parts[0] == "/undo" {
			if len(parts) < 2 {
				return fail(fmt.Errorf("usage: /undo <path>"))
			}
			err = w.Undo(strings.TrimSpace(strings.TrimPrefix(text, "/undo")))
			if err != nil {
				return fail(err)
			}
			c.reviewed = ""
			c.status = "Restored reviewed file"
			return c, nil, true
		}
		if parts[0] == "/discard" {
			if len(parts) != 1 {
				return fail(fmt.Errorf("usage: /discard"))
			}
			if err = w.Discard(); err != nil {
				return fail(err)
			}
			c.reviewed = ""
			c.status = "Staged changes discarded; refreshed from project"
			return c, nil, true
		}
		changes, err := w.Changes()
		if err != nil {
			return fail(err)
		}

		selectedPath := strings.TrimSpace(strings.TrimPrefix(text, parts[0]))
		if selectedPath != "" {
			selected, err := workspace.Relative(selectedPath)
			if err != nil {
				return fail(err)
			}
			filtered := changes[:0]
			for _, change := range changes {
				if change.Path == selected {
					filtered = append(filtered, change)
				}
			}
			if len(filtered) == 0 {
				return fail(fmt.Errorf("no staged changes in %s", selected))
			}
			changes = filtered
			selectedPath = selected
		}
		digest := changeDigest(changes)
		if parts[0] == "/changes" {
			if len(changes) == 0 {
				c.status = "No staged changes"
			} else {
				var preview strings.Builder
				for _, change := range changes {
					preview.WriteString(workspace.Preview(change))
				}
				if preview.Len() > 1<<20 {
					c.reviewed = ""
					return fail(fmt.Errorf("diff is too large to review at once; use /changes <path>"))
				}
				c.appendTranscript(safeText(preview.String()))
				c.status = fmt.Sprintf("%d changed files; /apply [path] applies reviewed changes", len(changes))
			}
			c.reviewed = digest
			return c, nil, true
		}
		if c.recovery {
			return fail(fmt.Errorf("review /changes, then /recover before applying interrupted work"))
		}
		if c.reviewed != digest {
			return fail(fmt.Errorf("run /changes to review the current changes before /apply"))
		}
		var paths []string
		if len(parts) > 1 {
			paths = []string{strings.TrimSpace(strings.TrimPrefix(text, "/apply"))}
		}
		n, err := w.Apply(paths)
		c.reviewed = ""
		if err != nil {
			return fail(fmt.Errorf("applied %d files: %w", n, err))
		}
		c.status = fmt.Sprintf("Applied %d files; /undo <path> selectively restores them", n)
		return c, nil, true
	case "/attach":
		path := strings.TrimSpace(strings.TrimPrefix(text, "/attach"))
		if path == "" {
			return fail(fmt.Errorf("usage: /attach <workspace path>"))
		}
		if err := c.ensureSession(); err != nil {
			return fail(err)
		}
		attachment, err := c.ag.Attach(path)
		if err != nil {
			return fail(err)
		}
		c.ta.SetValue(c.ta.Value() + "\n" + attachment + "\n")
		c.status = "File added to your draft"
		return c, nil, true
	case "/compact":
		if c.recovery {
			return fail(fmt.Errorf("acknowledge recovery before compacting"))
		}
		if err := c.ensureSession(); err != nil {
			return fail(err)
		}
		c.ag.SetPersistence(c.snapshot())
		ctx, cancel := context.WithTimeout(context.Background(), time.Duration(c.cfg.Limits.Seconds)*time.Second)
		c.cancel = cancel
		c.events = c.ag.ManualCompact(ctx)
		return c, tea.Batch(waitEvent(c.events), tick()), true
	case "/name":
		title := strings.TrimSpace(strings.TrimPrefix(text, "/name"))
		if title == "" {
			return fail(fmt.Errorf("usage: /name <title>"))
		}
		c.sessionTitle = truncateRunes(title, 80)
		if c.sessionID != "" && !c.saveSession() {
			return c, nil, true
		}
		c.status = "Session renamed"
		return c, nil, true
	case "/doctor":
		if !c.cfg.Safety.DockerSandbox {
			return c, func() tea.Msg {
				return diagnosticMsg{Text: "Docker sandbox (experimental): off; shell runs locally with normal host and network access"}
			}, true
		}
		image := c.cfg.Safety.SandboxImage
		return c, func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			err := tools.CheckSandbox(ctx, image)
			return diagnosticMsg{Text: "Experimental Docker sandbox enabled; commands have no network or host credentials", Err: err}
		}, true
	case "/session":
		if c.cfg.Safety.Ephemeral && len(parts) > 1 && (parts[1] == "delete" || parts[1] == "export") {
			return fail(fmt.Errorf("persistent session operations are disabled in ephemeral mode"))
		}
		if c.cfg.Safety.Ephemeral && (len(parts) < 2 || (parts[1] != "new" && parts[1] != "stats")) {
			return fail(fmt.Errorf("saved session browsing is disabled in ephemeral mode"))
		}
		if len(parts) < 2 || parts[1] == "new" || parts[1] == "stats" {
			return c, nil, false
		}
		if parts[1] == "search" {
			list, err := c.store.List(c.cwd)
			if err != nil {
				return fail(err)
			}
			query := strings.ToLower(strings.Join(parts[2:], " "))
			c.sessionList = nil
			for _, v := range list {
				if strings.Contains(strings.ToLower(v.Title+" "+v.ID), query) {
					c.sessionList = append(c.sessionList, v)
				}
			}
			c.sessionPicker = true
			c.sessionCursor = 0
			return c, nil, true
		}
		if parts[1] == "delete" {
			if len(parts) != 3 {
				return fail(fmt.Errorf("usage: /session delete <id>"))
			}
			v, err := c.store.Load(parts[2])
			if err != nil {
				return fail(err)
			}
			if v.CWD != c.cwd {
				return fail(fmt.Errorf("session belongs to another project"))
			}
			if err = c.store.Delete(v.ID); err != nil {
				return fail(err)
			}
			if c.sessionID == v.ID {
				c.sessionID = ""
				next, cmd := c.newSession()
				next.status = "Session deleted"
				return next, cmd, true
			}
			c.status = "Session deleted"
			return c, nil, true
		}
		if parts[1] == "export" {
			if len(parts) < 3 {
				return fail(fmt.Errorf("usage: /session export <new file path>"))
			}
			if !c.saveSession() {
				return c, nil, true
			}
			path := strings.TrimSpace(strings.TrimPrefix(text, "/session export"))
			if err := c.store.Export(c.sessionID, path); err != nil {
				return fail(err)
			}
			c.status = "Exported private session; inspect before sharing"
			return c, nil, true
		}
	}
	return c, nil, false
}
func (c *chatModel) snapshot() session.Session {
	r := redact.New(c.cfg.APIKey, c.cfg.DeepSeekAPIKey, c.cfg.OpenCodeGoAPIKey, c.cfg.ExaAPIKey, c.cfg.BraveAPIKey)
	return session.Session{Version: 1, ID: c.sessionID, Title: c.sessionTitle, CWD: c.cwd, Updated: c.sessionUpdated, Messages: c.ag.SafeHistory(), Transcript: r.Text(c.transcript.String()), Pinned: c.ag.Pinned, TokensIn: c.tokensIn, TokensOut: c.tokensOut, Cost: c.cost, Turns: c.turns, ContextUsed: c.contextUsed, ContextModel: c.contextModel, Models: copyModels(c.models), Running: false, PendingTool: c.ag.PendingTool(), Mode: c.ag.Mode()}
}
func copyModels(models map[string]session.ModelUsage) map[string]session.ModelUsage {
	out := map[string]session.ModelUsage{}
	for k, v := range models {
		out[k] = v
	}
	return out
}
