package tools

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"jevharness/internal/redact"
	"jevharness/internal/workspace"
)

// Preparation is an explicit operator command with build-time network access.
// Only Go lockfiles enter the build context. Generated agent commands remain offline.
func PrepareSandbox(ctx context.Context, image, source string, secrets ...string) error {
	if err := CheckSandbox(ctx, image); err != nil {
		return err
	}
	root, err := os.OpenRoot(source)
	if err != nil {
		return err
	}
	defer root.Close()
	dir, err := os.MkdirTemp("", "jev-dependencies-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	names := []string{}
	filter := redact.New(secrets...)
	for _, name := range []string{"go.mod", "go.sum"} {
		f, err := workspace.Read(root, name)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if filter.Text(string(f.Data)) != string(f.Data) {
			return errors.New("dependency lockfile contains a known credential")
		}
		if err = os.WriteFile(filepath.Join(dir, name), f.Data, 0600); err != nil {
			return err
		}
		names = append(names, name)
	}
	if len(names) == 0 || names[0] != "go.mod" {
		return errors.New("no root go.mod; prepare other dependency ecosystems in a trusted custom sandbox image")
	}
	dockerfile := "FROM " + image + "\nWORKDIR /deps\nCOPY " + strings.Join(names, " ") + " ./\nENV GOMODCACHE=/opt/jev-go-mod\nRUN go mod download && chmod -R a+rX /opt/jev-go-mod\nWORKDIR /workspace\n"
	if err = os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte(dockerfile), 0600); err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, "docker", "build", "-t", image, dir)
	cmd.Env = DockerEnv()
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err = cmd.Run(); err != nil {
		return fmt.Errorf("dependency preparation: %w", err)
	}
	return nil
}
func localDaemon(ctx context.Context) error {
	endpoint := os.Getenv("DOCKER_HOST")
	if endpoint == "" {
		cmd := exec.CommandContext(ctx, "docker", "context", "inspect", "--format", "{{.Endpoints.docker.Host}}")
		cmd.Env = DockerEnv()
		out, err := cmd.Output()
		if err != nil {
			return fmt.Errorf("Docker context unavailable: %w", err)
		}
		endpoint = strings.TrimSpace(string(out))
	}
	if !strings.HasPrefix(endpoint, "unix://") && !strings.HasPrefix(endpoint, "npipe://") {
		return errors.New("tool isolation requires a local Docker socket; remote engines are unsupported")
	}
	return nil
}
