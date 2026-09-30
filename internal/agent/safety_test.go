package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"jevharness/internal/config"
	"jevharness/internal/openrouter"
	"jevharness/internal/router"
	"jevharness/internal/session"
	"jevharness/internal/tools"
)

func testAgent(t *testing.T, handler http.HandlerFunc) (*Agent, *httptest.Server) {
	t.Helper()
	source := t.TempDir()
	_ = os.WriteFile(filepath.Join(source, "a.txt"), []byte("before"), 0644)
	s := httptest.NewServer(handler)
	cfg := config.Default()
	cfg.Roles = cfg.Roles[:1]
	cfg.Roles[0].ContextWindow = 10000
	cfg.Safety.Ephemeral = true
	cfg.Limits.OutputTokens = 64
	cfg.Limits.Seconds = 5
	c := openrouter.New("test-key")
	c.SetBaseURL(s.URL)
	a := New(c, router.New(c, cfg), cfg, source)
	t.Cleanup(a.Cleanup)
	return a, s
}
func toolResponse(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/event-stream")
	fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call-1\",\"type\":\"function\",\"function\":{\"name\":\"write_file\",\"arguments\":\"{\\\"path\\\":\\\"a.txt\\\",\\\"content\\\":\\\"after\\\"}\"}}]},\"finish_reason\":\"tool_calls\"}]}\n\ndata: [DONE]\n\n")
}
func finalResponse(w http.ResponseWriter) {
	fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"done\"},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":2,\"total_tokens\":12,\"cost\":0}}\n\ndata: [DONE]\n\n")
}
func TestStagedToolLoopAndModelProvenance(t *testing.T) {
	var calls atomic.Int32
	a, s := testAgent(t, func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			toolResponse(w)
		} else {
			var req openrouter.ChatRequest
			_ = json.NewDecoder(r.Body).Decode(&req)
			if len(req.Messages) < 4 || req.Messages[len(req.Messages)-1].Role != "tool" {
				t.Error("tool result not retained")
			}
			finalResponse(w)
		}
	})
	defer s.Close()
	_ = a.SetMode("autonomous")
	done := false
	for ev := range a.Submit(context.Background(), "change a.txt") {
		if ev.Approve != nil {
			t.Fatal("autonomous depends on UI bypass")
		}
		if ev.Kind == Error {
			t.Fatal(ev.Text)
		}
		done = done || ev.Kind == TurnDone
	}
	if !done || calls.Load() != 2 {
		t.Fatal("loop failed")
	}
	data, _ := os.ReadFile(filepath.Join(a.cwd, "a.txt"))
	if string(data) != "before" {
		t.Fatal("original changed")
	}
	w, err := a.Workspace()
	if err != nil {
		t.Fatal(err)
	}
	changes, err := w.Changes()
	if err != nil || len(changes) != 1 {
		t.Fatal("missing staged change", err)
	}
	history := a.History()
	for _, m := range history {
		if m.Role == "assistant" && m.Model == "" {
			t.Fatal("missing model provenance")
		}
	}
}
func TestMissingUsageReservesTokens(t *testing.T) {
	var calls atomic.Int32
	a, s := testAgent(t, func(w http.ResponseWriter, r *http.Request) { calls.Add(1); toolResponse(w) })
	defer s.Close()
	_ = a.SetMode("autonomous")
	if err := a.EnsureWorkspace(); err != nil {
		t.Fatal(err)
	}
	a.msgs[0] = a.system()
	msgs := append(a.msgs, openrouter.Message{Role: "user", Content: "change"})
	defs := tools.All(a.cwd)
	wire := make([]openrouter.ToolDef, len(defs))
	for i, d := range defs {
		wire[i] = d.Def
	}
	a.cfg.Limits.TotalTokens = estimateTokens(msgs, wire) + 64
	stopped := false
	for ev := range a.Submit(context.Background(), "change") {
		if ev.Kind == Error && strings.Contains(ev.Text, "budget") {
			stopped = true
		}
	}
	if !stopped || calls.Load() != 1 {
		t.Fatal("unreported usage bypassed budget", calls.Load())
	}
}
func TestInspectDeniesMutationAndAbortCompletesHistory(t *testing.T) {
	var calls atomic.Int32
	a, s := testAgent(t, func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			toolResponse(w)
		} else {
			finalResponse(w)
		}
	})
	defer s.Close()
	_ = a.SetMode("inspect")
	for ev := range a.Submit(context.Background(), "write a.txt") {
		if ev.Approve != nil {
			t.Fatal("inspect asks instead of denying")
		}
	}
	w, _ := a.Workspace()
	changes, _ := w.Changes()
	if len(changes) != 0 {
		t.Fatal("inspect changed stage")
	}
	calls.Store(0)
	_ = a.SetMode("develop")
	ctx, cancel := context.WithCancel(context.Background())
	for ev := range a.Submit(ctx, "write a.txt") {
		if ev.Kind == ToolCall {
			cancel()
		}
	}
	cancel()
	history := a.History()
	found := false
	for _, m := range history {
		if m.ToolCallID == "call-1" && strings.Contains(m.Content, "aborted") {
			found = true
		}
	}
	if !found {
		t.Fatal("aborted tool missing result")
	}
}
func TestDurableToolCheckpointAndEphemeral(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	a, s := testAgent(t, func(w http.ResponseWriter, r *http.Request) { finalResponse(w) })
	defer s.Close()
	a.cfg.Safety.Ephemeral = false
	id, _ := session.NewID()
	a.SetSessionID(id)
	a.SetPersistence(session.Session{ID: id, CWD: a.cwd, Title: "durable"})
	for ev := range a.Submit(context.Background(), "hello") {
		if ev.Kind == Error {
			t.Fatal(ev.Text)
		}
	}
	saved, err := session.DefaultStore().Load(id)
	if err != nil || saved.Running || len(saved.Messages) != 2 {
		t.Fatal("checkpoint failed", err)
	}
	events, err := session.DefaultStore().Events(id)
	if err != nil || len(events) < 3 {
		t.Fatal("journal missing", err)
	}
	a.cfg.Safety.Ephemeral = true
	other, _ := session.NewID()
	a.SetSessionID(other)
	a.SetPersistence(session.Session{ID: other, CWD: a.cwd})
	for range a.Submit(context.Background(), "ephemeral") {
	}
	if _, err = session.DefaultStore().Load(other); !os.IsNotExist(err) {
		t.Fatal("ephemeral persisted")
	}
}
func TestPermissionResetAndSteering(t *testing.T) {
	a, s := testAgent(t, func(w http.ResponseWriter, r *http.Request) { finalResponse(w) })
	defer s.Close()
	_ = a.SetMode("autonomous")
	a.Clear()
	if a.Mode() != "develop" {
		t.Fatal("elevated mode survived clear")
	}
	if !a.Steer("follow", true) || !a.Steer("steer", false) {
		t.Fatal("queue failed")
	}
	if !a.consumeSteering() || len(a.followups) != 1 {
		t.Fatal("steering/followup mixed")
	}
}

func TestContextOverflowCompactsAndRetriesWithoutReplayingTools(t *testing.T) {
	var requests atomic.Int32
	a, s := testAgent(t, func(w http.ResponseWriter, r *http.Request) {
		switch requests.Add(1) {
		case 1:
			w.WriteHeader(400)
			fmt.Fprint(w, `{"error":{"code":"context_length_exceeded","message":"maximum context length"}}`)
		case 2:
			fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"Earlier task completed.\"},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":2,\"total_tokens\":12,\"cost\":0}}\n\ndata: [DONE]\n\n")
		default:
			finalResponse(w)
		}
	})
	defer s.Close()
	a.msgs = append(a.msgs, openrouter.Message{Role: "user", Content: strings.Repeat("earlier work ", 200)}, openrouter.Message{Role: "assistant", Content: "old answer"})
	done := false
	for ev := range a.Submit(context.Background(), "continue") {
		if ev.Kind == Error {
			t.Fatal(ev.Text)
		}
		done = done || ev.Kind == TurnDone
	}
	if !done || requests.Load() != 3 {
		t.Fatal("overflow did not recover", requests.Load())
	}
}
