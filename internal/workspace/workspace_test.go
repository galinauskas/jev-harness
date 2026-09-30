package workspace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixture(t *testing.T) *Workspace {
	t.Helper()
	source := t.TempDir()
	if err := os.WriteFile(filepath.Join(source, "a.txt"), []byte("original\n"), 0751); err != nil {
		t.Fatal(err)
	}
	w, err := Open(source, filepath.Join(t.TempDir(), "staged"))
	if err != nil {
		t.Fatal(err)
	}
	return w
}
func stage(t *testing.T, w *Workspace, path, text string) {
	t.Helper()
	root, err := os.OpenRoot(w.Stage)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	before, err := Current(root, path)
	if err != nil {
		t.Fatal(err)
	}
	mode := os.FileMode(0644)
	if before != nil {
		mode = before.Mode
	}
	if err = AtomicWrite(root, path, []byte(text), mode, Hash(before)); err != nil {
		t.Fatal(err)
	}
}
func TestApplyConflictAndSelectiveUndo(t *testing.T) {
	w := fixture(t)
	stage(t, w, "a.txt", "agent\n")
	stage(t, w, "new.txt", "new\n")
	original, _ := os.ReadFile(filepath.Join(w.Source, "a.txt"))
	if string(original) != "original\n" {
		t.Fatal("staging changed original")
	}
	if n, err := w.Apply([]string{"a.txt"}); err != nil || n != 1 {
		t.Fatalf("apply: %d %v", n, err)
	}
	info, _ := os.Stat(filepath.Join(w.Source, "a.txt"))
	if info.Mode().Perm() != 0751 {
		t.Fatal("permissions changed")
	}
	if _, err := os.Stat(filepath.Join(w.Source, "new.txt")); !os.IsNotExist(err) {
		t.Fatal("selective apply wrote other file")
	}
	if err := w.Undo("a.txt"); err != nil {
		t.Fatal(err)
	}
	restored, _ := os.ReadFile(filepath.Join(w.Source, "a.txt"))
	if string(restored) != "original\n" {
		t.Fatal("undo failed")
	}
	stage(t, w, "a.txt", "agent again")
	if err := os.WriteFile(filepath.Join(w.Source, "a.txt"), []byte("user edit"), 0751); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Apply(nil); err == nil {
		t.Fatal("overwrote user edit")
	}
	if _, err := os.Stat(filepath.Join(w.Source, "new.txt")); !os.IsNotExist(err) {
		t.Fatal("partial apply occurred before conflict validation")
	}
}
func TestUndoProtectsLaterEdits(t *testing.T) {
	w := fixture(t)
	stage(t, w, "a.txt", "agent")
	if _, err := w.Apply(nil); err != nil {
		t.Fatal(err)
	}
	stage(t, w, "a.txt", "later staged")
	if err := w.Undo("a.txt"); err == nil {
		t.Fatal("overwrote later staged change")
	}
	actual, _ := os.ReadFile(filepath.Join(w.Source, "a.txt"))
	if string(actual) != "agent" {
		t.Fatal("failed undo changed original")
	}
	if err := os.WriteFile(filepath.Join(w.Source, "a.txt"), []byte("user change"), 0751); err != nil {
		t.Fatal(err)
	}
	if err := w.Undo("a.txt"); err == nil {
		t.Fatal("overwrote later user change")
	}
}
func TestBoundaryAndSecretFiltering(t *testing.T) {
	w := fixture(t)
	for _, p := range []string{"../outside", "/etc/passwd", ".env", "a/.git/config", "secret.key"} {
		if _, err := Relative(p); err == nil {
			t.Fatalf("accepted %s", p)
		}
	}
	outside := filepath.Join(t.TempDir(), "secret")
	_ = os.WriteFile(outside, []byte("private"), 0600)
	_ = os.Symlink(outside, filepath.Join(w.Stage, "escape"))
	root, err := os.OpenRoot(w.Stage)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if _, err = Read(root, "escape"); err == nil {
		t.Fatal("read symlink")
	}
	if err = AtomicWrite(root, "escape", []byte("overwrite"), 0644, "missing"); err == nil {
		t.Fatal("wrote symlink")
	}
	data, _ := os.ReadFile(outside)
	if string(data) != "private" {
		t.Fatal("outside changed")
	}
	_ = os.WriteFile(filepath.Join(w.Source, ".env"), []byte("TOKEN=secret"), 0600)
	_ = os.WriteFile(filepath.Join(w.Source, "config.go"), []byte("const key = \"known-secret-1234\""), 0644)
	copy, err := Open(w.Source, filepath.Join(t.TempDir(), "copy"), "known-secret-1234")
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{".env", "config.go"} {
		if _, err = os.Stat(filepath.Join(copy.Stage, p)); !os.IsNotExist(err) {
			t.Fatalf("copied secret %s", p)
		}
	}
}
func TestReviewHashAndResume(t *testing.T) {
	w := fixture(t)
	root, _ := os.OpenRoot(w.Stage)
	defer root.Close()
	before, _ := Current(root, "a.txt")
	stage(t, w, "a.txt", "other edit")
	if err := AtomicWrite(root, "a.txt", []byte("stale"), 0644, Hash(before)); err == nil {
		t.Fatal("accepted stale review")
	}
	resumed, err := Open(w.Source, w.Dir)
	if err != nil {
		t.Fatal(err)
	}
	changes, err := resumed.Changes()
	if err != nil || len(changes) != 1 || !strings.Contains(Preview(changes[0]), "other edit") {
		t.Fatal("staging not recoverable", err)
	}
}

func TestForkKeepsIndependentStaging(t *testing.T) {
	w := fixture(t)
	stage(t, w, "a.txt", "branch one")
	fork, err := w.Clone(filepath.Join(t.TempDir(), "fork"))
	if err != nil {
		t.Fatal(err)
	}
	stage(t, fork, "a.txt", "branch two")
	a, _ := os.ReadFile(filepath.Join(w.Stage, "a.txt"))
	b, _ := os.ReadFile(filepath.Join(fork.Stage, "a.txt"))
	if string(a) != "branch one" || string(b) != "branch two" {
		t.Fatal("fork shares files")
	}
	if _, err = w.Apply(nil); err != nil {
		t.Fatal(err)
	}
	if _, err = fork.Apply(nil); err == nil {
		t.Fatal("fork overwrote another branch's applied changes")
	}
}
