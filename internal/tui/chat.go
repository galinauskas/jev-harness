package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"jevharness/internal/agent"
	"jevharness/internal/config"
	"jevharness/internal/router"

	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// eventMsg carries one agent.Event into Update.
type eventMsg struct{ ev agent.Event }
type chanClosedMsg struct{}
type tickMsg struct{}
type modelContextMsg struct {
	model  string
	length int
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
	ag               *agent.Agent
	cfg              config.Config
	vp               viewport.Model
	ta               textarea.Model
	events           <-chan agent.Event
	cancel           context.CancelFunc
	spin             int // spinner frame
	lastDec          *router.Decision
	status           string // transient message shown in the status line
	statusIsErr      bool
	transcript       *strings.Builder
	pending          *strings.Builder // raw assistant text of the in-flight response
	cwd              string
	pendingTool      string // tool header held until ToolResult renders the box
	approval         chan bool
	approveText      string
	tokensIn         int
	tokensOut        int
	cost             float64
	turns            int
	contextUsed      int
	contextModel     string
	contextLengths   map[string]int
	contextRequested map[string]bool
	yolo             bool // auto-approve tool calls (/yolo)
	w, h             int
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
		transcript: new(strings.Builder), pending: new(strings.Builder),
		contextLengths: make(map[string]int), contextRequested: make(map[string]bool)}
	c.configureInput()
	return c
}

func (c *chatModel) setConfig(cfg config.Config) {
	c.cfg = cfg
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
	// The viewport, thinking line, bordered input and status line fill the screen.
	vh := h - c.ta.Height() - 4
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
	c.vp.SetContent(chatBanner(w) + c.transcript.String() + padText(safeText(c.pending.String()), w))
	if atBottom {
		c.vp.GotoBottom()
	}
}

