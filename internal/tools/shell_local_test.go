package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLocalShellDefaultStagesChangesAndReturnsExit(t *testing.T) {
	e := executor(t, "develop")
	p, err := e.Prepare("bash", json.RawMessage(`{"command":"printf after > a.txt; printf diagnostic; exit 7"}`))
	if err != nil || !p.NeedsApproval || !strings.Contains(p.Review, "normal host and network access") {
		t.Fatal(p, err)
	}
	out, err := e.Run(context.Background(), p)
	if err != nil || !strings.Contains(out, "diagnostic") || !strings.Contains(out, "exit 7") {
		t.Fatal(out, err)
	}
	staged, _ := os.ReadFile(filepath.Join(e.Workspace.Stage, "a.txt"))
	original, _ := os.ReadFile(filepath.Join(e.Workspace.Source, "a.txt"))
	if string(staged) != "after" || string(original) != "before" {
		t.Fatal("relative changes did not remain staged")
	}
	e.DockerSandbox = true
	p, err = e.Prepare("bash", json.RawMessage(`{"command":"true"}`))
	if err != nil || !strings.Contains(p.Review, "experimental Docker sandbox") {
		t.Fatal(p, err)
	}
	// No available image: opting in must fail rather than run locally.
	_, err = e.Run(context.Background(), p)
	if err == nil {
		t.Fatal("Docker mode fell back to local execution")
	}
}

func TestLocalShellCancellationAndCompactLogs(t *testing.T) {
	e := executor(t, "autonomous")
	e.CompactCommandOutput = true
	streamed := false
	e.Output = func(string) { streamed = true }
	p, _ := e.Prepare("bash", json.RawMessage(`{"command":"printf hello"}`))
	out, err := e.Run(context.Background(), p)
	if err != nil || !strings.Contains(out, "Command log:") || streamed {
		t.Fatal(out, err)
	}
	entries, err := os.ReadDir(filepath.Join(e.Workspace.Dir, "command-output"))
	if err != nil || len(entries) != 1 {
		t.Fatal(entries, err)
	}
	log, err := e.Workspace.ReadCommandOutput(entries[0].Name())
	if err != nil || !strings.Contains(log, "hello") {
		t.Fatal(log, err)
	}
	p, _ = e.Prepare("bash", json.RawMessage(`{"command":"sleep 30","timeout_seconds":1}`))
	started := time.Now()
	_, err = e.Run(context.Background(), p)
	if err == nil || !strings.Contains(err.Error(), "interrupted") || time.Since(started) > 5*time.Second {
		t.Fatal("timeout failed", err)
	}
}
