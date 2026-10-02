package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"jevharness/internal/agent"
	"jevharness/internal/session"
)

func requireCommandTable(t *testing.T, output string) {
	t.Helper()
	plain := ansi.Strip(output)
	if !strings.Contains(plain, "╭") || !strings.Contains(plain, "│") || !strings.Contains(plain, "╰") {
		t.Fatalf("command output is not a table:\n%s", plain)
	}
	if safeTranscript(output) != output {
		t.Fatal("command output contains unsafe terminal sequences")
	}
}

func TestEverySlashCommandOutput(t *testing.T) {
	commands := []string{
		"/fork", "/mode inspect", "/changes", "/apply", "/undo a.txt", "/discard",
		"/attach a.txt", "/recover", "/name Renamed", "/doctor", "/session", "/stats",
		"/clear", "/role basic", "/roles", "/yolo", "/quit", "/settings", "/compact",
	}
	covered := map[string]bool{}
	for _, command := range commands {
		covered[strings.Fields(command)[0]] = true
		t.Run(command, func(t *testing.T) {
			c := safetyChat(t)
			if command == "/apply" || strings.HasPrefix(command, "/undo") {
				if err := c.ensureSession(); err != nil {
					t.Fatal(err)
				}
				w, err := c.ag.Workspace()
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(w.Stage, "a.txt"), []byte("after"), 0644); err != nil {
					t.Fatal(err)
				}
				c, _ = c.slash("/changes")
				if strings.HasPrefix(command, "/undo") {
					c, _ = c.slash("/apply")
				}
				c.transcript.Reset()
			}
			if command == "/compact" {
				// A known context window and short history need no remote lookup.
				c.cfg.Roles[0].ContextWindow = 128000
				c.ag.SetConfig(c.cfg)
			}
			next, cmd := c.slash(command)
			if next.statusIsErr {
				t.Fatal(next.status)
			}
			switch command {
			case "/settings", "/quit":
				if cmd == nil || next.transcript.Len() != 0 {
					t.Fatal("navigation-only command unexpectedly printed output")
				}
			case "/session":
				requireCommandTable(t, next.sessionsView())
			case "/doctor":
				next, _ = next.Update(cmd())
				requireCommandTable(t, next.transcript.String())
			case "/compact":
				for ev := range next.events {
					next, _ = next.handleEvent(ev)
				}
				next, _ = next.Update(chanClosedMsg{})
				requireCommandTable(t, next.transcript.String())
			default:
				requireCommandTable(t, next.transcript.String())
			}
		})
	}
	for _, command := range slashCommands {
		if !covered[command.name] {
			t.Fatalf("slash command %s has no output audit", command.name)
		}
	}
}

func TestCommandErrorsAndSessionSubcommands(t *testing.T) {
	for _, command := range []string{"/unknown", "/mode invalid", "/role missing", "/stats extra", "/attach", "/apply", "/session invalid"} {
		t.Run(command, func(t *testing.T) {
			c := safetyChat(t)
			c, _ = c.slash(command)
			if !c.statusIsErr {
				t.Fatal("expected command error")
			}
			requireCommandTable(t, c.transcript.String())
			if !strings.Contains(ansi.Strip(c.transcript.String()), "Error") {
				t.Fatal("error table has no error label")
			}
		})
	}
	for _, subcommand := range []string{"new", "stats", "search", "delete", "export"} {
		t.Run("session-"+subcommand, func(t *testing.T) {
			c := safetyChat(t)
			if err := c.ensureSession(); err != nil || !c.saveSession() {
				t.Fatal("session setup failed", err, c.status)
			}
			command := "/session " + subcommand
			if subcommand == "delete" {
				command += " " + c.sessionID
			}
			if subcommand == "export" {
				command += " " + filepath.Join(t.TempDir(), "export.json")
			}
			c, _ = c.slash(command)
			if c.statusIsErr {
				t.Fatal(c.status)
			}
			if c.sessionPicker {
				requireCommandTable(t, c.sessionsView())
			} else {
				requireCommandTable(t, c.transcript.String())
			}
		})
	}
}

