// Package agent runs the turn loop: route -> chat stream -> tool calls ->
// chat stream, emitting events the TUI renders.
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"jevharness/internal/config"
	"jevharness/internal/openrouter"
	"jevharness/internal/router"
	"jevharness/internal/session"
	"jevharness/internal/tools"
)

const maxRounds = 25

// Kind identifies an Event.
type Kind int

const (
	Routed            Kind = iota // routing decision made
	TextDelta                     // assistant text fragment
	ToolCall                      // model requested a tool
	ToolResult                    // tool finished
	TurnDone                      // turn complete
	Error                         // turn failed or aborted
	Compacting                    // summarising earlier context
	Compacted                     // summary committed
	CompactionWarning             // compaction unavailable; history preserved
	UsageRecorded                 // reported usage for one API request
)

// Event is streamed to the TUI during a turn.
type Event struct {
	Kind     Kind
	Text     string // delta, tool output, or error message
	ToolName string
	ToolArgs string    // raw JSON, for display
	Approve  chan bool // present when a tool needs user approval
	Decision *router.Decision
	Usage    *openrouter.Usage
	Model    string // model billed for UsageRecorded
	// ContextTokens is the prompt size of the last request in the turn.
	ContextTokens int
	// Duration is the response wall time on TurnDone (all chat rounds).
	Duration time.Duration
}

// Agent holds conversation state and runs turns.
type Agent struct {
	opencodeGo *openrouter.Client
	deepseek   *openrouter.Client
	client     *openrouter.Client
	router     *router.Router
	cfg        config.Config
	cwd        string
	// Pinned is a role name or "" for auto routing.
	Pinned         string
	contextLengths map[string]int
	tokenRatios    map[string]float64
	msgs           []openrouter.Message // starts with the system message
}

// New creates an Agent whose history begins with the system prompt.
func New(client *openrouter.Client, r *router.Router, cfg config.Config, cwd string) *Agent {
	a := &Agent{client: client, deepseek: openrouter.NewDeepSeek(cfg.ProviderKey("deepseek")), opencodeGo: openrouter.NewOpenCodeGo(cfg.ProviderKey("opencode-go")), router: r, cfg: cfg, cwd: cwd}
	a.Clear()
	return a
}

func (a *Agent) system() openrouter.Message {
	return openrouter.Message{
		Role: "system",
		Content: fmt.Sprintf(
			"You are a coding agent running in the user's terminal, working directory %s. "+
				"Use the tools to inspect and change files and run commands. "+
				"Prefer reading before editing. Keep replies short.", a.cwd),
	}
}

// SetConfig swaps in a new config after a settings save and rebuilds the
// router so routing criteria reflect it immediately.
func (a *Agent) SetConfig(cfg config.Config) {
	a.cfg = cfg
	if a.client != nil {
		a.client.SetKey(cfg.Key())
	}
	a.deepseek.SetKey(cfg.ProviderKey("deepseek"))
	a.opencodeGo.SetKey(cfg.ProviderKey("opencode-go"))
	a.contextLengths = make(map[string]int)
	a.router = router.New(a.client, cfg)
	if a.Pinned != "" {
		if _, ok := cfg.RoleByName(a.Pinned); !ok {
			a.Pinned = ""
		}
	}
}

// Clear resets history to just the system prompt (/clear).
func (a *Agent) Clear() {
	a.msgs = []openrouter.Message{a.system()}
	id, err := session.NewID()
	if err != nil {
		panic("create provider session ID: " + err.Error())
	}
	a.SetSessionID(id)
	a.contextLengths = make(map[string]int)
	a.tokenRatios = make(map[string]float64)
}

// History returns a detached snapshot. Call only after the turn channel closes.
func (a *Agent) History() []openrouter.Message {
	msgs := append([]openrouter.Message(nil), a.msgs[1:]...)
	for i := range msgs {
		msgs[i] = cloneMessage(msgs[i])
	}
	return msgs
}

// Restore resumes history with the current system prompt and configuration.
// Call only while no turn is running.
func (a *Agent) Restore(msgs []openrouter.Message, pinned string) {
	a.Clear()
	for _, m := range msgs {
		if m.Role != "system" {
			m = cloneMessage(m)
			a.msgs = append(a.msgs, m)
		}
	}
	a.completeToolResults()
	a.Pinned = ""
	if _, ok := a.cfg.RoleByName(pinned); ok {
		a.Pinned = pinned
	}
}

// ContextLookup captures the client while idle, before a background lookup.
// The returned function only touches the client's synchronised state.
func (a *Agent) ContextLookup(role config.Role) func(context.Context) (int, error) {
	client := a.chatClient(role)
	return func(ctx context.Context) (int, error) {
		if client == nil {
			return 0, fmt.Errorf("provider client is unavailable")
		}
		return client.ContextLength(ctx, role.Model)
	}
}

