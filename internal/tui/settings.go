package tui

import (
	"fmt"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"jevharness/internal/agent"
	"jevharness/internal/config"
)

// closeSettingsMsg returns to chat; cfg is the current (validated) config.
type closeSettingsMsg struct{ cfg config.Config }

type sMode int

const (
	sList sMode = iota
	sRoleForm
	sGlobalsForm
	sStatusForm
	sHelp
	sContextForm
)

type settingsModel struct {
	ag   *agent.Agent
	cfg  config.Config
	mode sMode

	cursor       int
	statusCursor int
	editingIdx   int // index into cfg.Roles, or -1 for a new role
	inputs       []textinput.Model
	focus        int
	msg          string
	msgIsErr     bool
	w, h         int
}

func newSettings(ag *agent.Agent, cfg config.Config, w, h int) settingsModel {
	return settingsModel{ag: ag, cfg: cfg, w: w, h: h}
}

func (s *settingsModel) resize(w, h int) {
	s.w, s.h = w, h
	for i := range s.inputs {
		s.inputs[i].SetWidth(s.inputWidth())
	}
}

func (s settingsModel) Update(msg tea.Msg) (settingsModel, tea.Cmd) {
	switch s.mode {
	case sRoleForm, sGlobalsForm, sContextForm:
		return s.updateForm(msg)
	case sStatusForm:
		return s.updateStatus(msg)
	case sHelp:
		if k, ok := msg.(tea.KeyPressMsg); ok && (k.String() == "esc" || k.String() == "?") {
			s.mode = sList
		}
		return s, nil
	default:
		return s.updateList(msg)
	}
}

// ---- roles list ----

func (s settingsModel) updateList(msg tea.Msg) (settingsModel, tea.Cmd) {
	k, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return s, nil
	}
	fail := func(m string) (settingsModel, tea.Cmd) {
		s.msg, s.msgIsErr = m, true
		return s, nil
	}
	switch k.String() {
	case "esc":
		return s, func() tea.Msg { return closeSettingsMsg{cfg: s.cfg} }
	case "up", "k":
		if s.cursor > 0 {
			s.cursor--
		}
	case "down", "j":
		if s.cursor < len(s.cfg.Roles)-1 {
			s.cursor++
		}
	case "n":
		return s.openRoleForm(-1)
	case "enter", "e":
		return s.openRoleForm(s.cursor)
	case "d":
		if len(s.cfg.Roles) <= 1 {
			return fail("cannot delete the last role")
		}
		role := s.cfg.Roles[s.cursor]
		if role.Name == s.cfg.DefaultRole {
			return fail("cannot delete the default role; mark another default first (D)")
		}
		cfg := s.cfg
		cfg.Roles = append([]config.Role(nil), s.cfg.Roles...)
		cfg.Roles = append(cfg.Roles[:s.cursor], cfg.Roles[s.cursor+1:]...)
		m, cmd := s.applyConfig(cfg, "deleted "+role.Name)
		if !m.msgIsErr && s.cursor >= len(m.cfg.Roles) {
			m.cursor = len(m.cfg.Roles) - 1
		}
		return m, cmd
	case "D":
		cfg := s.cfg
		cfg.DefaultRole = s.cfg.Roles[s.cursor].Name
		return s.applyConfig(cfg, "default: "+cfg.DefaultRole)
	case "c":
		s.mode, s.focus, s.msg = sContextForm, 0, ""
		s.inputs = []textinput.Model{newInput("compaction threshold", strconv.Itoa(s.cfg.CompactionThreshold))}
		s.inputs[0].SetWidth(s.inputWidth())
		return s, s.inputs[0].Focus()
	case "g":
		return s.openGlobalsForm()
	case "s":
		s.mode = sStatusForm
		s.statusCursor = 0
		s.msg = ""
		return s, nil
	case "t":
		cfg := s.cfg
		cfg.ChatInputLines = !cfg.ChatInputLines
		return s.applyConfig(cfg, "")
	case "?":
		s.mode = sHelp
		s.msg = ""
		return s, nil
	}
	return s, nil
}

func (s settingsModel) applyConfig(cfg config.Config, message string) (settingsModel, tea.Cmd) {
	if err := cfg.Validate(); err != nil {
		s.msg, s.msgIsErr = err.Error(), true
		return s, nil
	}
	if err := config.Save(cfg); err != nil {
		s.msg, s.msgIsErr = "save: "+err.Error(), true
		return s, nil
	}
	s.ag.SetConfig(cfg)
	s.cfg = cfg
	s.msg, s.msgIsErr = message, false
	return s, nil
}

func (s settingsModel) updateStatus(msg tea.Msg) (settingsModel, tea.Cmd) {
	k, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return s, nil
	}
	switch k.String() {
	case "esc":
		s.mode = sList
		s.msg = ""
	case "up", "k", "shift+tab":
		s.statusCursor = (s.statusCursor + 3) % 4
	case "down", "j", "tab":
		s.statusCursor = (s.statusCursor + 1) % 4
	case "enter", "space", " ":
		cfg := s.cfg
		switch s.statusCursor {
		case 0:
			cfg.StatusLine.HideModel = !cfg.StatusLine.HideModel
		case 1:
			cfg.StatusLine.HideTokens = !cfg.StatusLine.HideTokens
		case 2:
			cfg.StatusLine.HideContext = !cfg.StatusLine.HideContext
		case 3:
			cfg.StatusLine.HideCost = !cfg.StatusLine.HideCost
		}
		return s.applyConfig(cfg, "")
	}
	return s, nil
}

