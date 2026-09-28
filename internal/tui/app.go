// Package tui is the Bubble Tea front end: chat screen plus settings screen.
package tui

import (
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"jevharness/internal/agent"
	"jevharness/internal/config"
)

type mode int

const (
	modeChat mode = iota
	modeSettings
)

var (
	dimStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	errStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("203"))
	warnStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	okStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("114"))
	boldStyle  = lipgloss.NewStyle().Bold(true)
	accent     = lipgloss.NewStyle().Foreground(lipgloss.Color("81"))
	routeStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("110"))
	userStyle  = lipgloss.NewStyle().
			Background(lipgloss.Color("236")).
			Foreground(lipgloss.Color("252"))
)

// openSettingsMsg asks the root model to switch to settings.
type openSettingsMsg struct{}

// App is the root model. It owns the config and routes messages to the
// active screen.
type App struct {
	agent    *agent.Agent
	cfg      config.Config
	cwd      string
	mode     mode
	chat     chatModel
	settings settingsModel
	w, h     int
}

// New builds the root model.
func New(ag *agent.Agent, cfg config.Config, cwd string) *App {
	return &App{
		agent: ag,
		cfg:   cfg,
		cwd:   cwd,
		mode:  modeChat,
		chat:  newChat(ag, cfg, cwd),
	}
}

// SetStatus seeds the chat status line (e.g. a missing API key at startup).
func (a *App) SetStatus(msg string, isErr bool) {
	a.chat.status = msg
	a.chat.statusIsErr = isErr
}

// Init implements tea.Model.
func (a *App) Init() tea.Cmd { return a.chat.requestContext() }

// Update implements tea.Model.
func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case tea.WindowSizeMsg:
		a.w, a.h = m.Width, m.Height
		a.chat.resize(m.Width, m.Height)
		a.settings.resize(m.Width, m.Height)
		return a, nil
	case openSettingsMsg:
		if a.chat.running() {
			a.chat.status = "finish or abort the current turn before opening settings"
			return a, nil
		}
		a.mode = modeSettings
		a.settings = newSettings(a.agent, a.cfg, a.w, a.h)
		return a, nil
	case closeSettingsMsg:
		a.mode = modeChat
		// m.cfg is the validated+saved config (or the unchanged one on
		// cancel); keep the agent in sync either way.
		a.cfg = m.cfg
		a.chat.setConfig(m.cfg)
		return a, a.chat.requestContext()
	case tea.KeyPressMsg:
		if m.String() == "ctrl+c" {
			if a.chat.cancel != nil {
				a.chat.cancel()
			}
			return a, tea.Quit
		}
		if m.String() == "ctrl+o" && a.mode == modeChat {
			return a.Update(openSettingsMsg{})
		}
	}

	if a.mode == modeSettings {
		var cmd tea.Cmd
		a.settings, cmd = a.settings.Update(msg)
		return a, cmd
	}
	var cmd tea.Cmd
	a.chat, cmd = a.chat.Update(msg)
	return a, cmd
}

// View implements tea.Model.
func (a *App) View() tea.View {
	var s string
	if a.mode == modeSettings {
		s = a.settings.View()
	} else {
		s = a.chat.View()
	}
	v := tea.NewView(s)
	v.AltScreen = true
	return v
}
