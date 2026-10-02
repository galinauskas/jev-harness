// jev-harness: a Bubble Tea coding agent that routes every user turn
// through Jev to pick a provider and model role.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"jevharness/internal/redact"
	"jevharness/internal/session"
	"jevharness/internal/tools"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	tea "charm.land/bubbletea/v2"

	"jevharness/internal/agent"
	"jevharness/internal/config"
	"jevharness/internal/herdr"
	"jevharness/internal/openrouter"
	"jevharness/internal/router"
	"jevharness/internal/tui"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		fatal("%s", err)
	}
	if err := cfg.Validate(); err != nil {
		fatal("invalid config %s: %s", config.Path(), err)
	}
	args := os.Args[1:]
	flags := flag.NewFlagSet("jev", flag.ExitOnError)
	resume := flags.String("resume", "", "resume a saved session in the current project")
	mode := flags.String("mode", "", "inspect, develop or autonomous (resets on session changes)")
	flags.BoolVar(&cfg.Safety.Ephemeral, "ephemeral", cfg.Safety.Ephemeral, "do not persist conversation or staged workspace")
	flags.IntVar(&cfg.Limits.OutputTokens, "output-tokens", cfg.Limits.OutputTokens, "maximum output tokens per request")
	flags.IntVar(&cfg.Limits.TotalTokens, "token-budget", cfg.Limits.TotalTokens, "total turn token budget; estimates used when usage is missing")
	flags.IntVar(&cfg.Limits.Seconds, "time-budget", cfg.Limits.Seconds, "maximum seconds per turn")
	flags.Float64Var(&cfg.Limits.Cost, "cost-budget", cfg.Limits.Cost, "reported USD budget; unavailable cost stops further requests")
	flags.Usage = func() {
		fmt.Fprintln(os.Stderr, "Usage: jev [flags] [route <text>|doctor|sandbox build|sandbox prepare|eval <cases.jsonl>]")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		fatal("%s", err)
	}
	args = flags.Args()
	if *resume != "" && (len(args) > 0 || cfg.Safety.Ephemeral) {
		fatal("--resume requires a persistent interactive session")
	}
	cfg.ApplyDefaults()
	if err := cfg.Validate(); err != nil {
		fatal("%s", err)
	}
	key := cfg.Key() // empty is fine: the key can be entered in /settings
	cwd, err := os.Getwd()
	if err != nil {
		fatal("getwd: %v", err)
	}

	canonical, err := filepath.EvalSymlinks(cwd)
	if err != nil {
		fatal("workspace: %v", err)
	}
	cwd = canonical
	client := openrouter.New(key)
	r := router.New(client, cfg)

	if len(args) > 0 && args[0] == "sandbox" {
		if len(args) != 2 || (args[1] != "build" && args[1] != "prepare") {
			fatal("usage: jev sandbox build|prepare")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		if args[1] == "prepare" {
			if err := tools.PrepareSandbox(ctx, cfg.Safety.SandboxImage, cwd, cfg.APIKey, cfg.DeepSeekAPIKey, cfg.OpenCodeGoAPIKey, cfg.ExaAPIKey); err != nil {
				fatal("%s", err)
			}
			return
		}
		if err := tools.BuildSandbox(ctx, cfg.Safety.SandboxImage); err != nil {
			fatal("sandbox build: %s", err)
		}
		return
	}
	if len(args) > 0 && args[0] == "doctor" {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		fmt.Printf("Workspace: %s\nMode: %s; writes are staged\nRouting: auto routing sends prompt/recent context to OpenRouter\n", cwd, cfg.Safety.Mode)
		for _, role := range cfg.Roles {
			state := "key missing"
			if cfg.ProviderKey(role.Backend()) != "" {
				state = "key available"
			}
			fmt.Printf("%s: %s / %s (%s)\n", role.Name, role.Backend(), role.Model, state)
		}
		fmt.Printf("Web search: %s (%s)\n", cfg.WebSearchProvider(), cfg.WebSearchStatus())
		if !cfg.Safety.DockerSandbox {
			fmt.Println("Docker sandbox (experimental): off; shell runs locally with normal host and network access")
			return
		}
		if err := tools.CheckSandbox(ctx, cfg.Safety.SandboxImage); err != nil {
			fatal("%s", err)
		}
		fmt.Println("Docker sandbox (experimental) ready: no shell network; bounded tmpfs, memory, CPU and processes")
		return
	}
	if len(args) > 0 && args[0] == "eval" {
		if len(args) != 2 {
			fatal("usage: jev eval <cases.jsonl>")
		}
		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer cancel()
		if err := evaluate(ctx, cfg, cwd, args[1]); err != nil {
			fatal("evaluation: %s", err)
		}
		return
	}
	if len(args) > 0 && args[0] == "route" {
		if len(args) < 2 {
			fatal("usage: jev route <text>")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		dec := r.Route(ctx, router.State{Message: redact.New(cfg.APIKey, cfg.DeepSeekAPIKey, cfg.OpenCodeGoAPIKey, cfg.ExaAPIKey).Text(strings.Join(args[1:], " "))})
		out, _ := json.MarshalIndent(dec, "", "  ")
		fmt.Println(string(out))
		if dec.Err != nil {
			os.Exit(2)
		}
		return
	}
	if len(args) > 0 {
		fatal("usage: jev [route <text>]")
	}

	ag := agent.New(client, r, cfg, cwd)
	defer ag.Cleanup()
	if !cfg.Safety.Ephemeral {
		if err := session.DefaultStore().Prune(cfg.Safety.RetentionDays); err != nil {
			fatal("session retention: %s", err)
		}
	}
	app := tui.New(ag, cfg, cwd)
	if *resume != "" {
		if err := app.ResumeSession(*resume); err != nil {
			ag.Cleanup()
			fatal("resume: %s", err)
		}
	}
	if *mode != "" {
		if err := ag.SetMode(*mode); err != nil {
			fatal("%s", err)
		}
	}
	reporter := herdr.NewFromEnvironment()
	defer reporter.Close()
	if reporter != nil {
		app.SetHerdrReporter(reporter)
	}
	if key == "" {
		app.SetStatus("OpenRouter routing key not set — configure /settings (g), or pin a direct-provider role", true)
	}
	p := tea.NewProgram(app, tea.WithoutSignalHandler())
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(signals)
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-signals:
			p.Send(tui.ShutdownMsg{})
		case <-done:
		}
	}()
	if _, err := p.Run(); err != nil {
		app.Stop()
		ag.Cleanup()
		reporter.Close()
		fatal("tui: %v", err)
	}
}

func fatal(format string, a ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", a...)
	os.Exit(1)
}
