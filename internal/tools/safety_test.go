package tools

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"jevharness/internal/workspace"
)

func executor(t *testing.T, mode string) *Executor {
	t.Helper()
	source := t.TempDir()
	_ = os.WriteFile(filepath.Join(source, "a.txt"), []byte("before"), 0644)
	w, err := workspace.Open(source, filepath.Join(t.TempDir(), "work"))
	if err != nil {
		t.Fatal(err)
	}
	return &Executor{Workspace: w, Mode: mode, Image: "unavailable-image"}
}
func TestCentralPolicyAndReview(t *testing.T) {
	e := executor(t, "inspect")
	for _, name := range []string{"write_file", "edit_file", "bash"} {
		if _, err := e.Prepare(name, json.RawMessage(`{"path":"a.txt","content":"after","old_string":"before","new_string":"after","command":"true"}`)); err == nil {
			t.Fatalf("inspect allowed %s", name)
		}
	}
	p, err := e.Prepare("read_file", json.RawMessage(`{"path":"a.txt"}`))
	if err != nil || p.NeedsApproval {
		t.Fatal("inspection needs approval", err)
	}
	out, err := e.Run(context.Background(), p)
	if err != nil || !strings.Contains(out, "before") {
		t.Fatal(out, err)
	}
	e.Mode = "develop"
	p, err = e.Prepare("write_file", json.RawMessage(`{"path":"a.txt","content":"after"}`))
	if err != nil || !p.NeedsApproval || !strings.Contains(p.Review, "-before") || !strings.Contains(p.Review, "+after") {
		t.Fatal("missing diff", err)
	}
	_ = os.WriteFile(filepath.Join(e.Workspace.Stage, "a.txt"), []byte("later"), 0644)
	out, err = e.Run(context.Background(), p)
	if err != nil || !strings.Contains(out, "changed") {
		t.Fatal("stale reviewed change accepted", out, err)
	}
	e.Mode = "inspect"
	if _, err = e.Run(context.Background(), p); err == nil {
		t.Fatal("executor trusted stale UI approval")
	}
}
func TestHostShellDisabled(t *testing.T) {
	if _, err := runBash(t.TempDir(), context.Background(), json.RawMessage(`{"command":"true"}`)); err == nil {
		t.Fatal("host shell enabled")
	}
}
func archive(t *testing.T, path string, kind byte, size int64, completion bool) []byte {
	t.Helper()
	var b bytes.Buffer
	w := tar.NewWriter(&b)
	_ = w.WriteHeader(&tar.Header{Name: path, Mode: 0644, Typeflag: kind, Size: size, Linkname: "../../outside"})
	if kind == tar.TypeReg && size < 1000 {
		_, _ = w.Write(bytes.Repeat([]byte("a"), int(size)))
	}
	if completion {
		_ = w.WriteHeader(&tar.Header{Name: "jev-exit", Mode: 0600, Size: 1})
		_, _ = w.Write([]byte("0"))
	}
	_ = w.Close()
	return b.Bytes()
}
func TestArchiveRejectsEscapesLinksAndIncompleteResults(t *testing.T) {
	for _, test := range []struct {
		name, path string
		kind       byte
		complete   bool
	}{{"escape", "../outside", tar.TypeReg, true}, {"absolute", "/etc/passwd", tar.TypeReg, true}, {"secret", ".env", tar.TypeReg, true}, {"symlink", "link", tar.TypeSymlink, true}, {"hardlink", "link", tar.TypeLink, true}, {"incomplete", "a.txt", tar.TypeReg, false}} {
		t.Run(test.name, func(t *testing.T) {
			if _, _, err := readArchive(bytes.NewReader(archive(t, test.path, test.kind, 0, test.complete))); err == nil {
				t.Fatal("accepted unsafe archive")
			}
		})
	}
	files, code, err := readArchive(bytes.NewReader(archive(t, "./a.txt", tar.TypeReg, 3, true)))
	if err != nil || code != 0 || string(files["a.txt"].Data) != "aaa" {
		t.Fatal("valid archive failed", err)
	}
}
func TestDockerEnvironmentExcludesSecrets(t *testing.T) {
	t.Setenv("OPENROUTER_API_KEY", "private-value")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "another-private-value")
	for _, entry := range DockerEnv() {
		if strings.Contains(entry, "private-value") {
			t.Fatal("credential inherited")
		}
	}
}

