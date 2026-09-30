package tui

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"
	"jevharness/internal/agent"
	"jevharness/internal/config"
	"jevharness/internal/openrouter"
	"jevharness/internal/router"
)

func safetyChat(t *testing.T) chatModel {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	cfg := config.Default()
	source := t.TempDir()
	_ = os.WriteFile(filepath.Join(source, "a.txt"), []byte("before"), 0644)
	client := openrouter.New("")
	ag := agent.New(client, router.New(client, cfg), cfg, source)
	t.Cleanup(ag.Cleanup)
	c := newChat(ag, cfg, source)
	c.resize(100, 35)
	return c
}
func TestPermissionResetAndReviewRequirement(t *testing.T) {
	c := safetyChat(t)
	c, _, _ = c.safetyCommand("/mode autonomous")
	if c.ag.Mode() != "autonomous" {
		t.Fatal("mode not changed")
	}
	c, _ = c.newSession()
	if c.ag.Mode() != "develop" || c.yolo {
		t.Fatal("mode survived session change")
	}
	if err := c.ensureSession(); err != nil {
		t.Fatal(err)
	}
	w, err := c.ag.Workspace()
	if err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(w.Stage, "a.txt"), []byte("agent"), 0644)
	c, _, _ = c.safetyCommand("/apply")
	if !c.statusIsErr {
		t.Fatal("apply skipped review")
	}
	c, _, _ = c.safetyCommand("/changes")
	c, _, _ = c.safetyCommand("/apply")
	data, _ := os.ReadFile(filepath.Join(c.cwd, "a.txt"))
	if string(data) != "agent" {
		t.Fatal("reviewed apply failed", c.status)
	}
}
func TestCtrlCSavesViaOrderlyExit(t *testing.T) {
	c := safetyChat(t)
	_ = c.ensureSession()
	ctx, cancel := context.WithCancel(context.Background())
	c.cancel = cancel
	c.events = make(chan agent.Event)
	c.approval = make(chan bool, 1)
	updated, _ := c.handleKey(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if !updated.quitting || ctx.Err() == nil {
		t.Fatal("Ctrl+C bypassed cancellation/drain")
	}
}
func TestRecoveredSessionDoesNotSubmit(t *testing.T) {
	c := safetyChat(t)
	c.recovery = true
	c.ta.SetValue("repeat the command")
	updated, cmd := c.submit()
	if cmd != nil || updated.events != nil || !updated.statusIsErr {
		t.Fatal("recovery silently reran work")
	}
	updated, _, _ = updated.safetyCommand("/recover")
	if updated.recovery {
		t.Fatal("recovery acknowledgement failed")
	}
}

func TestApplySelectedReviewAndRejectStaleReview(t *testing.T) {
	c := safetyChat(t)
	_ = c.ensureSession()
	w, err := c.ag.Workspace()
	if err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(w.Stage, "a.txt"), []byte("agent"), 0644)
	_ = os.WriteFile(filepath.Join(w.Stage, "b.txt"), []byte("new"), 0644)
	c, _, _ = c.safetyCommand("/changes a.txt")
	c, _, _ = c.safetyCommand("/apply a.txt")
	a, _ := os.ReadFile(filepath.Join(c.cwd, "a.txt"))
	if string(a) != "agent" {
		t.Fatal("selected review did not apply", c.status)
	}
	if _, err = os.Stat(filepath.Join(c.cwd, "b.txt")); !os.IsNotExist(err) {
		t.Fatal("unreviewed file applied")
	}
	c, _, _ = c.safetyCommand("/changes")
	_ = os.WriteFile(filepath.Join(w.Stage, "b.txt"), []byte("changed after review"), 0644)
	c, _, _ = c.safetyCommand("/apply")
	if !c.statusIsErr {
		t.Fatal("stale review applied")
	}
}

func TestDeleteCurrentSessionResetsUsageAndPermissions(t *testing.T) {
	c := safetyChat(t)
	if err := c.ensureSession(); err != nil {
		t.Fatal(err)
	}
	c.tokensIn, c.tokensOut, c.cost, c.turns = 10, 20, 0.1, 1
	c.sessionTitle = "Deleted conversation"
	if !c.saveSession() {
		t.Fatal(c.status)
	}
	id := c.sessionID
	c, _, _ = c.safetyCommand("/mode autonomous")
	c, _, _ = c.safetyCommand("/session delete " + id)
	if c.statusIsErr || c.sessionID != "" || c.sessionTitle != "" || c.tokensIn != 0 || c.tokensOut != 0 || c.cost != 0 || c.turns != 0 || c.ag.Mode() != "develop" {
		t.Fatal("deleted session leaked state into a new conversation", c.status)
	}
	if _, err := c.store.Load(id); err == nil {
		t.Fatal("deleted session remains saved")
	}
}
