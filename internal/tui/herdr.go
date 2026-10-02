package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"jevharness/internal/herdr"
)

// SetHerdrReporter attaches an optional, nonblocking lifecycle integration.
func (a *App) SetHerdrReporter(r interface{ Report(herdr.Snapshot) }) {
	a.herdrReporter = r
	a.reportHerdr()
}

// ResumeSession uses the same project checks and recovery flow as the picker.
func (a *App) ResumeSession(id string) error {
	if a.chat.cfg.Safety.Ephemeral {
		return fmt.Errorf("saved sessions are disabled in ephemeral mode")
	}
	if a.chat.running() {
		return fmt.Errorf("finish or abort the current turn before resuming")
	}
	v, err := a.chat.store.Load(id)
	if err != nil {
		return err
	}
	if v.CWD != a.cwd {
		return fmt.Errorf("session belongs to another working directory")
	}
	a.chat.restoreSession(v)
	return nil
}

func (a *App) reportHerdr() {
	if a.herdrReporter == nil {
		return
	}
	s := a.herdrSnapshot()
	// Commands such as /changes can create a session before any model turn.
	// Save it before advertising a resume command, without touching an active
	// worker's history or exposing reporting failures in the UI.
	if s.SessionID != "" && s.SessionID != a.herdrSession && !a.chat.running() {
		path := filepath.Join(a.chat.store.Dir, s.SessionID+".json")
		_, err := os.Stat(path)
		if os.IsNotExist(err) {
			v := a.chat.snapshot()
			v.Running = a.chat.recovery
			if a.chat.lastDec != nil {
				v.LastRole = a.chat.lastDec.Role
			}
			err = a.chat.store.Save(v)
		}
		if err != nil {
			s.SessionID = ""
			s.ResumeArgv = nil
		} else {
			a.herdrSession = s.SessionID
		}
	}
	a.herdrReporter.Report(s)
}

func (a *App) herdrSnapshot() herdr.Snapshot {
	s := herdr.Snapshot{State: "idle"}
	if a.chat.running() {
		s.State = "working"
	}
	if a.chat.approval != nil {
		s.State, s.Message = "blocked", "Tool approval required"
	} else if a.chat.recovery {
		s.State, s.Message = "blocked", "Interrupted session: review /changes and acknowledge with /recover"
	}
	cfg := a.chat.cfg
	// Session resume resets autonomous permissions, just like the picker.
	mode := a.agent.Mode()
	if mode == "autonomous" {
		mode = "develop"
	}
	s.ResumeArgv = []string{"jev", "--mode", mode,
		"--output-tokens", strconv.Itoa(cfg.Limits.OutputTokens),
		"--token-budget", strconv.Itoa(cfg.Limits.TotalTokens),
		"--time-budget", strconv.Itoa(cfg.Limits.Seconds),
		"--cost-budget", strconv.FormatFloat(cfg.Limits.Cost, 'g', -1, 64)}
	if cfg.Safety.Ephemeral {
		s.ResumeArgv = append(s.ResumeArgv, "--ephemeral")
	} else if a.chat.sessionID != "" {
		s.SessionID = a.chat.sessionID
		s.ResumeArgv = append(s.ResumeArgv, "--resume", s.SessionID)
	}
	return s
}
