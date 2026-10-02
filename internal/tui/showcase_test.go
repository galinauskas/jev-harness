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
	"jevharness/internal/agent"
	"jevharness/internal/config"
	"jevharness/internal/openrouter"
	"jevharness/internal/router"
	"jevharness/internal/session"
)

// TestShowcase renders deterministic UI fixtures, without provider requests.
// JEV_SHOWCASE_DIR is an explicit output directory; normal test runs skip it.
func TestShowcase(t *testing.T) {
	dir := os.Getenv("JEV_SHOWCASE_DIR")
	if dir == "" {
		t.Skip("set JEV_SHOWCASE_DIR to render the gallery")
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"OPENROUTER_API_KEY", "DEEPSEEK_API_KEY", "OPENCODE_GO_API_KEY", "EXA_API_KEY", "BRAVE_API_KEY"} {
		t.Setenv(key, "")
	}
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	cfg := config.Default()
	cfg.Roles = append(cfg.Roles, config.Role{Name: "direct", Provider: "deepseek", Model: "deepseek-chat", Description: "Tasks explicitly assigned to the direct DeepSeek provider."})
	cfg.Roles = append(cfg.Roles, config.Role{Name: "go", Provider: "opencode-go", Model: "kimi-k2.7-code", Description: "Tasks explicitly assigned to OpenCode Go. Demo catalog entry, not a recommendation."})
	source := t.TempDir()
	if err := os.WriteFile(filepath.Join(source, "example.go"), []byte("package example\n\nconst Greeting = \"hello\"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	client := openrouter.New("")
	newDemo := func() chatModel {
		ag := agent.New(client, router.New(client, cfg), cfg, source)
		t.Cleanup(ag.Cleanup)
		c := newChat(ag, cfg, "/demo/throwaway-project")
		c.resize(112, 34)
		return c
	}
	write := func(name, view string) {
		for _, line := range strings.Split(view, "\n") {
			if lipgloss.Width(line) > 112 {
				t.Fatalf("%s exceeds 112 columns", name)
			}
		}
		if lipgloss.Height(view) > 34 {
			t.Fatalf("%s exceeds 34 rows", name)
		}
		if err := os.WriteFile(filepath.Join(dir, name+".ansi"), []byte(view), 0644); err != nil {
			t.Fatal(err)
		}
	}
	chat := func(name string, setup func(*chatModel)) {
		c := newDemo()
		setup(&c)
		c.resize(112, 34)
		write(name, c.View())
	}
	route := func(c *chatModel, role int, src router.Source, confidence float64, prompt, reply string) {
		c.appendTranscript(c.userBlock(prompt) + "\n")
		d := router.Decision{Role: cfg.Roles[role], Source: src, Confidence: confidence}
		c.lastDec = &d
		c.appendTranscript(routeStyle.Render(routeLine(d)) + "\n")
		c.appendTranscript(mdHighlight(reply))
	}
	settings := func(name string, setup func(*settingsModel)) {
		c := newDemo()
		s := newSettings(c.ag, cfg, 112, 34)
		setup(&s)
		write(name, s.View())
	}
	chat("01-welcome", func(c *chatModel) {})
	chat("02-auto-routing-and-code", func(c *chatModel) {
		route(c, 0, router.SourceJev, .94, "Explain this Go function.", "It returns the greeting without changing any files.\n\n```go\nfunc Greeting() string {\n    return \"hello\"\n}\n```")
	})
	chat("03-complex-task-routing", func(c *chatModel) {
		route(c, 1, router.SourceJev, .88, "Find why retries duplicate writes across the client and worker.", "I will inspect the request IDs and retry loop before proposing changes.\n\n- Check whether a retry reuses the same request ID.\n- Check which failures trigger another write.")
	})
	chat("04-confidence-fallback", func(c *chatModel) {
		route(c, 0, router.SourceThreshold, .38, "Can you look at this?", "Which file or behaviour should I inspect?")
	})
	chat("05-pinned-role", func(c *chatModel) {
		c.ag.Pinned = "complex"
		route(c, 1, router.SourcePinned, 0, "Review the cancellation path.", "I will trace cancellation through the worker and the session checkpoint.")
	})
	chat("06-tool-approval", func(c *chatModel) {
		route(c, 0, router.SourceJev, .91, "Run the tests in this disposable workspace.", "The local shell has normal host and network access.")
		next, _ := c.handleEvent(agent.Event{Kind: agent.ToolCall, ToolName: "bash", ToolArgs: `{"command":"go test ./..."}`, Approve: make(chan bool, 1)})
		*c = next
	})
	chat("07-tool-output", func(c *chatModel) {
		route(c, 0, router.SourceJev, .91, "Run the example tests.", "")
		c.appendTranscript(c.toolBox(toolHeader("bash", `{"command":"go test ./..."}`), "ok  example  0.012s\n[exit 0; changes staged, use /changes and /apply]"))
	})
	chat("08-roles-command", func(c *chatModel) { next, _ := c.slash("/roles"); *c = next })
	settings("09-settings-overview", func(s *settingsModel) { s.rowCursor = 5 })
	settings("10-edit-role-criteria", func(s *settingsModel) { next, _ := s.openRoleForm(1); *s = next; s.focus = 2 })
	settings("11-add-role", func(s *settingsModel) {
		next, _ := s.openRoleForm(-1)
		*s = next
		s.inputs[0].SetValue("research")
		s.inputs[1].SetValue("deepseek-chat")
		s.inputs[2].SetValue("Research with configured web search, then cite source URLs.")
		s.inputs[3].SetValue("deepseek")
	})
	settings("12-routing-settings", func(s *settingsModel) { s.switchTab(settingsRouting); s.rowCursor = 1 })
	settings("13-status-line-settings", func(s *settingsModel) { s.rowCursor = 3 })
	settings("14-settings-shortcuts", func(s *settingsModel) { s.mode = sHelp })
	chat("15-multiline-input", func(c *chatModel) {
		c.cfg.ChatInputLines = true
		c.configureInput()
		c.ta.SetValue("Inspect the retry loop first.\nKeep the current request ID on retries.\nPropose the edit before running a command.")
	})
	chat("16-yolo-mode", func(c *chatModel) {
		next, _ := c.slash("/mode autonomous")
		*c = next
	})
	chat("17-command-suggestions", func(c *chatModel) { c.ta.SetValue("/mo") })
	chat("18-saved-sessions", func(c *chatModel) {
		c.sessionPicker = true
		c.sessionCursor = 1
		c.sessionList = []session.Session{{ID: "demo-session", Title: "Inspect retries", Updated: time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)}, {ID: "demo-other", Title: "Explain the worker", Updated: time.Date(2026, 9, 30, 11, 0, 0, 0, time.UTC)}}
	})
	chat("19-session-stats", func(c *chatModel) {
		c.sessionID = "demo-session"
		c.sessionTitle = "Illustrative usage"
		c.turns = 2
		c.recordUsage("openrouter:"+cfg.Roles[0].Model, &openrouter.Usage{PromptTokens: 1200, CompletionTokens: 240, Cost: .001, CostKnown: true})
		c.recordUsage("brave:web_search", &openrouter.Usage{})
		c.appendTranscript(c.sessionStats())
	})
	settings("20-context-settings", func(s *settingsModel) { s.switchTab(settingsContext) })
	chat("21-context-compaction", func(c *chatModel) {
		route(c, 1, router.SourcePinned, 0, "Continue with the retry analysis.", "The earlier context summary keeps the task and the pending checks.")
		next, _ := c.handleEvent(agent.Event{Kind: agent.Compacted, Text: "Context compacted: older messages replaced with a smaller summary."})
		*c = next
	})
	settings("22-provider-roles", func(s *settingsModel) { s.switchTab(settingsRoles) })
	chat("23-direct-provider", func(c *chatModel) {
		c.ag.Pinned = "direct"
		route(c, 2, router.SourcePinned, 0, "Explain the worker loop.", "The worker reads queued tasks and processes one task at a time.")
	})
	settings("24-provider-settings", func(s *settingsModel) {
		s.switchTab(settingsProviders)
		s.cfg.SearchProvider = "brave"
		s.rowCursor = 5
	})
	settings("25-search-provider", func(s *settingsModel) { s.switchTab(settingsProviders); s.mode = sSearchProvider; s.choiceCursor = 1 })
	settings("26-compact-command-output", func(s *settingsModel) { s.cfg.CompactCommandOutput = true; s.rowCursor = 6 })
	// These two captures exercise actual staged review/apply on disposable files.
	c := newDemo()
	if err := c.ensureSession(); err != nil {
		t.Fatal(err)
	}
	w, err := c.ag.Workspace()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(w.Stage, "example.go"), []byte("package example\n\nconst Greeting = \"hello, world\"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	c, _, _ = c.safetyCommand("/changes")
	if c.statusIsErr {
		t.Fatal(c.status)
	}
	write("27-staged-changes", c.View())
	c, _, _ = c.safetyCommand("/apply")
	if c.statusIsErr {
		t.Fatal(c.status)
	}
	write("28-reviewed-apply", c.View())
	chat("29-recovery", func(c *chatModel) {
		c.recovery = true
		c.appendTranscript("Interrupted session: inspect /changes and acknowledge with /recover. Pending tools will not be replayed.\n")
		c.ta.SetValue("Continue the task")
		next, _ := c.submit()
		*c = next
	})
	chat("30-wheel-scrolling", func(c *chatModel) {
		var rows [][]string
		for i := 1; i <= 40; i++ {
			rows = append(rows, []string{fmt.Sprint(i), "Demo transcript entry. No command was executed."})
		}
		c.appendTranscript(c.commandTable("Demo transcript", []string{"Entry", "Text"}, rows))
		c.vp.GotoBottom()
		bottom := c.vp.YOffset()
		c.ta.SetValue("Keep this draft while reviewing earlier output.")
		for range 3 {
			next, _ := c.Update(tea.MouseWheelMsg{Button: tea.MouseWheelUp})
			*c = next
		}
		if c.vp.YOffset() >= bottom || c.ta.Value() != "Keep this draft while reviewing earlier output." {
			t.Fatal("wheel capture did not scroll or preserve the draft")
		}
	})
}
