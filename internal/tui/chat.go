package tui

import (
	"context"
	"strings"
	"time"

	"jevharness/internal/agent"
	"jevharness/internal/config"
	"jevharness/internal/router"
	"jevharness/internal/session"

	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// eventMsg carries one agent.Event into Update.
type eventMsg struct{ ev agent.Event }
type chanClosedMsg struct{}
type tickMsg struct{}
type modelContextMsg struct {
	model      string
	length     int
	generation int
}

// waitEvent is the channel-to-tea.Cmd bridge: returns the next agent event,
// or chanClosedMsg when the turn ends.
func waitEvent(ch <-chan agent.Event) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return chanClosedMsg{}
		}
		return eventMsg{ev}
	}
}

func tick() tea.Cmd {
	return tea.Tick(150*time.Millisecond, func(time.Time) tea.Msg { return tickMsg{} })
}

type chatModel struct {
	ag                *agent.Agent
	cfg               config.Config
	vp                viewport.Model
	ta                textarea.Model
	events            <-chan agent.Event
	cancel            context.CancelFunc
	spin              int // spinner frame
	lastDec           *router.Decision
	status            string // transient message shown in the status line
	statusIsErr       bool
	transcript        *strings.Builder
	pending           *strings.Builder // raw assistant text of the in-flight response
	cwd               string
	pendingTool       string // tool header held until ToolResult renders the box
	approval          chan bool
	approveText       string
	models            map[string]session.ModelUsage
	tokensIn          int
	tokensOut         int
	cost              float64
	turns             int
	contextUsed       int
	contextModel      string
	contextLengths    map[string]int
	contextRequested  map[string]bool
	contextGeneration int
	yolo              bool // auto-approve tool calls (/yolo)
	store             session.Store
	sessionID         string
	sessionTitle      string
	sessionUpdated    time.Time
	sessionList       []session.Session
	sessionPicker     bool
	sessionCursor     int
	commandRunning    string
	commandReported   bool
	commandCursor     int
	commandDismissed  bool
	reviewed          string
	recovery          bool
	followups         []string
	quitting          bool
	w, h              int
}

func newChat(ag *agent.Agent, cfg config.Config, cwd string) chatModel {
	ta := textarea.New()
	ta.Prompt = ""
	ta.Placeholder = "Ask anything, or / for a command"
	ta.SetHeight(3)
	ta.ShowLineNumbers = false
	styles := ta.Styles()
	styles.Focused.CursorLine = lipgloss.NewStyle()
	styles.Focused.EndOfBuffer = lipgloss.NewStyle().Foreground(lipgloss.Color("236"))
	styles.Focused.Placeholder = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	ta.SetStyles(styles)
	ta.Focus()

	vp := viewport.New()
	vp.SoftWrap = true
	c := chatModel{ag: ag, cfg: cfg, cwd: cwd, ta: ta, vp: vp,
		store:      session.DefaultStore(),
		transcript: new(strings.Builder), pending: new(strings.Builder),
		contextLengths: make(map[string]int), contextRequested: make(map[string]bool)}
	c.configureInput()
	return c
}

func (c *chatModel) setConfig(cfg config.Config) {
	c.cfg = cfg
	c.contextGeneration++
	if c.lastDec != nil {
		if role, ok := cfg.RoleByName(c.lastDec.Role.Name); ok {
			decision := *c.lastDec
			decision.Role = role
			c.lastDec = &decision
		} else {
			c.lastDec = nil
		}
	}
	c.contextRequested = make(map[string]bool)
	c.contextLengths = make(map[string]int)
	c.configureInput()
	if c.w > 0 {
		c.resize(c.w, c.h)
	}
}

func (c *chatModel) configureInput() {
	if c.cfg.ChatInputLines {
		c.ta.DynamicHeight = true
		c.ta.MinHeight = 1
		c.ta.MaxHeight = 4
		// Keep long drafts scrollable after the visible input reaches four rows.
		c.ta.MaxContentHeight = 10000
		c.ta.SetWidth(max(1, c.w-4))
		return
	}
	c.ta.DynamicHeight = false
	c.ta.MaxHeight = 99
	c.ta.MaxContentHeight = 0
	c.ta.SetHeight(3)
}

func (c *chatModel) resize(w, h int) {
	c.w, c.h = w, h
	c.ta.SetWidth(max(1, w-4)) // border(2) + inner padding(2)
	c.vp.SetWidth(w)
	// Reserve room for the approval panel as well as the input and status line.
	vh := h - c.ta.Height() - 3 - max(1, lipgloss.Height(c.thinkingLine()))
	if vh < 1 {
		vh = 1
	}
	c.vp.SetHeight(vh)
	c.refreshVP()
}

// appendTranscript adds a rendered block to the transcript and scrolls down.
func (c *chatModel) appendTranscript(s string) {
	c.transcript.WriteString(s)
	if !strings.HasSuffix(s, "\n") {
		c.transcript.WriteString("\n")
	}
	if c.transcript.Len() > 2<<20 {
		all := c.transcript.String()
		cut := len(all) - (1 << 20)
		if i := strings.IndexByte(all[cut:], '\n'); i >= 0 {
			cut += i + 1
		}
		c.transcript.Reset()
		c.transcript.WriteString(dimStyle.Render("… older output omitted …") + "\n")
		c.transcript.WriteString(all[cut:])
	}
	c.refreshVP()
}

