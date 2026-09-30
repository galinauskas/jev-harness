package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"jevharness/internal/agent"
	"jevharness/internal/config"
	"jevharness/internal/openrouter"
	"jevharness/internal/router"
	"jevharness/internal/tools"
)

type evaluationCase struct {
	ID        string   `json:"id"`
	Prompt    string   `json:"prompt"`
	Followups []string `json:"followups"`
	Check     string   `json:"check"`
}
type evaluationResult struct {
	ID            string   `json:"id"`
	Strategy      string   `json:"strategy"`
	Models        []string `json:"models"`
	Completed     bool     `json:"completed"`
	Passed        bool     `json:"passed"`
	Tokens        int      `json:"reported_tokens"`
	Cost          float64  `json:"reported_cost_usd"`
	UsageComplete bool     `json:"usage_complete"`
	Seconds       float64  `json:"seconds"`
	Error         string   `json:"error,omitempty"`
}

// Evaluation deliberately uses fresh, ephemeral staged workspaces. Checks come
// from the operator's dataset and execute through the configured shell backend.
func evaluate(ctx context.Context, cfg config.Config, cwd, path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 4096), 1<<20)
	encoder := json.NewEncoder(os.Stdout)
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return err
		}
		if strings.TrimSpace(scanner.Text()) == "" {
			continue
		}
		var task evaluationCase
		if err = json.Unmarshal(scanner.Bytes(), &task); err != nil {
			return err
		}
		if task.ID == "" || task.Prompt == "" || task.Check == "" {
			return fmt.Errorf("each case requires id, prompt and an isolated check command")
		}
		for _, strategy := range []string{"auto", cfg.DefaultRole} {
			if err := ctx.Err(); err != nil {
				return err
			}
			runCfg := cfg
			runCfg.Safety.Ephemeral = true
			client := openrouter.New(runCfg.Key())
			ag := agent.New(client, router.New(client, runCfg), runCfg, cwd)
			if strategy != "auto" {
				ag.Pinned = strategy
			}
			_ = ag.SetMode("autonomous")
			result := evaluationResult{ID: task.ID, Strategy: strategy, UsageComplete: true}
			started := time.Now()
			prompts := append([]string{task.Prompt}, task.Followups...)
			for _, prompt := range prompts {
				done := false
				for ev := range ag.Submit(ctx, prompt) {
					switch ev.Kind {
					case agent.Routed:
						result.Models = append(result.Models, ev.Decision.Role.Backend()+":"+ev.Decision.Role.Model)
					case agent.UsageRecorded:
						if ev.Usage == nil {
							result.UsageComplete = false
						} else {
							result.Tokens += ev.Usage.TotalTokens
							result.Cost += ev.Usage.Cost
							result.UsageComplete = result.UsageComplete && ev.Usage.CostKnown
						}
					case agent.TurnDone:
						done = true
					case agent.Error:
						result.Error = ev.Text
					}
					if ev.Approve != nil {
						ev.Approve <- false
						result.Error = "evaluation unexpectedly required approval"
					}
				}
				if !done {
					break
				}
				result.Completed = true
			}
			if result.Error != "" {
				result.Completed = false
			}
			if result.Completed {
				w, e := ag.Workspace()
				if e != nil {
					result.Error = e.Error()
				} else {
					executor := tools.Executor{DockerSandbox: cfg.Safety.DockerSandbox, Workspace: w, Mode: "autonomous", Image: cfg.Safety.SandboxImage}
					args, _ := json.Marshal(map[string]any{"command": task.Check, "timeout_seconds": 60})
					p, e := executor.Prepare("bash", args)
					if e != nil {
						result.Error = e.Error()
					} else {
						ctx, cancel := context.WithTimeout(ctx, 65*time.Second)
						out, e := executor.Run(ctx, p)
						cancel()
						result.Passed = e == nil && strings.HasSuffix(out, "[exit 0; changes staged, use /changes and /apply]")
						if e != nil {
							result.Error = e.Error()
						} else if !result.Passed {
							result.Error = "isolated check failed"
						}
					}
				}
			}
			result.Seconds = time.Since(started).Seconds()
			ag.Cleanup()
			if err = encoder.Encode(result); err != nil {
				return err
			}
		}
	}
	return scanner.Err()
}