func cloneMessage(m openrouter.Message) openrouter.Message {
	m.ToolCalls = append([]openrouter.ToolCall(nil), m.ToolCalls...)
	items := make([]json.RawMessage, len(m.NativeItems))
	for i, item := range m.NativeItems {
		items[i] = append(json.RawMessage(nil), item...)
	}
	if m.NativeItems != nil {
		m.NativeItems = items
	}
	return m
}

// recentTurns extracts up to 6 user/assistant text messages, each truncated
// to 400 runes, oldest first, for the Jev state.
func (a *Agent) recentTurns() []router.Turn {
	var turns []router.Turn
	for _, m := range a.msgs {
		if m.Role != "user" && m.Role != "assistant" {
			continue
		}
		if m.Content == "" {
			continue
		}
		text := m.Content
		if r := []rune(text); len(r) > 400 {
			text = string(r[:400])
		}
		turns = append(turns, router.Turn{Role: m.Role, Text: text})
	}
	if len(turns) > 6 {
		turns = turns[len(turns)-6:]
	}
	return turns
}

// Submit runs one turn in a goroutine and closes the returned channel when
// the turn ends.
func (a *Agent) Submit(ctx context.Context, text string) <-chan Event {
	ch := make(chan Event, 64)
	go func() {
		defer close(ch)
		a.run(ctx, text, ch)
	}()
	return ch
}

// emit delivers a streamable event; returns false if ctx is done. Use for
// high-frequency events (text deltas) where dropping is fine on abort.
func emit(ctx context.Context, ch chan<- Event, ev Event) bool {
	select {
	case ch <- ev:
		return true
	case <-ctx.Done():
		return false
	}
}

// send delivers an event unconditionally. Use for terminal/sparse events the
// UI must observe even after cancellation (Routed, ToolCall, ToolResult,
// TurnDone, Error): the consumer drains the channel until close.
func send(ch chan<- Event, ev Event) { ch <- ev }

