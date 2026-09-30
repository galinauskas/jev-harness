package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"jevharness/internal/workspace"
)

// localBash starts in the staged directory, but provides no host isolation.
func (e *Executor) localBash(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Command string `json:"command"`
		Timeout int    `json:"timeout_seconds"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", err
	}
	if p.Timeout <= 0 {
		p.Timeout = 60
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(min(p.Timeout, 600))*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "sh", "-c", p.Command)
	cmd.Dir = e.Workspace.Stage
	configureProcess(cmd)
	cmd.WaitDelay = time.Second
	out := &cappedOutput{notify: e.Output}
	if e.CompactCommandOutput {
		out.limit = workspace.MaxCommandOutput
		out.notify = nil
	}
	cmd.Stdout, cmd.Stderr = out, out
	err := cmd.Run()
	s := strings.TrimRight(out.String(), "\n")
	if out.dropped > 0 {
		s += fmt.Sprintf("\n[truncated %d bytes]", out.dropped)
	}
	if ctx.Err() != nil {
		return s, fmt.Errorf("local command interrupted: %w; partial staged changes may remain", ctx.Err())
	}
	var exitErr *exec.ExitError
	if err != nil && !errors.As(err, &exitErr) {
		return s, err
	}
	return s + fmt.Sprintf("\n[exit %d; changes staged, use /changes and /apply]", cmd.ProcessState.ExitCode()), nil
}