func (c *chatModel) refreshVP() {
	w := c.vp.Width()
	if w <= 0 {
		w = 80
	}
	atBottom := c.vp.AtBottom()
	c.vp.SetContent(chatBanner(w) + c.transcript.String() + padText(mdHighlight(c.pending.String()), w))
	if atBottom {
		c.vp.GotoBottom()
	}
}

func (c chatModel) Update(msg tea.Msg) (chatModel, tea.Cmd) {
	switch m := msg.(type) {
	case tea.MouseWheelMsg:
		if c.sessionPicker {
			switch m.Button {
			case tea.MouseWheelUp:
				c.sessionCursor = max(0, c.sessionCursor-1)
			case tea.MouseWheelDown:
				c.sessionCursor = min(len(c.sessionList), c.sessionCursor+1)
			}
			return c, nil
		}
		var cmd tea.Cmd
		c.vp, cmd = c.vp.Update(m)
		return c, cmd
	case tea.KeyPressMsg:
		oldValue := c.ta.Value()
		oldSuggestions := c.commandSuggestionsView()
		updated, cmd := c.handleKey(m)
		updated.syncCommandInput(oldValue)
		if (c.approval != nil) != (updated.approval != nil) || oldSuggestions != updated.commandSuggestionsView() {
			updated.resize(updated.w, updated.h)
		}
		return updated, cmd
	case eventMsg:
		updated, cmd := c.handleEvent(m.ev)
		if c.approval != nil || updated.approval != nil {
			updated.resize(updated.w, updated.h)
		}
		return updated, cmd
	case diagnosticMsg:
		c.status = m.Text
		c.statusIsErr = m.Err != nil
		if m.Err != nil {
			c.status = m.Err.Error()
		}
		c.commandFeedback("/doctor")
		return c, nil
	case chanClosedMsg:
		if c.commandRunning != "" && !c.commandReported {
			c.status, c.statusIsErr = "No compaction needed; context unchanged", false
			c.commandFeedback(c.commandRunning)
		}
		c.commandRunning, c.commandReported = "", false
		c.events = nil
		c.recovery = c.recovery || c.ag.PendingTool() != ""
		if c.cancel != nil {
			c.cancel()
		}
		c.cancel = nil
		c.approval = nil
		c.approveText = ""
		c.resize(c.w, c.h)
		saved := c.saveSession()
		if c.quitting {
			if !saved {
				c.quitting = false
				return c, nil
			}
			return c, tea.Quit
		}
		c.followups = append(c.followups, c.ag.TakeFollowups()...)
		if len(c.followups) > 0 {
			next := c.followups[0]
			c.followups = c.followups[1:]
			c.ta.SetValue(next)
			return c.submit()
		}
		return c, nil
	case tickMsg:
		if c.running() {
			c.spin++
			c.refreshVP()
			return c, tick()
		}
		return c, nil
	case modelContextMsg:
		if m.generation != c.contextGeneration {
			return c, nil
		}
		if m.length > 0 {
			c.contextLengths[m.model] = m.length
		} else {
			delete(c.contextRequested, m.model)
		}
		return c, nil
	}
	// Pass asynchronous paste and cursor messages to the input as well.
	var viewportCmd, inputCmd tea.Cmd
	c.vp, viewportCmd = c.vp.Update(msg)
	if c.approval == nil && !c.sessionPicker {
		oldHeight := c.ta.Height()
		oldValue := c.ta.Value()
		oldSuggestions := c.commandSuggestionsView()
		c.ta, inputCmd = c.ta.Update(msg)
		c.syncCommandInput(oldValue)
		c.syncInputHeight(oldHeight)
		if oldSuggestions != c.commandSuggestionsView() {
			c.resize(c.w, c.h)
		}
	}
	return c, tea.Batch(viewportCmd, inputCmd)
}

func (c chatModel) running() bool { return c.events != nil }

