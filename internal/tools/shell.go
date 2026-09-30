package tools

import (
	"archive/tar"
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"jevharness/internal/workspace"
)

//go:embed sandbox.Dockerfile
var SandboxDockerfile string

func DockerEnv() []string {
	out := []string{}
	for _, name := range []string{"PATH", "HOME", "DOCKER_HOST", "DOCKER_CONTEXT", "DOCKER_CONFIG", "TMPDIR", "SYSTEMROOT"} {
		if v, ok := os.LookupEnv(name); ok {
			out = append(out, name+"="+v)
		}
	}
	return out
}
func CheckSandbox(ctx context.Context, image string) error {
	if err := localDaemon(ctx); err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, "docker", "image", "inspect", "--format", "{{.Id}}", image)
	cmd.Env = DockerEnv()
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("Docker sandbox unavailable: %s; start Docker and run jev sandbox build", truncate(string(out)))
	}
	return nil
}
func BuildSandbox(ctx context.Context, image string) error {
	if err := localDaemon(ctx); err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, "docker", "build", "-t", image, "-")
	cmd.Env = DockerEnv()
	cmd.Stdin = strings.NewReader(SandboxDockerfile)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
func runBash(_ string, _ context.Context, _ json.RawMessage) (string, error) {
	return "", errors.New("shell requires the session Executor")
}

const sandboxScript = `set -eu
cp -R /input/. /workspace/
cd /workspace
set +e
timeout --kill-after=1s "$2"s sh -c "$1" >&2
code=$?
set -e
printf '%s' "$code" > /tmp/jev-exit
# GNU tar emits only regular files/directories to the host importer.
tar -cf - --exclude='.git' --exclude='.env*' --exclude='.ssh' --exclude='.aws' --exclude='.config' --exclude='.codex' --exclude='*.pem' --exclude='*.key' --exclude='.npmrc' --exclude='.netrc' --exclude='.pypirc' --exclude='node_modules' --exclude='vendor' --exclude='.cache' -C /workspace . -C /tmp jev-exit
exit "$code"
`