func TestCommandTablesFitAndPreserveDiffs(t *testing.T) {
	for _, width := range []int{40, 80, 112} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			c := safetyChat(t)
			c.resize(width, 34)
			c.cfg.Roles[0].Model = "provider/" + strings.Repeat("long-model-", 10)
			c.cfg.Roles[0].Description = "Tasks with 界 and \x1b]52;c;bad\a terminal controls"
			if err := c.ensureSession(); err != nil {
				t.Fatal(err)
			}
			w, err := c.ag.Workspace()
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(w.Stage, "a.txt"), []byte(strings.Repeat("界", 120)+"\n\tindented\n"), 0644); err != nil {
				t.Fatal(err)
			}
			c, _ = c.slash("/changes")
			output := c.transcript.String()
			requireCommandTable(t, output)
			if strings.Count(output, "界") != 120 || !strings.Contains(output, "-before") || !strings.Contains(output, "+界") || !strings.Contains(output, "indented") || c.reviewed == "" {
				t.Fatal("diff table lost content or review state")
			}
			c.transcript.Reset()
			c, _ = c.slash("/roles")
			output += c.transcript.String() + c.sessionStats()
			for _, line := range strings.Split(output, "\n") {
				if lipgloss.Width(line) > width {
					t.Fatalf("table exceeds %d columns: %q", width, ansi.Strip(line))
				}
			}
		})
	}
}

func TestSessionTableScrollAndSelection(t *testing.T) {
	for _, height := range []int{10, 15, 34} {
		c := safetyChat(t)
		c.resize(60, height)
		for i := 0; i < 30; i++ {
			c.sessionList = append(c.sessionList, session.Session{ID: fmt.Sprint(i), Title: fmt.Sprintf("Session %02d", i), Updated: time.Now()})
		}
		c.sessionCursor = 29
		view := c.sessionsView()
		requireCommandTable(t, view)
		if !strings.Contains(ansi.Strip(view), "› Session 28") || lipgloss.Height(view) > height {
			t.Fatalf("selected session is not visible within %d rows:\n%s", height, ansi.Strip(view))
		}
		if height == 34 {
			c.status, c.statusIsErr = strings.Repeat("Cannot open this session. ", 10), true
			view = c.sessionsView()
			if lipgloss.Height(view) > height || !strings.Contains(ansi.Strip(view), "› Session 28") {
				t.Fatal("wrapped error hides session selection")
			}
		}
		c.sessionPicker = true
		c.sessionCursor = 0
		c, _ = c.sessionKey(tea.KeyPressMsg{Code: tea.KeyEnter})
		requireCommandTable(t, c.transcript.String())
	}
}

func TestAsynchronousCommandTables(t *testing.T) {
	for _, kind := range []agent.Kind{agent.Compacted, agent.CompactionWarning, agent.Error} {
		c := safetyChat(t)
		c.commandRunning = "/compact"
		c, _ = c.handleEvent(agent.Event{Kind: kind, Text: "Compaction result"})
		requireCommandTable(t, c.transcript.String())
		if !c.commandReported {
			t.Fatal("compaction result not tracked")
		}
		before := c.transcript.Len()
		c, _ = c.Update(chanClosedMsg{})
		if c.transcript.Len() != before || c.commandRunning != "" {
			t.Fatal("closing command duplicated its result")
		}
	}
	c := safetyChat(t)
	c, _ = c.Update(diagnosticMsg{Text: "Sandbox check", Err: fmt.Errorf("Docker unavailable")})
	requireCommandTable(t, c.transcript.String())
	if !c.statusIsErr || !strings.Contains(c.transcript.String(), "Docker unavailable") {
		t.Fatal("asynchronous diagnostic error lost")
	}
}
