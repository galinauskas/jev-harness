package tui

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"jevharness/internal/agent"
	"jevharness/internal/config"
)

type closeSettingsMsg struct{ cfg config.Config }
type sMode int

const (
	sList sMode = iota
	sRoleForm
	sValueForm
	sHelp
	sDefaultRole
	sDeleteRole
	sSearchProvider
)
const (
	settingsAppearance = iota
	settingsRouting
	settingsContext
	settingsProviders
	settingsRoles
)

var settingsRoleName = regexp.MustCompile(`^[a-z0-9_-]{1,32}$`)

var roleProviders = []string{"openrouter", "deepseek", "opencode-go"}

type settingsModel struct {
	ag                       *agent.Agent
	cfg                      config.Config
	cwd                      string
	mode                     sMode
	tab, rowCursor, cursor   int
	positions                [5]int
	choiceCursor, helpCursor int
	editingIdx               int
	editRow                  settingRow
	inputs                   []textinput.Model
	focus                    int
	msg                      string
	msgIsErr                 bool
	w, h                     int
}

func newSettings(ag *agent.Agent, cfg config.Config, w, h int) settingsModel {
	cwd := ""
	if ag != nil {
		cwd = ag.ProjectDirectory()
	}
	return settingsModel{ag: ag, cfg: cfg, cwd: cwd, w: w, h: h, editingIdx: -1}
}
func (s *settingsModel) resize(w, h int) {
	s.w, s.h = w, h
	for i := range s.inputs {
		s.inputs[i].SetWidth(s.inputWidth())
	}
}
func (s *settingsModel) clearMessage() { s.msg, s.msgIsErr = "", false }
func (s settingsModel) selectedRow() int {
	if s.tab == settingsRoles {
		return s.cursor
	}
	return s.rowCursor
}
func (s *settingsModel) switchTab(tab int) {
	s.positions[s.tab] = s.selectedRow()
	s.tab = (tab + len(settingsTabs)) % len(settingsTabs)
	s.rowCursor = min(s.positions[s.tab], max(0, len(s.rows())-1))
	if s.tab == settingsRoles {
		s.cursor = s.rowCursor
	}
	s.clearMessage()
}
func (s settingsModel) Update(msg tea.Msg) (settingsModel, tea.Cmd) {
	switch s.mode {
	case sRoleForm, sValueForm:
		return s.updateForm(msg)
	case sDefaultRole, sDeleteRole, sSearchProvider:
		return s.updateChoice(msg)
	case sHelp:
		if k, ok := msg.(tea.KeyPressMsg); ok {
			switch k.String() {
			case "esc", "?":
				s.mode = sList
			case "down", "j", "tab":
				s.helpCursor = min(s.helpCursor+1, len(settingsHelpLines(max(1, s.boxWidth()-6)))-1)
			case "up", "k", "shift+tab":
				s.helpCursor = max(0, s.helpCursor-1)
			}
		}
		return s, nil
	default:
		return s.updateList(msg)
	}
}
func (s settingsModel) updateList(msg tea.Msg) (settingsModel, tea.Cmd) {
	k, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return s, nil
	}
	fail := func(message string) (settingsModel, tea.Cmd) { s.msg, s.msgIsErr = message, true; return s, nil }
	switch k.String() {
	case "esc":
		return s, func() tea.Msg { return closeSettingsMsg{cfg: s.cfg} }
	case "left":
		s.switchTab(s.tab - 1)
	case "right":
		s.switchTab(s.tab + 1)
	case "up", "k", "down", "j", "tab", "shift+tab":
		rows := s.rows()
		if len(rows) == 0 {
			return s, nil
		}
		current, next := s.selectedRow(), s.selectedRow()
		delta := 1
		if k.String() == "up" || k.String() == "k" || k.String() == "shift+tab" {
			delta = -1
		}
		if k.String() == "tab" || k.String() == "shift+tab" {
			// A single-section page still lets Tab move through its rows.
			next = (current + delta + len(rows)) % len(rows)
			for offset := 1; offset < len(rows); offset++ {
				candidate := (current + delta*offset + len(rows)) % len(rows)
				if rows[candidate].section != rows[current].section {
					next = candidate
					break
				}
			}
		} else {
			next = max(0, min(current+delta, len(rows)-1))
		}
		if s.tab == settingsRoles {
			s.cursor = next
		} else {
			s.rowCursor = next
		}
		s.clearMessage()
	case "enter", "e", "space", " ":
		if s.tab == settingsRoles {
			if len(s.cfg.Roles) > 0 {
				return s.openRoleForm(s.cursor)
			}
			return s, nil
		}
		row := s.rows()[s.rowCursor]
		if strings.HasPrefix(row.action, "provider:") {
			provider := strings.TrimPrefix(row.action, "provider:")
			enabled := !s.cfg.ProviderAllowed(s.cwd, provider)
			cfg := s.withProviderEnabled(provider, enabled)
			if !enabled {
				hasRole := false
				for _, role := range cfg.Roles {
					hasRole = hasRole || cfg.ProviderAllowed(s.cwd, role.Backend())
				}
				if !hasRole {
					return fail("Keep a provider enabled for at least one role. Configure roles in the Roles tab.")
				}
			}
			return s.applyConfig(cfg, row.label+" "+settingOn(enabled))
		}
		if row.action == "restore-providers" {
			cfg := s.withProviderList(config.Default().Safety.AllowedProviders)
			return s.applyConfig(cfg, "Providers restored for this project")
		}
		switch row.action {
		case "t", "b", "x":
			return s.updateList(tea.KeyPressMsg{Code: rune(row.action[0])})
		case "search-provider":
			s.mode, s.choiceCursor = sSearchProvider, 0
			if s.cfg.WebSearchProvider() == "brave" {
				s.choiceCursor = 1
			}
			s.clearMessage()
			return s, nil
		case "default":
			s.mode, s.choiceCursor = sDefaultRole, 0
			for i, role := range s.cfg.Roles {
				if role.Name == s.cfg.DefaultRole {
					s.choiceCursor = i
				}
			}
			s.clearMessage()
			return s, nil
		case "status0", "status1", "status2", "status3":
			cfg := s.cfg
			hidden := []*bool{&cfg.StatusLine.HideModel, &cfg.StatusLine.HideTokens, &cfg.StatusLine.HideContext, &cfg.StatusLine.HideCost}
			index := int(row.action[len(row.action)-1] - '0')
			*hidden[index] = !*hidden[index]
			return s.applyConfig(cfg, row.label+" saved")
		default:
			return s.openValueForm(row)
		}
	case "n":
		if s.tab == settingsRoles {
			return s.openRoleForm(-1)
		}
	case "d":
		if s.tab != settingsRoles || len(s.cfg.Roles) == 0 {
			return s, nil
		}
		if len(s.cfg.Roles) == 1 {
			return fail("Keep at least one role.")
		}
		if s.cfg.Roles[s.cursor].Name == s.cfg.DefaultRole {
			return fail("Choose another default role with D before deleting this one.")
		}
		s.mode = sDeleteRole
		s.clearMessage()
	case "D":
		if s.tab == settingsRoles && len(s.cfg.Roles) > 0 {
			cfg := s.cfg
			cfg.DefaultRole = cfg.Roles[s.cursor].Name
			return s.applyConfig(cfg, "Default role: "+cfg.DefaultRole)
		}
	case "g":
		s.switchTab(settingsRouting)
		s.rowCursor = 0
		return s.openValueForm(s.rows()[0])
	case "c":
		s.switchTab(settingsContext)
		s.rowCursor = 0
		return s.openValueForm(s.rows()[0])
	case "s":
		s.switchTab(settingsAppearance)
		s.rowCursor = 1
	case "t", "b", "x":
		s.switchTab(settingsAppearance)
		cfg := s.cfg
		label := "Chat input style"
		if k.String() == "t" {
			s.rowCursor = 0
			cfg.ChatInputLines = !cfg.ChatInputLines
		} else if k.String() == "x" {
			s.rowCursor = 5
			cfg.Safety.DockerSandbox = !cfg.Safety.DockerSandbox
			label = "Docker sandbox (experimental)"
		} else {
			s.rowCursor = 6
			cfg.CompactCommandOutput = !cfg.CompactCommandOutput
			label = "Command output"
		}
		return s.applyConfig(cfg, label+" saved")
	case "?":
		s.mode, s.helpCursor = sHelp, 0
		s.clearMessage()
	}
	return s, nil
}
func (s settingsModel) applyConfig(cfg config.Config, message string) (settingsModel, tea.Cmd) {
	if err := cfg.Validate(); err != nil {
		s.msg, s.msgIsErr = err.Error(), true
		return s, nil
	}
	if err := config.Save(cfg); err != nil {
		s.msg, s.msgIsErr = "Could not save: "+err.Error(), true
		return s, nil
	}
	if s.ag != nil {
		s.ag.SetConfig(cfg)
	}
	s.cfg, s.msg, s.msgIsErr = cfg, message, false
	return s, nil
}
func (s settingsModel) updateChoice(msg tea.Msg) (settingsModel, tea.Cmd) {
	k, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return s, nil
	}
	switch k.String() {
	case "esc":
		s.mode = sList
		s.clearMessage()
	case "up", "k", "shift+tab":
		if s.mode == sSearchProvider {
			s.choiceCursor = (s.choiceCursor + 1) % 2
			s.clearMessage()
		} else if s.mode == sDefaultRole {
			s.choiceCursor = (s.choiceCursor + len(s.cfg.Roles) - 1) % len(s.cfg.Roles)
			s.clearMessage()
		}
	case "down", "j", "tab":
		if s.mode == sSearchProvider {
			s.choiceCursor = (s.choiceCursor + 1) % 2
			s.clearMessage()
		} else if s.mode == sDefaultRole {
			s.choiceCursor = (s.choiceCursor + 1) % len(s.cfg.Roles)
			s.clearMessage()
		}
	case "enter":
		cfg := s.cfg
		message := ""
		if s.mode == sSearchProvider {
			cfg.SearchProvider = []string{"exa", "brave"}[s.choiceCursor]
			message = "Search provider: " + cfg.SearchProvider
		} else if s.mode == sDefaultRole {
			cfg.DefaultRole = cfg.Roles[s.choiceCursor].Name
			message = "Default role: " + cfg.DefaultRole
		} else {
			message = "Deleted " + cfg.Roles[s.cursor].Name
			cfg.Roles = append([]config.Role(nil), cfg.Roles...)
			cfg.Roles = append(cfg.Roles[:s.cursor], cfg.Roles[s.cursor+1:]...)
		}
		m, cmd := s.applyConfig(cfg, message)
		if !m.msgIsErr {
			m.mode = sList
			m.cursor = min(m.cursor, len(m.cfg.Roles)-1)
		}
		return m, cmd
	}
	return s, nil
}
func (s settingsModel) openRoleForm(idx int) (settingsModel, tea.Cmd) {
	if s.tab != settingsRoles {
		s.switchTab(settingsRoles)
	}
	s.mode, s.editingIdx, s.focus = sRoleForm, idx, 0
	s.clearMessage()
	role := config.Role{}
	if idx >= 0 {
		role = s.cfg.Roles[idx]
	}
	s.inputs = []textinput.Model{newInput("e.g. research", role.Name), newInput("e.g. provider/model", role.Model), newInput("When should Jev use this role?", role.Description), newInput("", role.Backend())}
	for i := range s.inputs {
		s.inputs[i].SetWidth(s.inputWidth())
	}
	return s, s.inputs[0].Focus()
}
func (s settingsModel) openValueForm(row settingRow) (settingsModel, tea.Cmd) {
	s.mode, s.editRow, s.focus = sValueForm, row, 0
	s.clearMessage()
	value := ""
	switch row.action {
	case "model":
		value = s.cfg.JevModel
	case "threshold":
		value = fmt.Sprintf("%g", s.cfg.ConfidenceThreshold)
	case "c":
		value = strconv.Itoa(s.cfg.CompactionThreshold)
	case "key0":
		value = s.cfg.APIKey
	case "key1":
		value = s.cfg.DeepSeekAPIKey
	case "key2":
		value = s.cfg.OpenCodeGoAPIKey
	case "key3":
		value = s.cfg.ExaAPIKey
	case "key4":
		value = s.cfg.BraveAPIKey
	}
	s.inputs = []textinput.Model{newInput("", value)}
	if strings.HasPrefix(row.action, "key") {
		s.inputs[0].EchoMode = textinput.EchoPassword
	}
	s.inputs[0].SetWidth(s.inputWidth())
	return s, s.inputs[0].Focus()
}
func newInput(placeholder, value string) textinput.Model {
	t := textinput.New()
	t.Placeholder = placeholder
	t.SetValue(value)
	t.SetWidth(60)
	return t
}
func (s settingsModel) updateForm(msg tea.Msg) (settingsModel, tea.Cmd) {
	if k, ok := msg.(tea.KeyPressMsg); ok {
		switch k.String() {
		case "esc":
			s.mode = sList
			s.inputs = nil
			s.clearMessage()
			return s, nil
		case "enter":
			return s.saveForm()
		case "tab", "down", "shift+tab", "up":
			delta := 1
			if k.String() == "shift+tab" || k.String() == "up" {
				delta = -1
			}
			s.inputs[s.focus].Blur()
			s.focus = (s.focus + delta + len(s.inputs)) % len(s.inputs)
			s.clearMessage()
			return s, s.inputs[s.focus].Focus()
		}
		if s.mode == sRoleForm && s.focus == 3 {
			if k.String() == "left" || k.String() == "right" || k.String() == "space" || k.String() == " " {
				current := 0
				for i, provider := range roleProviders {
					if provider == s.inputs[3].Value() {
						current = i
					}
				}
				delta := 1
				if k.String() == "left" {
					delta = -1
				}
				s.inputs[3].SetValue(roleProviders[(current+delta+len(roleProviders))%len(roleProviders)])
				s.clearMessage()
			}
			return s, nil
		}
		s.clearMessage()
	}
	var cmd tea.Cmd
	s.inputs[s.focus], cmd = s.inputs[s.focus].Update(msg)
	return s, cmd
}
func (s settingsModel) saveForm() (settingsModel, tea.Cmd) {
	fail := func(message string) (settingsModel, tea.Cmd) { s.msg, s.msgIsErr = message, true; return s, nil }
	cfg := s.cfg
	if s.mode == sRoleForm {
		cfg.Roles = append([]config.Role(nil), cfg.Roles...)
		role := config.Role{}
		if s.editingIdx >= 0 {
			role = cfg.Roles[s.editingIdx]
		}
		role.Name, role.Model = strings.TrimSpace(s.inputs[0].Value()), strings.TrimSpace(s.inputs[1].Value())
		role.Description, role.Provider = strings.TrimSpace(s.inputs[2].Value()), s.inputs[3].Value()
		fieldError := func(index int, message string) (settingsModel, tea.Cmd) {
			s.inputs[s.focus].Blur()
			s.focus, s.msg, s.msgIsErr = index, message, true
			return s, s.inputs[index].Focus()
		}
		if !settingsRoleName.MatchString(role.Name) {
			return fieldError(0, "Use 1–32 lowercase letters, numbers, underscores or hyphens.")
		}
		for i, other := range cfg.Roles {
			if i != s.editingIdx && other.Name == role.Name {
				return fieldError(0, "A role with this name already exists. Choose another name.")
			}
		}
		if role.Model == "" || strings.HasPrefix(role.Model, "-") {
			return fieldError(1, "Enter a model ID for this provider.")
		}
		if role.Provider == "openrouter" && !strings.Contains(role.Model, "/") {
			return fieldError(1, "OpenRouter model IDs use provider/model, such as openai/gpt-4.1.")
		}
		if role.Description == "" {
			return fieldError(2, "Describe when Jev should use this role.")
		}

		if s.editingIdx >= 0 {
			if cfg.DefaultRole == cfg.Roles[s.editingIdx].Name {
				cfg.DefaultRole = role.Name
			}
			cfg.Roles[s.editingIdx] = role
		} else {
			cfg.Roles = append(cfg.Roles, role)
		}
	} else {
		value := strings.TrimSpace(s.inputs[0].Value())
		switch s.editRow.action {
		case "model":
			cfg.JevModel = value
		case "threshold":
			number, err := strconv.ParseFloat(value, 64)
			if err != nil || math.IsNaN(number) || math.IsInf(number, 0) || number < 0 || number > 1 {
				return fail("Enter a number from 0 to 1, such as 0.7.")
			}
			cfg.ConfidenceThreshold = number
		case "c":
			number, err := strconv.Atoi(value)
			if err != nil || number < 0 || number > 100 {
				return fail("Enter a whole percentage from 0 to 100. Use 0 to turn compaction off.")
			}
			cfg.CompactionThreshold = number
		case "key0":
			cfg.APIKey = value
		case "key1":
			cfg.DeepSeekAPIKey = value
		case "key2":
			cfg.OpenCodeGoAPIKey = value
		case "key3":
			cfg.ExaAPIKey = value
		case "key4":
			cfg.BraveAPIKey = value
		}
	}
	m, cmd := s.applyConfig(cfg, "Saved")
	if !m.msgIsErr {
		if s.mode == sRoleForm && s.editingIdx < 0 {
			m.cursor = len(cfg.Roles) - 1
		}
		m.mode, m.inputs = sList, nil
	}
	return m, cmd
}

// Copy lists and maps before editing so cancellation/save failures and other
// projects retain their original provider settings.
func (s settingsModel) withProviderList(list []string) config.Config {
	cfg := s.cfg
	list = append([]string(nil), list...)
	if s.cwd == "" {
		cfg.Safety.AllowedProviders = list
		return cfg
	}
	cfg.Safety.Projects = make(map[string][]string, len(s.cfg.Safety.Projects)+1)
	for path, providers := range s.cfg.Safety.Projects {
		cfg.Safety.Projects[path] = append([]string(nil), providers...)
	}
	cfg.Safety.Projects[s.cwd] = list
	return cfg
}

func (s settingsModel) withProviderEnabled(provider string, enabled bool) config.Config {
	list := s.cfg.Safety.AllowedProviders
	if project, ok := s.cfg.Safety.Projects[s.cwd]; ok {
		list = project
	}
	updated := make([]string, 0, len(list)+1)
	for _, p := range list {
		if p != provider {
			updated = append(updated, p)
		}
	}
	if enabled {
		updated = append(updated, provider)
	}
	return s.withProviderList(updated)
}