func (c chatModel) Update(msg tea.Msg) (chatModel, tea.Cmd) {
	switch m := msg.(type) {
	case tea.KeyPressMsg:
		return c.handleKey(m)
	case eventMsg:
		return c.handleEvent(m.ev)
	case chanClosedMsg:
		c.events = nil
		if c.cancel != nil {
			c.cancel()
		}
		c.cancel = nil
		c.approval = nil
		c.approveText = ""
		return c, nil
	case tickMsg:
		if c.running() {
			c.spin++
			c.refreshVP()
			return c, tick()
		}
		return c, nil
	case modelContextMsg:
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
	if c.approval == nil {
		oldHeight := c.ta.Height()
		c.ta, inputCmd = c.ta.Update(msg)
		c.syncInputHeight(oldHeight)
	}
	return c, tea.Batch(viewportCmd, inputCmd)
}

func (c chatModel) running() bool { return c.events != nil }

func (c chatModel) handleKey(m tea.KeyPressMsg) (chatModel, tea.Cmd) {
	if c.approval != nil {
		switch m.String() {
		case "ctrl+c":
			if c.cancel != nil {
				c.cancel()
			}
			return c, tea.Quit
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
	switch m.String() {
	case "ctrl+c":
		if c.cancel != nil {
			c.cancel()
		}
		return c, tea.Quit
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
		return c, nil
	case "shift+enter", "alt+enter":
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

func (c *chatModel) activeModel() string {
	if c.ag.Pinned != "" {
		if role, ok := c.cfg.RoleByName(c.ag.Pinned); ok {
			return role.Model
		}
	}
	if c.lastDec != nil {
		return c.lastDec.Role.Model
	}
	if role, ok := c.cfg.RoleByName(c.cfg.DefaultRole); ok {
		return role.Model
	}
	return ""
}

func (c *chatModel) requestContext() tea.Cmd {
	model := c.activeModel()
	if model == "" || c.contextRequested[model] {
		return nil
	}
	c.contextRequested[model] = true
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		length, _ := c.ag.ContextLength(ctx, model)
		return modelContextMsg{model: model, length: length}
	}
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

	if strings.HasPrefix(text, "/") {
		return c.slash(text)
	}

	c.appendTranscript("\n" + c.userBlock(text) + "\n\n")
	ctx, cancel := context.WithCancel(context.Background())
	c.cancel = cancel
	c.events = c.ag.Submit(ctx, text)
	return c, tea.Batch(waitEvent(c.events), tick())
}

// slash handles commands before submit. Returns the model and cmd.
func (c chatModel) slash(text string) (chatModel, tea.Cmd) {
	parts := strings.Fields(text)
	switch parts[0] {
	case "/settings":
		return c, func() tea.Msg { return openSettingsMsg{} }
	case "/quit":
		return c, tea.Quit
	case "/clear":
		c.ag.Clear()
		c.transcript.Reset()
		c.pending.Reset()
		c.lastDec = nil
		c.tokensIn, c.tokensOut, c.cost, c.turns = 0, 0, 0, 0
		c.contextUsed, c.contextModel = 0, ""
		c.refreshVP()
		c.status = "cleared"
		return c, nil
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

func (c chatModel) handleEvent(ev agent.Event) (chatModel, tea.Cmd) {
	next := waitEvent(c.events)
	switch ev.Kind {
	case agent.Routed:
		unchanged := c.lastDec != nil &&
			c.lastDec.Role.Name == ev.Decision.Role.Name &&
			c.lastDec.Role.Model == ev.Decision.Role.Model
		c.lastDec = ev.Decision
		if !unchanged {
			c.appendTranscript(padText(routeStyle.Render(routeLine(*ev.Decision))+"\n", c.w) + "\n")
		}
		return c, tea.Batch(next, c.requestContext())
	case agent.TextDelta:
		c.pending.WriteString(ev.Text)
	case agent.ToolCall:
		c.flushPending()
		// hold the header; the box is rendered when the result arrives
		c.pendingTool = toolHeader(safeText(ev.ToolName), truncateRunes(safeText(ev.ToolArgs), 300))
		if c.yolo && ev.Approve != nil {
			ev.Approve <- true
			break
		}
		c.approval = ev.Approve
		c.approveText = strings.Join(strings.Fields(safeText(ev.ToolName)+" "+safeText(ev.ToolArgs)), " ")
		if c.approval != nil {
			c.appendTranscript(padText(warnStyle.Render("Review tool request · PgUp/PgDn to scroll\n"+
				safeText(ev.ToolName)+" "+safeText(ev.ToolArgs)), c.w))
		}
	case agent.ToolResult:
		c.approval = nil
		c.approveText = ""
		header := c.pendingTool
		if header == "" {
			header = toolHeader(ev.ToolName, "")
		}
		c.pendingTool = ""
		c.appendTranscript("\n" + c.toolBox(header, safeText(ev.Text)) + "\n")
	case agent.TurnDone:
		c.flushPending()
		if u := ev.Usage; u != nil {
			tok := u.TotalTokens
			line := fmt.Sprintf("%d tokens", tok)
			if ev.Duration > 0 {
				line += fmt.Sprintf(" · %.1fs", ev.Duration.Seconds())
				if u.CompletionTokens > 0 {
					line += fmt.Sprintf(" · %.1f tok/s", float64(u.CompletionTokens)/ev.Duration.Seconds())
				}
			}
			c.appendTranscript("\n" + padText(dimStyle.Render(line), c.w))
		} else {
			c.appendTranscript("\n")
		}
		c.status = ""
		c.contextUsed = ev.ContextTokens
		if c.lastDec != nil {
			c.contextModel = c.lastDec.Role.Model
		}
		c.turns++
		if ev.Usage != nil {
			c.tokensIn += ev.Usage.PromptTokens
			c.tokensOut += ev.Usage.CompletionTokens
			c.cost += ev.Usage.Cost
		}
	case agent.Error:
		c.approval = nil
		c.approveText = ""
		c.flushPending()
		if c.pendingTool != "" {
			c.appendTranscript(padText(dimStyle.Render(c.pendingTool), c.w))
			c.pendingTool = ""
		}
		c.appendTranscript(padText(errStyle.Render("✗ "+safeText(ev.Text)), c.w))
		c.status = ""
	}
	return c, next
}
