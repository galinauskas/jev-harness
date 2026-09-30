package tools

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

const commandPreviewBytes = 2000

type commandOutputArgs struct {
	ID     string `json:"id"`
	Offset int    `json:"offset"`
	Limit  int    `json:"limit"`
}

func parseCommandOutput(args json.RawMessage) (commandOutputArgs, error) {
	var p commandOutputArgs
	if err := json.Unmarshal(args, &p); err != nil {
		return p, err
	}
	if p.ID == "" || p.Offset < 0 || p.Limit < 0 || p.Limit > 4000 {
		return p, errors.New("id is required; offset must be nonnegative; limit must be 0–4000 bytes")
	}
	if p.Limit == 0 {
		p.Limit = commandPreviewBytes
	}
	return p, nil
}

// Move byte boundaries forward to avoid splitting UTF-8 characters.
func outputBoundary(s string, n int) int {
	n = min(n, len(s))
	for n < len(s) && !utf8.RuneStart(s[n]) {
		n++
	}
	return n
}

func (e *Executor) saveCommandResult(out string, runErr error) (string, error) {
	log := out
	if runErr != nil {
		log += "\nerror: " + runErr.Error()
	}
	id, log, err := e.Workspace.SaveCommandOutput(log)
	if err != nil {
		return compactCommandPreview(out), errors.Join(runErr, fmt.Errorf("save command output: %w", err))
	}
	return compactCommandPreview(log) + fmt.Sprintf("\n[Command log: %s; %d bytes. Use read_command_output with id, offset and limit for details.]", id, len(log)), runErr
}

func compactCommandPreview(out string) string {
	if len(out) <= commandPreviewBytes {
		return out
	}
	head := outputBoundary(out, commandPreviewBytes/2)
	tail := outputBoundary(out, len(out)-commandPreviewBytes/2)
	return out[:head] + fmt.Sprintf("\n[%d bytes omitted from context]\n", tail-head) + strings.TrimLeft(out[tail:], "\n")
}

func (e *Executor) readCommandOutput(args json.RawMessage) (string, error) {
	p, err := parseCommandOutput(args)
	if err != nil {
		return "", err
	}
	log, err := e.Workspace.ReadCommandOutput(p.ID)
	if err != nil {
		return "", err
	}
	start := outputBoundary(log, p.Offset)
	end := min(len(log), start+p.Limit)
	for end > start && end < len(log) && !utf8.RuneStart(log[end]) {
		end--
	}
	if end == start && start < len(log) {
		return "", errors.New("limit is too small for the next UTF-8 character")
	}
	return log[start:end] + fmt.Sprintf("\n[bytes %d–%d of %d; next offset %d]", start, end, len(log), end), nil
}