func TestLiveDockerIsolation(t *testing.T) {
	if os.Getenv("JEV_DOCKER_TEST") != "1" {
		t.Skip("set JEV_DOCKER_TEST=1 after jev sandbox build")
	}
	e := executor(t, "autonomous")
	e.DockerSandbox = true
	e.Image = "jev-harness-sandbox:1"
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	p, err := e.Prepare("bash", json.RawMessage(`{"command":"test ! -e /input/.env && test ! -e /Users && test -z \"$OPENROUTER_API_KEY\" && printf after > a.txt && python3 -c 'import socket; s=socket.socket(); s.settimeout(1); result=s.connect_ex((\"1.1.1.1\",443)); assert result != 0'"}`))
	if err != nil {
		t.Fatal(err)
	}
	out, err := e.Run(ctx, p)
	if err != nil || !strings.Contains(out, "exit 0") {
		t.Fatal(out, err)
	}
	data, _ := os.ReadFile(filepath.Join(e.Workspace.Stage, "a.txt"))
	if string(data) != "after" {
		t.Fatal("shell change not staged")
	}
	original, _ := os.ReadFile(filepath.Join(e.Workspace.Source, "a.txt"))
	if string(original) != "before" {
		t.Fatal("shell changed original")
	}
}

func TestLiveDockerFailureAndTimeoutPreserveBoundary(t *testing.T) {
	if os.Getenv("JEV_DOCKER_TEST") != "1" {
		t.Skip("set JEV_DOCKER_TEST=1 after jev sandbox build")
	}
	e := executor(t, "autonomous")
	e.DockerSandbox = true
	e.Image = "jev-harness-sandbox:1"
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	p, err := e.Prepare("bash", json.RawMessage(`{"command":"printf changed > a.txt; exit 7"}`))
	if err != nil {
		t.Fatal(err)
	}
	out, err := e.Run(ctx, p)
	if err != nil || !strings.HasSuffix(out, "[exit 7; changes staged, use /changes and /apply]") {
		t.Fatal("nonzero result lost", out, err)
	}
	p, err = e.Prepare("bash", json.RawMessage(`{"command":"sleep 30","timeout_seconds":2}`))
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	out, err = e.Run(ctx, p)
	if time.Since(started) > 8*time.Second {
		t.Fatal("timeout failed")
	}
	if err == nil && !strings.Contains(out, "exit 124") {
		t.Fatal("timeout falsely succeeded", out)
	}
	original, _ := os.ReadFile(filepath.Join(e.Workspace.Source, "a.txt"))
	if string(original) != "before" {
		t.Fatal("failed command modified original")
	}
}

func TestLiveDockerProjectChecks(t *testing.T) {
	if os.Getenv("JEV_DOCKER_PROJECT_TEST") != "1" {
		t.Skip("set JEV_DOCKER_PROJECT_TEST=1 after jev sandbox prepare")
	}
	source, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	w, err := workspace.Open(source, filepath.Join(t.TempDir(), "project"))
	if err != nil {
		t.Fatal(err)
	}
	e := &Executor{DockerSandbox: true, Workspace: w, Mode: "autonomous", Image: "jev-harness-sandbox:1"}
	p, err := e.Prepare("bash", json.RawMessage(`{"command":"go test ./...","timeout_seconds":180}`))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 190*time.Second)
	defer cancel()
	out, err := e.Run(ctx, p)
	if err != nil || !strings.HasSuffix(out, "[exit 0; changes staged, use /changes and /apply]") {
		t.Fatal("offline project checks failed", out, err)
	}
}
