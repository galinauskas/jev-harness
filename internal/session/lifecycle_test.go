package session

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDurableSessionLifecycle(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	id, _ := NewID()
	v := Session{ID: id, CWD: "/project", Title: "first", Updated: time.Now(), Running: true, PendingTool: "call"}
	if err := s.Save(v); err != nil {
		t.Fatal(err)
	}
	if err := s.Append(id, Record{Kind: "tool-started", ToolID: "call"}); err != nil {
		t.Fatal(err)
	}
	loaded, err := s.Load(id)
	if err != nil || !loaded.Running || loaded.PendingTool != "call" {
		t.Fatal("checkpoint lost", err)
	}
	records, err := s.Events(id)
	if err != nil || len(records) != 1 {
		t.Fatal("journal lost", err)
	}
	if err = s.Rename(id, "renamed"); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(filepath.Join(s.Dir, id+".json"))
	if info.Mode().Perm() != 0600 {
		t.Fatal("session is not private")
	}
	bad, _ := NewID()
	_ = os.WriteFile(filepath.Join(s.Dir, bad+".json"), []byte("bad json"), 0600)
	list, err := s.List("/project")
	if err != nil || len(list) != 1 {
		t.Fatal("one corrupt session broke listing", err)
	}
	if err = s.Delete(id); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Load(id); !os.IsNotExist(err) {
		t.Fatal("delete failed")
	}
	if err = s.Delete("../outside"); err == nil {
		t.Fatal("invalid ID accepted")
	}
}
