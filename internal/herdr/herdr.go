// Package herdr reports Jev's lifecycle to its owning Herdr pane.
package herdr

import (
	"context"
	"io"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
)

const (
	source        = "jev-harness"
	agentName     = "jev"
	reportTimeout = 250 * time.Millisecond
)

// Snapshot contains lifecycle metadata only, never prompts or credentials.
type Snapshot struct {
	State      string
	Message    string
	SessionID  string
	ResumeArgv []string
}

type runner func(context.Context, string, []string) error

// Reporter serializes CLI reports and retains only the latest pending state.
type Reporter struct {
	mu              sync.Mutex
	bin, pane       string
	latest, pending *Snapshot
	closed          bool
	wake            chan struct{}
	done            chan struct{}
	ctx             context.Context
	cancel          context.CancelFunc
	run             runner
	now             func() time.Time
	seq             int64
	allowResume     bool
}

// NewFromEnvironment is inert unless all of Herdr's pane variables are present.
func NewFromEnvironment() *Reporter {
	bin, pane := os.Getenv("HERDR_BIN_PATH"), os.Getenv("HERDR_PANE_ID")
	if os.Getenv("HERDR_ENV") != "1" || bin == "" || pane == "" || os.Getenv("HERDR_SOCKET_PATH") == "" {
		return nil
	}
	r := newReporter(bin, pane, runCLI, time.Now)
	_, err := exec.LookPath("jev")
	r.allowResume = err == nil
	return r
}

func newReporter(bin, pane string, run runner, now func() time.Time) *Reporter {
	ctx, cancel := context.WithCancel(context.Background())
	r := &Reporter{bin: bin, pane: pane, run: run, now: now, ctx: ctx, cancel: cancel,
		wake: make(chan struct{}, 1), done: make(chan struct{}), allowResume: true}
	go r.loop()
	return r
}

// Report never waits for Herdr or starts concurrent CLI processes.
func (r *Reporter) Report(s Snapshot) {
	if r == nil || (s.State != "idle" && s.State != "working" && s.State != "blocked") {
		return
	}
	if !r.allowResume || !validResumeArgv(s.ResumeArgv) {
		s.ResumeArgv = nil
	}
	s.ResumeArgv = slices.Clone(s.ResumeArgv)
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed || (r.latest != nil && s.State == r.latest.State && s.Message == r.latest.Message && s.SessionID == r.latest.SessionID && slices.Equal(s.ResumeArgv, r.latest.ResumeArgv)) {
		return
	}
	r.latest, r.pending = &s, &s
	select {
	case r.wake <- struct{}{}:
	default:
	}
}

// Close cancels an in-flight report, releases the pane, and waits at most the
// bounded CLI timeouts. Session changes use Report, never Close.
func (r *Reporter) Close() {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.closed = true
	r.mu.Unlock()
	r.cancel()
	select {
	case r.wake <- struct{}{}:
	default:
	}
	<-r.done
}

func (r *Reporter) nextSeq() string {
	r.seq = max(r.seq+1, r.now().UnixMicro())
	return strconv.FormatInt(r.seq, 10)
}

func (r *Reporter) args(action string) []string {
	return []string{"pane", action, r.pane, "--source", source, "--agent", agentName, "--seq", r.nextSeq()}
}

func (r *Reporter) send(ctx context.Context, args []string) error {
	ctx, cancel := context.WithTimeout(ctx, reportTimeout)
	defer cancel()
	return r.run(ctx, r.bin, args)
}

func (r *Reporter) loop() {
	defer close(r.done)
	for range r.wake {
		r.mu.Lock()
		s, closed := r.pending, r.closed
		r.pending = nil
		r.mu.Unlock()
		if closed {
			_ = r.send(context.Background(), r.args("release-agent"))
			return
		}
		if s == nil {
			continue
		}
		if err := r.send(r.ctx, r.reportArgs(*s, true)); err != nil && len(s.ResumeArgv) > 0 && r.ctx.Err() == nil {
			// Pre-0.9.2 CLIs reject arguments after --. A fresh sequence also
			// makes this retry safe if the first request was partially accepted.
			r.mu.Lock()
			stale := r.pending != nil
			r.mu.Unlock()
			if !stale {
				_ = r.send(r.ctx, r.reportArgs(*s, false))
			}
		}
	}
}

func (r *Reporter) reportArgs(s Snapshot, resume bool) []string {
	args := append(r.args("report-agent"), "--state", s.State)
	if s.Message != "" {
		args = append(args, "--message", s.Message)
	}
	if s.SessionID != "" {
		args = append(args, "--agent-session-id", s.SessionID)
	}
	if resume && len(s.ResumeArgv) > 0 {
		args = append(args, "--")
		args = append(args, s.ResumeArgv...)
	}
	return args
}

func validResumeArgv(argv []string) bool {
	if len(argv) == 0 || len(argv) > 64 || argv[0] == "" || strings.ContainsAny(argv[0], "/\\ ") || strings.HasPrefix(argv[0], "-") {
		return false
	}
	size := 0
	for _, arg := range argv {
		size += len(arg) + 1
		if strings.ContainsRune(arg, '\'') || strings.ContainsFunc(arg, unicode.IsControl) {
			return false
		}
	}
	return size <= 8<<10
}

func runCLI(ctx context.Context, bin string, args []string) error {
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
	cmd.WaitDelay = 50 * time.Millisecond
	return cmd.Run()
}
