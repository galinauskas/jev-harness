// jev-harness: a Bubble Tea coding agent that routes every user turn
// through Jev to pick a provider and model role.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"jevharness/internal/agent"
	"jevharness/internal/config"
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
	key := cfg.Key() // empty is fine: the key can be entered in /settings
	cwd, err := os.Getwd()
	if err != nil {
		fatal("getwd: %v", err)
	}

	client := openrouter.New(key)
	r := router.New(client, cfg)

	if len(os.Args) > 1 && os.Args[1] == "route" {
		if len(os.Args) < 3 {
			fatal("usage: jev route <text>")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		dec := r.Route(ctx, router.State{Message: strings.Join(os.Args[2:], " ")})
		out, _ := json.MarshalIndent(dec, "", "  ")
		fmt.Println(string(out))
		if dec.Err != nil {
			os.Exit(2)
		}
		return
	}
	if len(os.Args) > 1 {
		fatal("usage: jev [route <text>]")
	}

	ag := agent.New(client, r, cfg, cwd)
	app := tui.New(ag, cfg, cwd)
	if key == "" {
		app.SetStatus("OpenRouter routing key not set — configure /settings (g), or pin a direct-provider role", true)
	}
	p := tea.NewProgram(app)
	if _, err := p.Run(); err != nil {
		fatal("tui: %v", err)
	}
}

func fatal(format string, a ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", a...)
	os.Exit(1)
}