func (a *Agent) run(ctx context.Context, text string, ch chan<- Event) {
	defer a.completeToolResults()
	recent := a.recentTurns()
	a.msgs = append(a.msgs, openrouter.Message{Role: "user", Content: text})

	var dec router.Decision
	if a.Pinned != "" {
		if role, ok := a.cfg.RoleByName(a.Pinned); ok {
			dec = router.Decision{Role: role, Source: router.SourcePinned, Confidence: 1}
		} else {
			// pinned role was deleted; fall back to routing
			dec = a.router.Route(ctx, router.State{Message: text, RecentTurns: recent})
		}
	} else {
		dec = a.router.Route(ctx, router.State{Message: text, RecentTurns: recent})
	}
	send(ch, Event{Kind: Routed, Decision: &dec})
	if dec.Usage != nil {
		send(ch, Event{Kind: UsageRecorded, Model: dec.Model, Usage: dec.Usage})
	}
	if dec.Role.Name == "" {
		send(ch, Event{Kind: Error, Text: "routing failed: no role available"})
		return
	}

	chatClient := a.chatClient(dec.Role)
	cacheKey := dec.Role.Backend() + ":" + dec.Role.Model
	defs := tools.All(a.cwd)
	toolDefs := make([]openrouter.ToolDef, len(defs))
	for i, t := range defs {
		toolDefs[i] = t.Def
	}

	var total openrouter.Usage
	var turnDuration time.Duration
	var contextTokens int
	metadataCtx, cancelMetadata := context.WithTimeout(ctx, 5*time.Second)
	window := a.contextLengths[cacheKey]
	if a.cfg.CompactionThreshold > 0 && window == 0 {
		var err error
		window, err = chatClient.ContextLength(metadataCtx, dec.Role.Model)
		if err != nil && ctx.Err() == nil {
			send(ch, Event{Kind: CompactionWarning, Text: "Context window unavailable; automatic compaction skipped: " + err.Error()})
		}
		a.contextLengths[cacheKey] = window
	}
	cancelMetadata()
	for round := 0; round < maxRounds; round++ {
		if err := a.compactWithClient(ctx, chatClient, dec.Role.Model, window, toolDefs, ch); err != nil {
			send(ch, Event{Kind: Error, Text: "compaction: " + err.Error()})
			return
		}
		promptEstimate := estimateTokens(a.msgs, toolDefs)
		stream, err := chatClient.ChatStream(ctx, openrouter.ChatRequest{
			Model:    dec.Role.Model, // every request in the turn uses the routed model
			Messages: a.msgs,
			Tools:    toolDefs,
		})
		if err != nil {
			msg := "chat: " + err.Error()
			if ctx.Err() != nil {
				msg = "aborted"
			}
			send(ch, Event{Kind: Error, Text: msg})
			return
		}

		var (
			content        strings.Builder
			calls          []openrouter.ToolCall
			finish         string
			usage          *openrouter.Usage
			sErr           error
			started        time.Time
			nativeProvider string
			nativeItems    []json.RawMessage
			reasoning      string
		)
		for ev := range stream {
			if ev.TextDelta != "" {
				content.WriteString(ev.TextDelta)
				if !emit(ctx, ch, Event{Kind: TextDelta, Text: ev.TextDelta}) {
					// Drain the cancelled stream so its terminal send cannot leak a goroutine.
					for terminal := range stream {
						if terminal.Done && terminal.Usage != nil {
							send(ch, Event{Kind: UsageRecorded, Model: dec.Role.Model, Usage: terminal.Usage})
						}
					}
					// aborted mid-delta: keep the partial reply
					a.msgs = append(a.msgs, openrouter.Message{Role: "assistant", Content: content.String()})
					send(ch, Event{Kind: Error, Text: "aborted"})
					return
				}
			}
			if ev.Done {
				calls, finish, usage, sErr, started = ev.ToolCalls, ev.Finish, ev.Usage, ev.Err, ev.Started
				nativeProvider, nativeItems, reasoning = ev.NativeProvider, ev.NativeItems, ev.ReasoningContent
			}
		}
		if usage != nil {
			send(ch, Event{Kind: UsageRecorded, Model: dec.Role.Model, Usage: usage})
		}
		if !started.IsZero() {
			turnDuration += time.Since(started)
		}

		// Keep whatever text arrived, even on error/abort. Incomplete tool
		// calls must not enter history without matching tool results.
		if sErr != nil {
			calls = nil
			nativeProvider, nativeItems, reasoning = "", nil, ""
		}
		a.msgs = append(a.msgs, openrouter.Message{
			Role:           "assistant",
			Content:        content.String(),
			ToolCalls:      calls,
			NativeProvider: nativeProvider, NativeItems: nativeItems, ReasoningContent: reasoning,
		})

		if sErr != nil {
			msg := sErr.Error()
			if ctx.Err() != nil {
				msg = "aborted"
			}
			send(ch, Event{Kind: Error, Text: msg})
			return
		}
		if usage != nil {
			contextTokens = usage.PromptTokens
			if usage.PromptTokens > 0 && promptEstimate > 0 {
				a.tokenRatios[chatClient.ModelKey(dec.Role.Model)] = float64(usage.PromptTokens) / float64(promptEstimate)
			}
			total.PromptTokens += usage.PromptTokens
			total.CompletionTokens += usage.CompletionTokens
			total.TotalTokens += usage.TotalTokens
			total.Cost += usage.Cost
		}
		if len(calls) == 0 && finish != "tool_calls" {
			var turnUsage *openrouter.Usage
			if total.TotalTokens > 0 || total.Cost > 0 {
				turnUsage = &total
			}
			send(ch, Event{Kind: TurnDone, Usage: turnUsage, ContextTokens: contextTokens, Duration: turnDuration})
			return
		}

		for _, c := range calls {
			approval := make(chan bool, 1)
			send(ch, Event{Kind: ToolCall, ToolName: c.Function.Name, ToolArgs: c.Function.Arguments, Approve: approval})
			out := "error: unknown tool"
			var approved bool
			select {
			case approved = <-approval:
			case <-ctx.Done():
				send(ch, Event{Kind: Error, Text: "aborted"})
				return
			}
			if ctx.Err() != nil {
				send(ch, Event{Kind: Error, Text: "aborted"})
				return
			}
			if !approved {
				out = "error: tool call declined by user"
			} else if t, ok := tools.Find(a.cwd, c.Function.Name); ok {
				var rerr error
				out, rerr = t.Run(ctx, json.RawMessage(c.Function.Arguments))
				if rerr != nil {
					out = "error: " + rerr.Error()
				}
			}
			a.msgs = append(a.msgs, openrouter.Message{
				Role:       "tool",
				ToolCallID: c.ID,
				Content:    out,
			})
			send(ch, Event{Kind: ToolResult, ToolName: c.Function.Name, Text: out})
		}
	}
	send(ch, Event{Kind: Error, Text: "stopped after 25 tool rounds"})
}

// completeToolResults keeps aborted turns valid for the next API request.
func (a *Agent) completeToolResults() {
	pending := make(map[string]bool)
	for _, m := range a.msgs {
		for _, call := range m.ToolCalls {
			pending[call.ID] = true
		}
		if m.Role == "tool" {
			delete(pending, m.ToolCallID)
		}
	}
	for _, m := range a.msgs {
		for _, call := range m.ToolCalls {
			if pending[call.ID] {
				a.msgs = append(a.msgs, openrouter.Message{Role: "tool", ToolCallID: call.ID, Content: "error: tool call aborted"})
				delete(pending, call.ID)
			}
		}
	}
}

func (a *Agent) chatClient(role config.Role) *openrouter.Client {
	if role.Backend() == "opencode-go" {
		return a.opencodeGo
	}
	if role.Backend() == "deepseek" {
		return a.deepseek
	}
	return a.client
}

// SetSessionID keeps Go routing and prompt caching stable across saved-session resumes.
func (a *Agent) SetSessionID(id string) { a.opencodeGo.SetSessionID(id) }

func (a *Agent) ContextLength(ctx context.Context, model string) (int, error) {
	return a.client.ContextLength(ctx, model)
}
