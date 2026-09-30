package tools

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"jevharness/internal/workspace"
)

func TestCompactCommandOutputAndBoundedRetrieval(t *testing.T) {
	t.Setenv("TEST_COMMAND_SECRET_KEY", "a-secret-to-redact")
	e := executor(t, "develop")
	out := "start\n" + strings.Repeat("a-secret-to-redact diagnostic\n", 500) + "[exit 7; changes staged, use /changes and /apply]"
	result, err := e.saveCommandResult(out, errors.New("command failed"))
	if err == nil || len(result) > 2400 || !strings.Contains(result, "exit 7") || strings.Contains(result, "a-secret-to-redact") {
		t.Fatalf("invalid compact result: %s, %v", result, err)
	}
	entries, err := os.ReadDir(filepath.Join(e.Workspace.Dir, "command-output"))
	if err != nil || len(entries) != 1 {
		t.Fatal(entries, err)
	}
	id := entries[0].Name()
	if !strings.Contains(result, id) {
		t.Fatal("missing log ID")
	}
	info, _ := entries[0].Info()
	if info.Mode().Perm() != 0600 {
		t.Fatal("log is not private")
	}
	log, err := e.Workspace.ReadCommandOutput(id)
	if err != nil || strings.Contains(log, "a-secret-to-redact") || !strings.Contains(log, "[REDACTED]") || !strings.Contains(log, "command failed") {
		t.Fatal("log redaction or error missing", err)
	}
	args, _ := json.Marshal(commandOutputArgs{ID: id, Offset: 2500, Limit: 100})
	e.Mode = "inspect"
	p, err := e.Prepare("read_command_output", args)
	if err != nil || p.NeedsApproval {
		t.Fatal("log read unavailable in inspect", err)
	}
	slice, err := e.Run(context.Background(), p)
	if err != nil || !strings.HasPrefix(slice, log[2500:2600]) || len(slice) > 200 {
		t.Fatal("unbounded or wrong log slice", slice, err)
	}
	changes, err := e.Workspace.Changes()
	if err != nil || len(changes) != 0 {
		t.Fatal("command log polluted staged changes", err)
	}
	clone, err := e.Workspace.Clone(filepath.Join(t.TempDir(), "fork"))
	if err != nil {
		t.Fatal(err)
	}
	cloned, err := clone.ReadCommandOutput(id)
	if err != nil || cloned != log {
		t.Fatal("fork lost command logs", err)
	}
	resumed, err := workspace.Open(e.Workspace.Source, e.Workspace.Dir)
	if err != nil {
		t.Fatal(err)
	}
	retained, err := resumed.ReadCommandOutput(id)
	if err != nil || retained != log {
		t.Fatal("resume lost command logs", err)
	}
}

func TestCommandOutputRejectsInvalidRequests(t *testing.T) {
	e := executor(t, "develop")
	for _, args := range []string{`{}`, `{"id":"x","offset":-1}`, `{"id":"x","limit":4001}`} {
		if _, err := e.Prepare("read_command_output", json.RawMessage(args)); err == nil {
			t.Fatal("accepted invalid request", args)
		}
	}
	for _, id := range []string{"../manifest.json", "/etc/passwd", "not-a-log"} {
		if _, err := e.Workspace.ReadCommandOutput(id); err == nil {
			t.Fatal("accepted invalid log ID", id)
		}
	}
	id, _, err := e.Workspace.SaveCommandOutput("abc")
	if err != nil {
		t.Fatal(err)
	}
	args, _ := json.Marshal(commandOutputArgs{ID: id, Offset: 1 << 30})
	out, err := e.readCommandOutput(args)
	if err != nil || !strings.Contains(out, "bytes 3–3 of 3") {
		t.Fatal("offset past end failed", out, err)
	}
}

func TestCommandPreviewPreservesUTF8AndTail(t *testing.T) {
	out := "start\n" + strings.Repeat("界", 2000) + "\nexit 1"
	preview := compactCommandPreview(out)
	if !utf8.ValidString(preview) || len(preview) > 2100 || !strings.HasSuffix(preview, "exit 1") || !strings.HasPrefix(preview, "start") {
		t.Fatal("invalid preview")
	}
}

func TestCompactOutputCaptureIsBounded(t *testing.T) {
	o := &cappedOutput{limit: 100}
	n, err := o.Write([]byte(strings.Repeat("x", 150)))
	if err != nil || n != 150 || len(o.String()) != 100 || o.dropped != 50 {
		t.Fatal("capture limit failed")
	}
	_, _ = o.Write([]byte("more"))
	if len(o.String()) != 100 || o.dropped != 54 {
		t.Fatal("capture grew past cap")
	}
}

func TestLiveDockerCompactCommandOutput(t *testing.T) {
	if os.Getenv("JEV_DOCKER_TEST") != "1" {
		t.Skip("set JEV_DOCKER_TEST=1 after jev sandbox build")
	}
	e := executor(t, "autonomous")
	e.DockerSandbox = true
	e.Image = "jev-harness-sandbox:1"
	e.CompactCommandOutput = true
	streamed := false
	e.Output = func(string) { streamed = true }
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	p, err := e.Prepare("bash", json.RawMessage(`{"command":"printf after > a.txt; python3 -c 'print(\"diagnostic\\n\" * 5000)'; exit 7"}`))
	if err != nil {
		t.Fatal(err)
	}
	out, err := e.Run(ctx, p)
	if err != nil || len(out) > 2400 || !strings.Contains(out, "exit 7") || !strings.Contains(out, "read_command_output") || streamed {
		t.Fatal("compact command execution failed", out, err)
	}
	entries, err := os.ReadDir(filepath.Join(e.Workspace.Dir, "command-output"))
	if err != nil || len(entries) != 1 {
		t.Fatal("command log missing", entries, err)
	}
	log, err := e.Workspace.ReadCommandOutput(entries[0].Name())
	if err != nil || len(log) < 50000 || !strings.Contains(log, "exit 7") {
		t.Fatal("full command output not retained", err)
	}
	staged, _ := os.ReadFile(filepath.Join(e.Workspace.Stage, "a.txt"))
	original, _ := os.ReadFile(filepath.Join(e.Workspace.Source, "a.txt"))
	if string(staged) != "after" || string(original) != "before" {
		t.Fatal("staging boundary changed")
	}
}