func (e *Executor) bash(ctx context.Context, args json.RawMessage) (string, error) {
	if !e.DockerSandbox {
		return e.localBash(ctx, args)
	}
	if strings.Contains(e.Workspace.Stage, ",") {
		return "", errors.New("workspace path cannot contain a comma for Docker mounts")
	}
	var p struct {
		Command string `json:"command"`
		Timeout int    `json:"timeout_seconds"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", err
	}
	if p.Timeout <= 0 {
		p.Timeout = 60
	}
	p.Timeout = min(p.Timeout, 600)
	ctx, cancel := context.WithTimeout(ctx, time.Duration(p.Timeout)*time.Second)
	defer cancel()
	if err := CheckSandbox(ctx, e.Image); err != nil {
		return "", err
	}
	id := fmt.Sprintf("jev-tool-%d-%d", os.Getpid(), time.Now().UnixNano())
	uid := os.Getuid()
	gid := os.Getgid()
	if uid <= 0 {
		uid = 1000
	}
	if gid < 0 {
		gid = 1000
	}
	cmd := exec.CommandContext(ctx, "docker", "run", "--rm", "--name", id, "--label", "io.jev-harness.tool=true", "--init", "--network=none", "--read-only", "--cap-drop=ALL", "--security-opt=no-new-privileges", "--pids-limit=128", "--memory=2g", "--memory-swap=2g", "--cpus=1", "--user", fmt.Sprintf("%d:%d", uid, gid), "--tmpfs", "/workspace:rw,exec,nosuid,nodev,size=512m,mode=1777", "--tmpfs", "/tmp:rw,exec,nosuid,nodev,size=1g,mode=1777", "--mount", "type=bind,src="+e.Workspace.Stage+",dst=/input,readonly", "--workdir", "/workspace", "--entrypoint", "sh", e.Image, "-c", sandboxScript, "--", p.Command, fmt.Sprint(max(1, p.Timeout-1)))
	cmd.Env = DockerEnv()
	configureProcess(cmd)
	cmd.WaitDelay = time.Second
	// Always remove the container: killing docker's client alone does not stop it.
	defer func() {
		cleanupCtx, c := context.WithTimeout(context.Background(), 5*time.Second)
		defer c()
		cleanup := exec.CommandContext(cleanupCtx, "docker", "rm", "-f", id)
		cleanup.Env = DockerEnv()
		_ = cleanup.Run()
	}()
	out := &cappedOutput{notify: e.Output}
	if e.CompactCommandOutput {
		out.limit = workspace.MaxCommandOutput
		// Compact mode emits just the final preview, keeping transcripts small too.
		out.notify = nil
	}
	cmd.Stderr = out
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		return "", err
	}
	if err = cmd.Start(); err != nil {
		return "", err
	}
	files, code, importErr := readArchive(io.LimitReader(pipe, workspace.MaxTotal+(4<<20)))
	if importErr != nil {
		cancel()
		_, _ = io.Copy(io.Discard, pipe)
	}
	runErr := cmd.Wait()
	s := strings.TrimRight(out.String(), "\n")
	if out.dropped > 0 {
		s += fmt.Sprintf("\n[truncated %d bytes]", out.dropped)
	}
	if ctx.Err() != nil {
		return s, fmt.Errorf("sandbox interrupted: %w; staged files preserved", ctx.Err())
	}
	if runErr != nil {
		var exitErr *exec.ExitError
		if !errors.As(runErr, &exitErr) {
			return s, fmt.Errorf("sandbox failed: %w; staged files preserved", runErr)
		}
	}
	if importErr != nil {
		return s, importErr
	}
	if cmd.ProcessState == nil || cmd.ProcessState.ExitCode() != code {
		return s, errors.New("sandbox completion status did not match process exit; staged files preserved")
	}
	// Populate a fresh tree, then replace the staged snapshot only after full validation.
	next, err := os.MkdirTemp(e.Workspace.Dir, ".import-*")
	if err != nil {
		return s, err
	}
	defer os.RemoveAll(next)
	if err = workspace.Populate(next, files); err != nil {
		return s, err
	}
	old := e.Workspace.Stage + ".previous"
	if err = os.Rename(e.Workspace.Stage, old); err != nil {
		return s, err
	}
	if err = os.Rename(next, e.Workspace.Stage); err != nil {
		_ = os.Rename(old, e.Workspace.Stage)
		return s, err
	}
	_ = os.RemoveAll(old)
	return s + fmt.Sprintf("\n[exit %d; changes staged, use /changes and /apply]", code), nil
}
func readArchive(r io.Reader) (map[string]workspace.File, int, error) {
	tr := tar.NewReader(r)
	files := map[string]workspace.File{}
	total := int64(0)
	code := -1
	count := 0
	completed := false
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, 0, err
		}
		count++
		if count > workspace.MaxFiles*2 {
			return nil, 0, errors.New("sandbox archive has too many entries")
		}
		if h.Name == "jev-exit" {
			if completed || h.Size > 10 {
				return nil, 0, errors.New("invalid sandbox exit record")
			}
			data, err := io.ReadAll(tr)
			if err != nil {
				return nil, 0, err
			}
			if _, err = fmt.Sscanf(string(data), "%d", &code); err != nil || code < 0 || code > 255 {
				return nil, 0, errors.New("invalid exit status")
			}
			completed = true
			continue
		}
		path, err := workspace.Relative(filepath.FromSlash(h.Name))
		if err != nil {
			return nil, 0, err
		}
		if path == "." {
			continue
		}
		if h.Typeflag == tar.TypeDir {
			continue
		}
		if h.Typeflag != tar.TypeReg || h.Size < 0 || h.Size > workspace.MaxFile {
			return nil, 0, errors.New("sandbox may return only regular files of at most 2 MiB")
		}
		if _, ok := files[path]; ok {
			return nil, 0, errors.New("duplicate sandbox archive path")
		}
		total += h.Size
		if total > workspace.MaxTotal || len(files) >= workspace.MaxFiles {
			return nil, 0, errors.New("sandbox workspace exceeds limits")
		}
		data, err := io.ReadAll(tr)
		if err != nil || int64(len(data)) != h.Size {
			return nil, 0, errors.New("incomplete sandbox file")
		}
		files[path] = workspace.File{Data: data, Mode: os.FileMode(h.Mode) & 0777}
	}
	if !completed {
		return nil, 0, errors.New("sandbox ended without a completion record; no changes imported")
	}
	return files, code, nil
}

type cappedOutput struct {
	limit   int
	mu      sync.Mutex
	buf     bytes.Buffer
	dropped int
	notify  func(string)
}

func (o *cappedOutput) Write(p []byte) (int, error) {
	o.mu.Lock()
	n := len(p)
	limit := o.limit
	if limit == 0 {
		limit = maxOut - 100
	}
	keep := min(n, max(0, limit-o.buf.Len()))
	if keep > 0 {
		_, _ = o.buf.Write(p[:keep])
	}
	o.dropped += n - keep
	notify := o.notify
	o.mu.Unlock()
	if keep > 0 && notify != nil {
		notify(string(p[:keep]))
	}
	return n, nil
}
func (o *cappedOutput) String() string { o.mu.Lock(); defer o.mu.Unlock(); return o.buf.String() }
