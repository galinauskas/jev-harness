package herdr

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

func receive(t *testing.T, ch <-chan []string) []string {
	t.Helper()
	select {
	case args := <-ch:
		return args
	case <-time.After(time.Second):
		t.Fatal("report did not arrive")
		return nil
	}
}

func argValue(args []string, flag string) string {
	if i := slices.Index(args, flag); i >= 0 && i+1 < len(args) {
		return args[i+1]
	}
	return ""
}

func TestLifecycleCoalescingSequenceAndRelease(t *testing.T) {
	calls := make(chan []string, 10)
	first, unblock := make(chan struct{}), make(chan struct{})
	r := newReporter("herdr-bin", "w1:p2", func(ctx context.Context, bin string, args []string) error {
		if bin != "herdr-bin" {
			t.Error("wrong binary")
		}
		calls <- slices.Clone(args)
		if argValue(args, "--state") == "idle" {
			select {
			case <-first:
			default:
				close(first)
				select {
				case <-unblock:
				case <-ctx.Done():
				}
			}
		}
		return nil
	}, func() time.Time { return time.Unix(100, 0) })
	t.Cleanup(r.Close)
	r.Report(Snapshot{State: "idle", ResumeArgv: []string{"jev"}})
	initial := receive(t, calls)
	<-first
	for _, state := range []string{"working", "blocked", "working"} {
		r.Report(Snapshot{State: state, SessionID: "session-2", ResumeArgv: []string{"jev", "--resume", "session-2"}})
	}
	close(unblock)
	latest := receive(t, calls)
	if argValue(latest, "--state") != "working" || argValue(latest, "--agent-session-id") != "session-2" || argValue(latest, "--source") != source || argValue(latest, "--agent") != "jev" {
		t.Fatal("latest report or identity lost", latest)
	}
	if i := slices.Index(latest, "--"); i < 0 || !slices.Equal(latest[i+1:], []string{"jev", "--resume", "session-2"}) {
		t.Fatal("resume command lost", latest)
	}
	r.Report(Snapshot{State: "working", SessionID: "session-2", ResumeArgv: []string{"jev", "--resume", "session-2"}})
	r.Close()
	release := receive(t, calls)
	if release[1] != "release-agent" {
		t.Fatal("duplicate report or missing release", release)
	}
	var previous int64
	for _, args := range [][]string{initial, latest, release} {
		seq, err := strconv.ParseInt(argValue(args, "--seq"), 10, 64)
		if err != nil || seq <= previous {
			t.Fatal("sequence did not increase", args)
		}
		previous = seq
	}
	if len(calls) != 0 {
		t.Fatal("stale queued reports sent")
	}
}

func TestOlderCLIAndUnavailableHerdr(t *testing.T) {
	calls := make(chan []string, 10)
	r := newReporter("old-herdr", "w1:p1", func(_ context.Context, _ string, args []string) error {
		calls <- slices.Clone(args)
		return errors.New("old CLI rejects resume argv or server is unavailable")
	}, time.Now)
	t.Cleanup(r.Close)
	r.Report(Snapshot{State: "blocked", Message: "Tool approval required", SessionID: "session", ResumeArgv: []string{"jev", "--resume", "session"}})
	withResume, fallback := receive(t, calls), receive(t, calls)
	if !slices.Contains(withResume, "--") || slices.Contains(fallback, "--") || argValue(fallback, "--state") != "blocked" || argValue(fallback, "--message") != "Tool approval required" || argValue(fallback, "--agent-session-id") != "session" {
		t.Fatal("state fallback lost metadata", fallback)
	}
	if argValue(withResume, "--seq") == argValue(fallback, "--seq") {
		t.Fatal("fallback reused sequence")
	}
	r.Close()
	if receive(t, calls)[1] != "release-agent" {
		t.Fatal("failed reports prevented release")
	}
}

func TestCloseCancelsHungReport(t *testing.T) {
	started := make(chan struct{})
	calls := make(chan []string, 10)
	r := newReporter("herdr", "pane", func(ctx context.Context, _ string, args []string) error {
		calls <- args
		if args[1] == "report-agent" {
			close(started)
			<-ctx.Done()
			return ctx.Err()
		}
		return nil
	}, time.Now)
	r.Report(Snapshot{State: "working"})
	<-started
	before := time.Now()
	r.Close()
	if time.Since(before) > time.Second || receive(t, calls)[1] != "report-agent" || receive(t, calls)[1] != "release-agent" {
		t.Fatal("shutdown blocked or failed to release")
	}
}

func TestEnvironmentGating(t *testing.T) {
	vars := []string{"HERDR_ENV", "HERDR_BIN_PATH", "HERDR_PANE_ID", "HERDR_SOCKET_PATH"}
	for _, missing := range vars {
		t.Run(missing, func(t *testing.T) {
			for _, name := range vars {
				t.Setenv(name, "present")
			}
			t.Setenv("HERDR_ENV", "1")
			t.Setenv(missing, "")
			r := NewFromEnvironment()
			if r != nil {
				r.Close()
				t.Fatal("integration enabled without complete pane environment")
			}
			// Nil reporters can safely be used by callers outside Herdr.
			r.Report(Snapshot{State: "idle"})
			r.Close()
		})
	}
}

func TestResumeValidation(t *testing.T) {
	invalid := [][]string{nil, {"/tmp/jev"}, {"jev", "bad'arg"}, {"jev", "bad\narg"}, {"jev", strings.Repeat("x", 8192)}, make([]string, 65)}
	for _, argv := range invalid {
		if validResumeArgv(argv) {
			t.Fatal("unsafe resume command accepted")
		}
	}
	if !validResumeArgv([]string{"jev", "--resume", "id", "--mode", "inspect"}) {
		t.Fatal("valid command rejected")
	}
}

func TestCLIHelper(t *testing.T) {
	if os.Getenv("JEV_HERDR_TEST_HELPER") != "1" {
		return
	}
	i := slices.Index(os.Args, "--")
	if i < 0 {
		os.Exit(2)
	}
	args := os.Args[i+1:]
	if len(args) > 0 && args[0] == "hang" {
		time.Sleep(time.Hour)
	}
	data, _ := json.Marshal(args)
	if err := os.WriteFile(os.Getenv("JEV_HERDR_TEST_OUTPUT"), data, 0600); err != nil {
		os.Exit(2)
	}
}

func TestRunCLIUsesLiteralArgumentsAndTimeout(t *testing.T) {
	t.Setenv("JEV_HERDR_TEST_HELPER", "1")
	output := filepath.Join(t.TempDir(), "argv.json")
	t.Setenv("JEV_HERDR_TEST_OUTPUT", output)
	args := []string{"pane", "report-agent", "w1:p2", "--message", "$(touch never-run); literal text"}
	helperArgs := append([]string{"-test.run=^TestCLIHelper$", "--"}, args...)
	if err := runCLI(context.Background(), os.Args[0], helperArgs); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(output)
	var got []string
	if err != nil || json.Unmarshal(data, &got) != nil || !slices.Equal(got, args) {
		t.Fatal("CLI changed literal argv", err, string(data))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	before := time.Now()
	if runCLI(ctx, os.Args[0], []string{"-test.run=^TestCLIHelper$", "--", "hang"}) == nil || time.Since(before) > time.Second {
		t.Fatal("CLI timeout not enforced")
	}
}