func (c chatModel) handleKey(m tea.KeyPressMsg) (chatModel, tea.Cmd) {
	if c.sessionPicker {
		return c.sessionKey(m)
	}
	if c.approval != nil {
		switch m.String() {
		case "ctrl+c":
			return c.quit()
		case "esc":
			if c.cancel != nil {
				c.cancel()
			}
			c.status = "aborting…"
			return c, nil
		case "y", "Y", "n", "N", "enter":
			c.approval <- m.String() == "y" || m.String() == "Y"
			c.approval = nil
			c.approveText = ""
			return c, nil
		case "pgup", "pgdown", "shift+up", "shift+down":
			if m.String() == "shift+up" {
				c.vp.ScrollUp(1)
				return c, nil
			}
			if m.String() == "shift+down" {
				c.vp.ScrollDown(1)
				return c, nil
			}
			var cmd tea.Cmd
			c.vp, cmd = c.vp.Update(m)
			return c, cmd
		default:
			return c, nil
		}
	}
	if suggestions := c.commandSuggestions(); len(suggestions) > 0 {
		switch m.String() {
		case "up":
			c.commandCursor = (c.commandCursor + len(suggestions) - 1) % len(suggestions)
			return c, nil
		case "down":
			c.commandCursor = (c.commandCursor + 1) % len(suggestions)
			return c, nil
		case "esc":
			c.commandDismissed = true
			return c, nil
		case "tab", "enter":
			selected := suggestions[min(c.commandCursor, len(suggestions)-1)]
			// Enter still executes a fully typed command.
			if m.String() == "enter" && selected.name == strings.TrimSpace(c.ta.Value()) {
				return c.submit()
			}
			c.ta.SetValue(selected.name + " ")
			c.ta.CursorEnd()
			return c, nil
		}
	}
	switch m.String() {
	case "ctrl+c":
		return c.quit()
	case "esc":
		if c.running() && c.cancel != nil {
			c.cancel()
			c.status = "aborting…"
			return c, nil
		}
		return c, nil
	case "enter":
		if !c.running() {
			return c.submit()
		}
		text := strings.TrimSpace(c.ta.Value())
		if text != "" {
			if c.ag.Steer(text, false) {
				c.ta.Reset()
				c.status = "Steering queued for the next model request"
			} else {
				c.status = "Steering queue is full"
			}
		}
		return c, nil
	case "alt+enter":
		if c.running() {
			text := strings.TrimSpace(c.ta.Value())
			if text != "" && c.ag.Steer(text, true) {
				c.ta.Reset()
				c.status = "Follow-up queued"
			}
			return c, nil
		}
		fallthrough
	case "shift+enter":
		oldHeight := c.ta.Height()
		c.ta.InsertString("\n")
		c.syncInputHeight(oldHeight)
		return c, nil
	case "pgup", "pgdown", "shift+up", "shift+down":
		if m.String() == "shift+up" {
			c.vp.ScrollUp(1)
			return c, nil
		}
		if m.String() == "shift+down" {
			c.vp.ScrollDown(1)
			return c, nil
		}
		var cmd tea.Cmd
		c.vp, cmd = c.vp.Update(m)
		return c, cmd
	case "tab":
		if c.running() {
			return c, nil
		}
		c.cycleRole()
		c.status = ""
		c.statusIsErr = false
		return c, c.requestContext()
	}
	var cmd tea.Cmd
	oldHeight := c.ta.Height()
	c.ta, cmd = c.ta.Update(m)
	c.syncInputHeight(oldHeight)
	return c, cmd
}

func (c *chatModel) syncInputHeight(oldHeight int) {
	if c.ta.Height() != oldHeight && c.w > 0 {
		c.resize(c.w, c.h)
	}
}

// cycleRole steps the pinned role forward: auto → each role in order → auto.
func (c *chatModel) cycleRole() {
	if c.ag.Pinned == "" {
		if len(c.cfg.Roles) > 0 {
			c.ag.Pinned = c.cfg.Roles[0].Name
		}
		return
	}
	for i, r := range c.cfg.Roles {
		if r.Name == c.ag.Pinned {
			if i+1 < len(c.cfg.Roles) {
				c.ag.Pinned = c.cfg.Roles[i+1].Name
			} else {
				c.ag.Pinned = ""
			}
			return
		}
	}
	c.ag.Pinned = "" // pinned role no longer exists
}

func (c chatModel) submit() (chatModel, tea.Cmd) {
	text := strings.TrimSpace(c.ta.Value())
	if text == "" {
		return c, nil
	}
	oldHeight := c.ta.Height()
	c.ta.Reset()
	c.syncInputHeight(oldHeight)
	c.status = ""
	c.statusIsErr = false

	if c.recovery && !strings.HasPrefix(text, "/") {
		c.status, c.statusIsErr = "Interrupted work requires /changes and /recover before continuing", true
		c.ta.SetValue(text)
		return c, nil
	}
	if strings.HasPrefix(text, "/") {
		return c.slash(text)
	}
	if c.sessionID == "" {
		id, err := session.NewID()
		if err != nil {
			c.status, c.statusIsErr = "create session: "+err.Error(), true
			return c, nil
		}
		c.sessionID = id
		c.sessionTitle = truncateRunes(strings.Join(strings.Fields(text), " "), 80)
	}
	c.ag.SetSessionID(c.sessionID)
	c.sessionUpdated = time.Now()
	for _, token := range strings.Fields(text) {
		if strings.HasPrefix(token, "@") && len(token) > 1 {
			attachment, err := c.ag.Attach(strings.TrimPrefix(token, "@"))
			if err != nil {
				c.status, c.statusIsErr = err.Error(), true
				c.ta.SetValue(text)
				return c, nil
			}
			text += "\n\n" + attachment
		}
	}
	c.ag.SetPersistence(c.snapshot())
	c.reviewed = ""

	c.appendTranscript("\n" + c.userBlock(text) + "\n\n")
	ctx, cancel := context.WithCancel(context.Background())
	c.cancel = cancel
	c.events = c.ag.Submit(ctx, text)
	return c, tea.Batch(waitEvent(c.events), tick())
}
