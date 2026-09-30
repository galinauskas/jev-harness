// Package workspace stages bounded, secret-filtered copies and applies reviewed changes.
package workspace

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"jevharness/internal/config"
	"jevharness/internal/privatefile"
	"jevharness/internal/redact"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const MaxFile = 2 << 20
const MaxTotal = 128 << 20
const MaxFiles = 10000

type File struct {
	Data []byte      `json:"data"`
	Mode fs.FileMode `json:"mode"`
}
type Change struct {
	Path          string
	Before, After *File
}
type Workspace struct {
	Source, Dir, Stage string
	base               map[string]File
	secrets            []string
}
type manifest struct {
	Source  string
	Base    map[string]File
	Applied []Change
}

// Protected applies equally to reads, writes, attachments and shell imports.
func Protected(path string) bool {
	for _, part := range strings.Split(filepath.ToSlash(path), "/") {
		p := strings.ToLower(part)
		if p == ".git" || p == ".ssh" || p == ".aws" || p == ".config" || p == ".codex" || p == ".jev" || p == "credentials" || p == "auth.json" || p == "id_rsa" || p == "id_ed25519" || p == ".npmrc" || p == ".netrc" || p == ".pypirc" || strings.HasPrefix(p, ".env") || strings.HasSuffix(p, ".pem") || strings.HasSuffix(p, ".key") {
			return true
		}
	}
	return false
}
func Relative(path string) (string, error) {
	if path == "" {
		path = "."
	}
	path = filepath.Clean(path)
	if filepath.IsAbs(path) || path == ".." || strings.HasPrefix(path, ".."+string(filepath.Separator)) || Protected(path) {
		return "", fmt.Errorf("path is outside the permitted workspace or protected: %q", path)
	}
	return path, nil
}
func Open(source, dir string, secrets ...string) (*Workspace, error) {
	source, err := filepath.EvalSymlinks(source)
	if err != nil {
		return nil, err
	}
	w := &Workspace{Source: source, Dir: dir, Stage: filepath.Join(dir, "work"), secrets: secrets}
	if data, err := readPrivate(filepath.Join(dir, "manifest.json")); err == nil {
		var m manifest
		if err = json.Unmarshal(data, &m); err != nil {
			return nil, err
		}
		if m.Source != source {
			return nil, errors.New("staged workspace belongs to another project")
		}
		w.base = m.Base
		if _, err = os.Stat(w.Stage); errors.Is(err, os.ErrNotExist) {
			if e := os.Rename(w.Stage+".previous", w.Stage); e != nil {
				return nil, err
			}
		} else if err != nil {
			return nil, err
		}
		return w, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	files, err := sourceSnapshot(source, secrets)
	if err != nil {
		return nil, err
	}
	if err = os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	if err = os.RemoveAll(w.Stage); err != nil {
		return nil, err
	}
	if err = os.Mkdir(w.Stage, 0700); err != nil {
		return nil, err
	}
	if err = Populate(w.Stage, files); err != nil {
		return nil, err
	}
	w.base = files
	if err = w.save(manifest{Source: source, Base: files}); err != nil {
		return nil, err
	}
	return w, nil
}
func readPrivate(path string) ([]byte, error) { return privatefile.Read(path, MaxTotal*2) }

func Snapshot(dir string) (map[string]File, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	files := map[string]File{}
	total := 0
	err = fs.WalkDir(root.FS(), ".", func(path string, d fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if path == "." {
			return nil
		}
		if Protected(path) || d.Name() == "node_modules" || d.Name() == "vendor" || d.Name() == ".cache" || d.Name() == "jev" {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if configDir := filepath.Dir(config.Path()); configDir == filepath.Join(dir, filepath.FromSlash(path)) {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		f, e := Read(root, path)
		if e != nil {
			return fmt.Errorf("snapshot %s: %w", path, e)
		}
		total += len(f.Data)
		if total > MaxTotal || len(files) >= MaxFiles {
			return errors.New("workspace exceeds 128 MiB or 10,000 files; use a smaller project directory")
		}
		files[path] = *f
		return nil
	})
	return files, err
}
func NoLinks(root *os.Root, path string) error {
	path, err := Relative(path)
	if err != nil {
		return err
	}
	parts := strings.Split(path, string(filepath.Separator))
	current := ""
	for _, p := range parts {
		current = filepath.Join(current, p)
		info, e := root.Lstat(current)
		if errors.Is(e, os.ErrNotExist) {
			return nil
		}
		if e != nil {
			return e
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return errors.New("symbolic links are not permitted")
		}
	}
	return nil
}
func Read(root *os.Root, path string) (*File, error) {
	if err := NoLinks(root, path); err != nil {
		return nil, err
	}
	f, err := root.OpenFile(path, os.O_RDONLY|nonblockFlag, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > MaxFile {
		return nil, errors.New("expected a regular file of at most 2 MiB")
	}
	data, err := io.ReadAll(io.LimitReader(f, MaxFile+1))
	if err != nil {
		return nil, err
	}
	if len(data) > MaxFile {
		return nil, errors.New("file exceeds 2 MiB")
	}
	return &File{Data: data, Mode: info.Mode().Perm()}, nil
}
func Hash(f *File) string {
	if f == nil {
		return "missing"
	}
	h := sha256.Sum256(f.Data)
	return hex.EncodeToString(h[:])
}
func Equal(a, b *File) bool { return Hash(a) == Hash(b) && (a == nil || b == nil || a.Mode == b.Mode) }
func Current(root *os.Root, path string) (*File, error) {
	f, e := Read(root, path)
	if errors.Is(e, os.ErrNotExist) {
		return nil, nil
	}
	return f, e
}

// AtomicWrite validates the reviewed contents again before replacement.
func AtomicWrite(root *os.Root, path string, data []byte, mode fs.FileMode, expected string) error {
	if _, err := Relative(path); err != nil {
		return err
	}
	if len(data) > MaxFile {
		return errors.New("content exceeds 2 MiB")
	}
	if err := NoLinks(root, path); err != nil {
		return err
	}
	before, err := Current(root, path)
	if err != nil {
		return err
	}
	if Hash(before) != expected {
		return errors.New("file changed since review")
	}
	if err = root.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	var id [12]byte
	if _, err = rand.Read(id[:]); err != nil {
		return err
	}
	tmp := filepath.Join(filepath.Dir(path), ".jev-write-"+hex.EncodeToString(id[:]))
	f, err := root.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode.Perm())
	if err != nil {
		return err
	}
	defer root.Remove(tmp)
	if err = f.Chmod(mode.Perm()); err == nil {
		_, err = f.Write(data)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	current, err := Current(root, path)
	if err != nil {
		return err
	}
	if Hash(current) != expected {
		return errors.New("file changed during write")
	}
	return root.Rename(tmp, path)
}
func Populate(dir string, files map[string]File) error {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer root.Close()
	for path, f := range files {
		if err = AtomicWrite(root, path, f.Data, f.Mode, "missing"); err != nil {
			return err
		}
	}
	return nil
}
func (w *Workspace) Changes() ([]Change, error) {
	files, err := Snapshot(w.Stage)
	if err != nil {
		return nil, err
	}
	var changes []Change
	for path, b := range w.base {
		a, ok := files[path]
		if !ok {
			bb := b
			changes = append(changes, Change{Path: path, Before: &bb})
		} else if !Equal(&b, &a) {
			bb, aa := b, a
			changes = append(changes, Change{path, &bb, &aa})
		}
		delete(files, path)
	}
	for path, a := range files {
		aa := a
		changes = append(changes, Change{Path: path, After: &aa})
	}
	sort.Slice(changes, func(i, j int) bool { return changes[i].Path < changes[j].Path })
	return changes, nil
}
func (w *Workspace) save(m manifest) error {
	data, err := json.Marshal(m)
	if err != nil {
		return err
	}
	if len(data) > MaxTotal*2 {
		return errors.New("workspace history exceeds 256 MiB; start a new session")
	}
	tmp, err := os.CreateTemp(w.Dir, ".manifest-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err = tmp.Write(data); err == nil {
		err = tmp.Sync()
	}
	ce := tmp.Close()
	if err != nil {
		return err
	}
	if ce != nil {
		return ce
	}
	return os.Rename(tmp.Name(), filepath.Join(w.Dir, "manifest.json"))
}
func (w *Workspace) loadManifest() (manifest, error) {
	var m manifest
	data, err := readPrivate(filepath.Join(w.Dir, "manifest.json"))
	if err == nil {
		err = json.Unmarshal(data, &m)
	}
	return m, err
}
func (w *Workspace) Apply(paths []string) (int, error) {
	changes, err := w.Changes()
	if err != nil {
		return 0, err
	}
	if len(paths) > 0 {
		wanted := map[string]bool{}
		for _, p := range paths {
			p, e := Relative(p)
			if e != nil {
				return 0, e
			}
			wanted[p] = true
		}
		filtered := changes[:0]
		for _, c := range changes {
			if wanted[c.Path] {
				filtered = append(filtered, c)
				delete(wanted, c.Path)
			}
		}
		if len(wanted) > 0 {
			return 0, errors.New("selected path has no staged changes")
		}
		changes = filtered
	}
	root, err := os.OpenRoot(w.Source)
	if err != nil {
		return 0, err
	}
	defer root.Close()
	for _, c := range changes {
		current, e := Current(root, c.Path)
		if e != nil {
			return 0, e
		}
		if !Equal(current, c.Before) {
			return 0, fmt.Errorf("conflict in %s; original project changed", c.Path)
		}
	}
	m, err := w.loadManifest()
	if err != nil {
		return 0, err
	}
	// Save the undo record before the first filesystem mutation.
	m.Applied = append(m.Applied, changes...)
	if err = w.save(m); err != nil {
		return 0, err
	}
	n := 0
	for _, c := range changes {
		if c.After == nil {
			current, e := Current(root, c.Path)
			if e != nil {
				return n, e
			}
			if !Equal(current, c.Before) {
				return n, fmt.Errorf("conflict in %s", c.Path)
			}
			err = root.Remove(c.Path)
		} else {
			err = AtomicWrite(root, c.Path, c.After.Data, c.After.Mode, Hash(c.Before))
		}
		if err != nil {
			return n, err
		}
		n++
		if c.After == nil {
			delete(w.base, c.Path)
		} else {
			w.base[c.Path] = *c.After
		}
		m.Base = w.base
		if err = w.save(m); err != nil {
			return n, err
		}
	}
	return n, nil
}
func (w *Workspace) Undo(path string) error {
	path, err := Relative(path)
	if err != nil {
		return err
	}
	m, err := w.loadManifest()
	if err != nil {
		return err
	}
	idx := -1
	for i := len(m.Applied) - 1; i >= 0; i-- {
		if m.Applied[i].Path == path {
			idx = i
			break
		}
	}
	if idx < 0 {
		return errors.New("no applied change for this path")
	}
	c := m.Applied[idx]
	root, err := os.OpenRoot(w.Source)
	if err != nil {
		return err
	}
	defer root.Close()
	current, err := Current(root, path)
	if err != nil {
		return err
	}
	if !Equal(current, c.After) {
		return errors.New("undo would overwrite subsequent changes")
	}
	stage, err := os.OpenRoot(w.Stage)
	if err != nil {
		return err
	}
	defer stage.Close()
	staged, err := Current(stage, path)
	if err != nil {
		return err
	}
	if !Equal(staged, c.After) {
		return errors.New("undo would overwrite later staged edits; review or discard them first")
	}
	if c.Before == nil {
		err = root.Remove(path)
	} else {
		err = AtomicWrite(root, path, c.Before.Data, c.Before.Mode, Hash(current))
	}
	if err != nil {
		return err
	}
	if c.Before == nil {
		err = stage.Remove(path)
		delete(w.base, path)
	} else {
		err = AtomicWrite(stage, path, c.Before.Data, c.Before.Mode, Hash(staged))
		w.base[path] = *c.Before
	}
	if err != nil {
		return err
	}
	m.Base = w.base
	m.Applied = append(m.Applied[:idx], m.Applied[idx+1:]...)
	return w.save(m)
}
func (w *Workspace) Discard() error {
	files, err := sourceSnapshot(w.Source, w.secrets)
	if err != nil {
		return err
	}
	m, err := w.loadManifest()
	if err != nil {
		return err
	}
	next, err := os.MkdirTemp(w.Dir, ".refresh-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(next)
	if err = Populate(next, files); err != nil {
		return err
	}
	old := w.Stage + ".previous"
	_ = os.RemoveAll(old)
	if err = os.Rename(w.Stage, old); err != nil {
		return err
	}
	if err = os.Rename(next, w.Stage); err != nil {
		_ = os.Rename(old, w.Stage)
		return err
	}
	w.base = files
	m.Base = files
	if err = w.save(m); err != nil {
		_ = os.RemoveAll(w.Stage)
		_ = os.Rename(old, w.Stage)
		return err
	}
	return os.RemoveAll(old)
}
func Preview(c Change) string {
	var b strings.Builder
	fmt.Fprintf(&b, "--- %s (original)\n+++ %s (staged)\n", c.Path, c.Path)
	var before, after []string
	if c.Before != nil {
		before = strings.Split(string(c.Before.Data), "\n")
	}
	if c.After != nil {
		after = strings.Split(string(c.After.Data), "\n")
	}
	prefix := 0
	for prefix < len(before) && prefix < len(after) && before[prefix] == after[prefix] {
		prefix++
	}
	suffix := 0
	for suffix < len(before)-prefix && suffix < len(after)-prefix && before[len(before)-1-suffix] == after[len(after)-1-suffix] {
		suffix++
	}
	start := max(0, prefix-3)
	fmt.Fprintf(&b, "@@ -%d +%d @@\n", start+1, start+1)
	for i := start; i < prefix; i++ {
		fmt.Fprintf(&b, " %s\n", before[i])
	}
	for i := prefix; i < len(before)-suffix; i++ {
		fmt.Fprintf(&b, "-%s\n", before[i])
	}
	for i := prefix; i < len(after)-suffix; i++ {
		fmt.Fprintf(&b, "+%s\n", after[i])
	}
	for i := 0; i < min(3, suffix); i++ {
		fmt.Fprintf(&b, " %s\n", before[len(before)-suffix+i])
	}
	if c.Before != nil && c.After != nil && c.Before.Mode != c.After.Mode {
		fmt.Fprintf(&b, "mode %04o → %04o\n", c.Before.Mode, c.After.Mode)
	}
	return b.String()
}

func sourceSnapshot(dir string, secrets []string) (map[string]File, error) {
	configDir := filepath.Dir(config.Path())
	if dir == configDir {
		return nil, errors.New("the harness configuration directory cannot be a workspace")
	}
	files, err := Snapshot(dir)
	if err != nil {
		return nil, err
	}
	r := redact.New(secrets...)
	for path, f := range files {
		if r.Text(string(f.Data)) != string(f.Data) {
			delete(files, path)
		}
	}
	return files, nil
}

// Clone copies conversation-associated staged state; it never rewinds project files.
func (w *Workspace) Clone(dir string) (*Workspace, error) {
	files, err := Snapshot(w.Stage)
	if err != nil {
		return nil, err
	}
	if err = os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	stage := filepath.Join(dir, "work")
	if err = os.Mkdir(stage, 0700); err != nil {
		return nil, err
	}
	if err = Populate(stage, files); err != nil {
		return nil, err
	}
	base := map[string]File{}
	for path, f := range w.base {
		base[path] = f
	}
	clone := &Workspace{Source: w.Source, Dir: dir, Stage: stage, base: base, secrets: w.secrets}
	if err = clone.save(manifest{Source: w.Source, Base: base}); err != nil {
		return nil, err
	}
	return clone, nil
}