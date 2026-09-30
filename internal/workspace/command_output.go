package workspace

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"regexp"

	"jevharness/internal/redact"
)

const MaxCommandOutput = 1 << 20

var commandOutputID = regexp.MustCompile(`^[a-f0-9]{32}$`)

func (w *Workspace) SaveCommandOutput(output string) (string, string, error) {
	output = redact.New(w.secrets...).Text(output)
	if len(output) > MaxCommandOutput+1024 {
		return "", "", errors.New("command output exceeds log limit")
	}
	var token [16]byte
	if _, err := rand.Read(token[:]); err != nil {
		return "", "", err
	}
	id := hex.EncodeToString(token[:])
	return id, output, w.writeCommandOutput(id, []byte(output))
}

func (w *Workspace) writeCommandOutput(id string, output []byte) error {
	root, err := os.OpenRoot(w.Dir)
	if err != nil {
		return err
	}
	defer root.Close()
	if err = root.Mkdir("command-output", 0700); err != nil && !errors.Is(err, os.ErrExist) {
		return err
	}
	f, err := root.OpenFile("command-output/"+id, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	_, err = f.Write(output)
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	return errors.Join(err, closeErr)
}

func (w *Workspace) ReadCommandOutput(id string) (string, error) {
	if !commandOutputID.MatchString(id) {
		return "", errors.New("invalid command output ID")
	}
	root, err := os.OpenRoot(w.Dir)
	if err != nil {
		return "", err
	}
	defer root.Close()
	info, err := root.Lstat("command-output/" + id)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || info.Size() > MaxCommandOutput+1024 {
		return "", errors.New("invalid command output log")
	}
	f, err := root.Open("command-output/" + id)
	if err != nil {
		return "", err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, MaxCommandOutput+1025))
	if err != nil {
		return "", err
	}
	if len(data) > MaxCommandOutput+1024 {
		return "", errors.New("command output exceeds log limit")
	}
	return redact.New(w.secrets...).Text(string(data)), nil
}

func (w *Workspace) cloneCommandOutput(clone *Workspace) error {
	root, err := os.OpenRoot(w.Dir)
	if err != nil {
		return err
	}
	defer root.Close()
	dir, err := root.Open("command-output")
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer dir.Close()
	entries, err := dir.ReadDir(-1)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		output, err := w.ReadCommandOutput(entry.Name())
		if err != nil {
			return err
		}
		if err = clone.writeCommandOutput(entry.Name(), []byte(output)); err != nil {
			return err
		}
	}
	return nil
}
