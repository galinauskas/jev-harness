package config

import (
	"math"
	"testing"
)

func TestSafetyValidation(t *testing.T) {
	cfg := Default()
	if cfg.Safety.Mode != "develop" || cfg.Limits.OutputTokens != 8192 {
		t.Fatal("defaults missing")
	}
	cfg.Limits.Cost = math.NaN()
	if cfg.Validate() == nil {
		t.Fatal("NaN budget accepted")
	}
	cfg.Limits.Cost = 0
	cfg.Safety.SandboxImage = "--privileged"
	if cfg.Validate() == nil {
		t.Fatal("image option accepted")
	}
}

func TestDockerSandboxDefaultsOff(t *testing.T) {
	cfg := Default()
	if cfg.Safety.DockerSandbox {
		t.Fatal("Docker enabled by default")
	}
	cfg.Safety.DockerSandbox = true
	cfg.ApplyDefaults()
	if !cfg.Safety.DockerSandbox {
		t.Fatal("explicit opt-in was lost")
	}
}
