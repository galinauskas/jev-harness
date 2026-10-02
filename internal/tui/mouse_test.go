package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"jevharness/internal/agent"
	"jevharness/internal/config"
	"jevharness/internal/session"
)

func TestMouseWheelScrollsChatThroughApp(t *testing.T) {
	for _, state := range []string{"idle", "working", "approval"} {
		t.Run(state, func(t *testing.T) {
			c := safetyChat(t)
			a := &App{chat: c}
			a.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
			a.chat.appendTranscript(strings.Repeat("Transcript line\n", 100))
			a.chat.vp.GotoBottom()
			a.chat.ta.SetValue("keep this draft")
			if state != "idle" {
				a.chat.events = make(chan agent.Event)
			}
			if state == "approval" {
				a.chat.approval = make(chan bool, 1)
			}
			if a.View().MouseMode != tea.MouseModeCellMotion {
				t.Fatal("terminal mouse reporting is disabled")
			}
			bottom := a.chat.vp.YOffset()
			a.Update(tea.MouseWheelMsg{Button: tea.MouseWheelUp})
			if a.chat.vp.YOffset() >= bottom {
				t.Fatal("wheel up did not scroll the transcript")
			}
			if state != "idle" {
				position := a.chat.vp.YOffset()
				a.Update(tickMsg{})
				if a.chat.vp.YOffset() != position {
					t.Fatal("stream refresh discarded the scroll position")
				}
			}
			a.Update(tea.MouseWheelMsg{Button: tea.MouseWheelDown})
			if a.chat.vp.YOffset() != bottom || a.chat.ta.Value() != "keep this draft" {
				t.Fatal("wheel down should return to the bottom and preserve input")
			}
		})
	}
}

func TestMouseWheelNavigatesActivePicker(t *testing.T) {
	c := safetyChat(t)
	c.sessionPicker = true
	c.sessionList = make([]session.Session, 3)
	c.vp.SetContent(strings.Repeat("line\n", 100))
	position := c.vp.YOffset()
	for range 10 {
		c, _ = c.Update(tea.MouseWheelMsg{Button: tea.MouseWheelDown})
	}
	if c.sessionCursor != 3 || c.vp.YOffset() != position {
		t.Fatal("wheel should navigate and clamp the session picker")
	}
	c, _ = c.Update(tea.MouseWheelMsg{Button: tea.MouseWheelUp})
	if c.sessionCursor != 2 {
		t.Fatal("wheel up should select the previous session")
	}

	a := &App{mode: modeSettings, settings: newSettings(nil, config.Default(), 80, 24)}
	a.Update(tea.MouseWheelMsg{Button: tea.MouseWheelDown})
	if a.settings.rowCursor != 1 || a.View().MouseMode != tea.MouseModeCellMotion {
		t.Fatal("settings should receive wheel navigation and enable mouse reporting")
	}
	a.settings.mode = sHelp
	a.Update(tea.MouseWheelMsg{Button: tea.MouseWheelDown})
	if a.settings.helpCursor != 1 {
		t.Fatal("wheel should scroll settings help")
	}
	a.settings.mode = sSearchProvider
	for range 5 {
		a.Update(tea.MouseWheelMsg{Button: tea.MouseWheelDown})
	}
	if a.settings.choiceCursor != 1 {
		t.Fatal("wheel should stop at the final provider choice")
	}
	a.Update(tea.MouseWheelMsg{Button: tea.MouseWheelUp})
	if a.settings.choiceCursor != 0 {
		t.Fatal("wheel should select the previous provider")
	}
}
