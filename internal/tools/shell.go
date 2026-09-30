package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

func runBash(dir string, ctx context.Context, args json.RawMessage) (string, error) {
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
	if p.Timeout > 600 {
		p.Timeout = 600
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(p.Timeout)*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "sh", "-c", p.Command)
	cmd.Dir = dir
	// Keep the harness credential out of child process environments.
	cmd.Env = make([]string, 0)
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "OPENROUTER_API_KEY=") && !strings.HasPrefix(entry, "DEEPSEEK_API_KEY=") && !strings.HasPrefix(entry, "OPENCODE_GO_API_KEY=") {
			cmd.Env = append(cmd.Env, entry)
		}
	}
	configureProcess(cmd)
	cmd.WaitDelay = time.Second
	var out cappedOutput
	cmd.Stdout, cmd.Stderr = &out, &out
	err := cmd.Run()
	s := strings.TrimRight(out.String(), "\n")
	if ctx.Err() != nil {
		return s + "\n[" + ctx.Err().Error() + "]", nil
	}
	if out.dropped > 0 {
		s += fmt.Sprintf("\n[truncated %d bytes]", out.dropped)
	}
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return s + fmt.Sprintf("\n[exit %d]", ee.ExitCode()), nil
		}
		return "", err
	}
	return s, nil
}

// cappedOutput drains command output while retaining only the first maxOut
// bytes. Draining prevents chatty commands from blocking on a full pipe.
type cappedOutput struct {
	mu      sync.Mutex
	buf     bytes.Buffer
	dropped int
}

func (o *cappedOutput) Write(p []byte) (int, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	n := len(p)
	keep := min(n, maxOut-100-o.buf.Len())
	if keep > 0 {
		_, _ = o.buf.Write(p[:keep])
	}
	o.dropped += n - keep
	return n, nil
}

func (o *cappedOutput) String() string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.buf.String()
}

var _ io.Writer = (*cappedOutput)(nil)
