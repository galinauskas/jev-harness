package tui

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	tea "charm.land/bubbletea/v2"
	"jevharness/internal/agent"
	"jevharness/internal/herdr"
	"jevharness/internal/openrouter"
	"jevharness/internal/session"
)

type herdrRecorder struct{ reports []herdr.Snapshot }

func (r *herdrRecorder) Report(s herdr.Snapshot) { r.reports = append(r.reports, s) }
func (r *herdrRecorder) latest() herdr.Snapshot  { return r.reports[len(r.reports)-1] }

func herdrApp(t *testing.T) *App {
	c := safetyChat(t)
	return &App{agent: c.ag, cfg: c.cfg, cwd: c.cwd, chat: c}
}

func TestHerdrLifecycleAcrossUIEvents(t *testing.T) {
	a := herdrApp(t)
	r := &herdrRecorder{}
	a.SetHerdrReporter(r)
	if r.latest().State != "idle" {
		t.Fatal("startup not idle")
	}
	a.chat.events = make(chan agent.Event)
	a.Update(tickMsg{})
	if r.latest().State != "working" {
		t.Fatal("turn not working")
	}
	a.Update(eventMsg{ev: agent.Event{Kind: agent.ToolCall, ToolName: "bash", ToolArgs: "secret arguments", Approve: make(chan bool, 1)}})
	if r.latest().State != "blocked" || r.latest().Message != "Tool approval required" {
		t.Fatal("approval not blocked or tool arguments leaked", r.latest())
	}
	a.Update(eventMsg{ev: agent.Event{Kind: agent.ToolResult, ToolName: "bash", Text: "done"}})
	if r.latest().State != "working" {
		t.Fatal("approval completion not working")
	}
	a.Update(chanClosedMsg{})
	if r.latest().State != "idle" {
		t.Fatal("channel closure not idle")
	}
	a.chat.recovery = true
	a.Update(tea.WindowSizeMsg{Width: 100, Height: 35})
	if r.latest().State != "blocked" {
		t.Fatal("recovery not blocked")
	}
	a.chat.ta.SetValue("/recover")
	a.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if r.latest().State != "idle" {
		t.Fatal("recovery acknowledgement not idle")
	}
}

func TestHerdrSessionChangesAndResumeOptions(t *testing.T) {
	a := herdrApp(t)
	r := &herdrRecorder{}
	a.SetHerdrReporter(r)
	a.chat, _ = a.chat.slash("/changes")
	a.Update(tickMsg{})
	id := a.chat.sessionID
	s := r.latest()
	if s.SessionID != id || !slices.Contains(s.ResumeArgv, id) {
		t.Fatal("session identity missing", s)
	}
	if _, err := a.chat.store.Load(id); err != nil {
		t.Fatal("advertised session was not saved", err)
	}
	_ = a.agent.SetMode("autonomous")
	a.Update(tickMsg{})
	if slices.Contains(r.latest().ResumeArgv, "autonomous") {
		t.Fatal("automatic restore retained autonomous permission")
	}
	_ = a.agent.SetMode("inspect")
	a.Update(tickMsg{})
	if !slices.Contains(r.latest().ResumeArgv, "inspect") {
		t.Fatal("inspect mode not retained")
	}
	a.chat, _ = a.chat.slash("/fork")
	a.Update(tickMsg{})
	if r.latest().SessionID == id || r.latest().SessionID == "" {
		t.Fatal("fork did not replace resume identity")
	}
	a.chat, _ = a.chat.slash("/clear")
	a.Update(tickMsg{})
	if r.latest().SessionID != "" || slices.Contains(r.latest().ResumeArgv, "--resume") {
		t.Fatal("new session retained old resume command")
	}
	a.chat.cfg.Safety.Ephemeral = true
	a.chat, _ = a.chat.slash("/changes")
	a.Update(tickMsg{})
	if r.latest().SessionID != "" || !slices.Contains(r.latest().ResumeArgv, "--ephemeral") || slices.Contains(r.latest().ResumeArgv, "--resume") {
		t.Fatal("ephemeral session advertised persistent resume")
	}
}

func TestCLIResumeRestoresHistoryUsageAndRecovery(t *testing.T) {
	a := herdrApp(t)
	id, _ := session.NewID()
	v := session.Session{ID: id, CWD: a.cwd, Title: "Resume me", Pinned: "basic", Running: true, Mode: "autonomous",
		Messages: []openrouter.Message{{Role: "user", Content: "unfinished task"}}, TokensIn: 42, Turns: 3, LastRole: a.cfg.Roles[0]}
	if err := a.chat.store.Save(v); err != nil {
		t.Fatal(err)
	}
	if err := a.ResumeSession(id); err != nil {
		t.Fatal(err)
	}
	if a.chat.sessionID != id || a.chat.tokensIn != 42 || a.chat.turns != 3 || a.agent.Pinned != "basic" || !a.chat.recovery || a.agent.Mode() != "develop" || a.chat.lastDec == nil {
		t.Fatal("resume did not restore state or reset permissions")
	}
	r := &herdrRecorder{}
	a.SetHerdrReporter(r)
	loaded, err := a.chat.store.Load(id)
	if err != nil || !loaded.Running || len(loaded.Messages) != 1 || loaded.Messages[0].Content != "unfinished task" {
		t.Fatal("reporting overwrote interrupted checkpoint", err)
	}
	if r.latest().State != "blocked" {
		t.Fatal("resumed interrupted session not blocked")
	}
	v.CWD = t.TempDir()
	if err := a.chat.store.Save(v); err != nil {
		t.Fatal(err)
	}
	if a.ResumeSession(id) == nil || a.ResumeSession("../../elsewhere") == nil {
		t.Fatal("resume bypassed project or ID validation")
	}
	a.chat.cfg.Safety.Ephemeral = true
	if a.ResumeSession(id) == nil {
		t.Fatal("ephemeral mode loaded persisted history")
	}
}

func TestReportingSaveFailureDoesNotBreakUI(t *testing.T) {
	a := herdrApp(t)
	r := &herdrRecorder{}
	a.SetHerdrReporter(r)
	if err := a.chat.ensureSession(); err != nil {
		t.Fatal(err)
	}
	badDir := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(badDir, []byte("file"), 0600); err != nil {
		t.Fatal(err)
	}
	a.chat.store.Dir = badDir
	a.Update(tickMsg{})
	if r.latest().State != "idle" || r.latest().SessionID != "" || a.chat.statusIsErr {
		t.Fatal("integration failure affected UI or advertised invalid resume")
	}
}