func (s settingsModel) openRoleForm(idx int) (settingsModel, tea.Cmd) {
	s.mode = sRoleForm
	s.editingIdx = idx
	s.focus = 0
	s.msg = ""
	role := config.Role{}
	if idx >= 0 {
		role = s.cfg.Roles[idx]
	}
	s.inputs = []textinput.Model{
		newInput("name", role.Name),
		newInput("model", role.Model),
		newInput("description", role.Description),
		newInput("provider (openrouter, deepseek, opencode-go)", role.Backend()),
	}
	for i := range s.inputs {
		s.inputs[i].SetWidth(s.inputWidth())
	}
	return s, s.inputs[0].Focus()
}

func (s settingsModel) openGlobalsForm() (settingsModel, tea.Cmd) {
	s.mode = sGlobalsForm
	s.focus = 0
	s.msg = ""
	s.inputs = []textinput.Model{
		newInput("jev model", s.cfg.JevModel),
		newInput("confidence threshold", fmt.Sprintf("%g", s.cfg.ConfidenceThreshold)),
		newInput("openrouter api key", s.cfg.APIKey),
		newInput("deepseek api key", s.cfg.DeepSeekAPIKey),
		newInput("opencode go api key", s.cfg.OpenCodeGoAPIKey),
	}
	for i := 2; i < len(s.inputs); i++ {
		s.inputs[i].EchoMode = textinput.EchoPassword
	}
	for i := range s.inputs {
		s.inputs[i].SetWidth(s.inputWidth())
	}
	return s, s.inputs[0].Focus()
}

func newInput(placeholder, value string) textinput.Model {
	t := textinput.New()
	t.Placeholder = placeholder
	t.SetValue(value)
	t.SetWidth(60)
	return t
}

// ---- forms ----

func (s settingsModel) updateForm(msg tea.Msg) (settingsModel, tea.Cmd) {
	k, ok := msg.(tea.KeyPressMsg)
	if ok {
		switch k.String() {
		case "esc":
			s.mode = sList
			s.inputs = nil
			return s, nil
		case "enter":
			return s.saveForm()
		case "tab", "down":
			s.inputs[s.focus].Blur()
			s.focus = (s.focus + 1) % len(s.inputs)
			return s, s.inputs[s.focus].Focus()
		case "shift+tab", "up":
			s.inputs[s.focus].Blur()
			s.focus = (s.focus - 1 + len(s.inputs)) % len(s.inputs)
			return s, s.inputs[s.focus].Focus()
		}
	}
	var cmd tea.Cmd
	s.inputs[s.focus], cmd = s.inputs[s.focus].Update(msg)
	return s, cmd
}

func (s settingsModel) saveForm() (settingsModel, tea.Cmd) {
	fail := func(m string) (settingsModel, tea.Cmd) {
		s.msg, s.msgIsErr = m, true
		return s, nil
	}
	cfg := s.cfg
	cfg.Roles = append([]config.Role(nil), s.cfg.Roles...)

	if s.mode == sRoleForm {
		role := config.Role{
			Name:        strings.TrimSpace(s.inputs[0].Value()),
			Model:       strings.TrimSpace(s.inputs[1].Value()),
			Description: strings.TrimSpace(s.inputs[2].Value()),
			Provider:    strings.ToLower(strings.TrimSpace(s.inputs[3].Value())),
		}
		if s.editingIdx >= 0 {
			old := cfg.Roles[s.editingIdx].Name
			cfg.Roles[s.editingIdx] = role
			// renamed default role must keep its marker
			if cfg.DefaultRole == old {
				cfg.DefaultRole = role.Name
			}
		} else {
			cfg.Roles = append(cfg.Roles, role)
		}
	} else if s.mode == sContextForm {
		n, err := strconv.Atoi(strings.TrimSpace(s.inputs[0].Value()))
		if err != nil {
			return fail("compaction threshold must be a whole percentage (0–100)")
		}
		cfg.CompactionThreshold = n
	} else {
		cfg.JevModel = strings.TrimSpace(s.inputs[0].Value())
		f, err := strconv.ParseFloat(strings.TrimSpace(s.inputs[1].Value()), 64)
		if err != nil {
			return fail("confidence threshold: " + err.Error())
		}
		cfg.ConfidenceThreshold = f
		cfg.APIKey = strings.TrimSpace(s.inputs[2].Value())
		cfg.DeepSeekAPIKey = strings.TrimSpace(s.inputs[3].Value())
		cfg.OpenCodeGoAPIKey = strings.TrimSpace(s.inputs[4].Value())
	}

	if err := cfg.Validate(); err != nil {
		return fail(err.Error())
	}
	if err := config.Save(cfg); err != nil {
		return fail("save: " + err.Error())
	}
	s.ag.SetConfig(cfg)
	s.cfg = cfg
	s.mode = sList
	s.inputs = nil
	s.msg, s.msgIsErr = "saved", false
	if s.cursor >= len(s.cfg.Roles) {
		s.cursor = len(s.cfg.Roles) - 1
	}
	return s, nil
}
