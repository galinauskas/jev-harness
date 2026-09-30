package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"jevharness/internal/config"
)

func TestCommandOutputTogglePersistsAndUpdatesAgent(t *testing.T) {
	c := safetyChat(t)
	s := newSettings(c.ag, c.cfg, 140, 50)
	if s.cfg.CompactCommandOutput {
		t.Fatal("old configs should retain standard output")
	}
	for _, enabled := range []bool{true, false} {
		s, _ = s.Update(tea.KeyPressMsg{Code: 'b'})
		cfg, err := config.Load()
		if err != nil || cfg.CompactCommandOutput != enabled || s.ag.Config().CompactCommandOutput != enabled {
			t.Fatal("toggle did not persist/update agent", err)
		}
	}
	for _, height := range []int{50, 20} {
		s.resize(140, height)
		if !strings.Contains(s.View(), "Command output") {
			t.Fatal("toggle is not discoverable")
		}
	}
}

func TestExperimentalDockerTogglePersistsAndUpdatesAgent(t *testing.T) {
	c := safetyChat(t)
	s := newSettings(c.ag, c.cfg, 140, 50)
	if s.cfg.Safety.DockerSandbox {
		t.Fatal("Docker should default off")
	}
	for _, enabled := range []bool{true, false} {
		s, _ = s.Update(tea.KeyPressMsg{Code: 'x'})
		cfg, err := config.Load()
		if err != nil || cfg.Safety.DockerSandbox != enabled || s.ag.Config().Safety.DockerSandbox != enabled {
			t.Fatal("toggle not saved", err)
		}
	}
	if !strings.Contains(s.View(), "Docker sandbox (experimental)") {
		t.Fatal("missing experimental label")
	}
}
