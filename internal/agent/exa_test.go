package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"jevharness/internal/openrouter"
)

type exaTransport func(*http.Request) (*http.Response, error)

func (f exaTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestExaAgentLoopPolicyAndBudget(t *testing.T) {
	t.Setenv("EXA_API_KEY", "")
	for _, tc := range []struct {
		name                               string
		key, allowed, disableTools, budget bool
	}{
		{name: "enabled", key: true, allowed: true},
		{name: "no-key", allowed: true},
		{name: "forbidden", key: true},
		{name: "tools-disabled", key: true, allowed: true, disableTools: true},
		{name: "spending-budget", key: true, allowed: true, budget: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			searches, completions := 0, 0
			original := http.DefaultTransport
			http.DefaultTransport = exaTransport(func(r *http.Request) (*http.Response, error) {
				if r.URL.Host != "api.exa.ai" {
					return original.RoundTrip(r)
				}
				searches++
				if r.URL.Path != "/search" || r.Header.Get("x-api-key") != "saved-exa-secret" {
					t.Error("wrong Exa request")
				}
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"results":[{"title":"Go","url":"https://go.dev","highlights":["Go release"]}],"costDollars":{"total":0.005}}`))}, nil
			})
			defer func() { http.DefaultTransport = original }()
			a, s := testAgent(t, func(w http.ResponseWriter, r *http.Request) {
				completions++
				var req openrouter.ChatRequest
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
					t.Fatal(err)
				}
				enabled := tc.key && tc.allowed && !tc.disableTools
				found := false
				for _, def := range req.Tools {
					found = found || def.Function.Name == "web_search"
				}
				prompt := req.Messages[0].Content
				if enabled && !strings.Contains(prompt, "You have live web access") {
					t.Error("enabled search missing from system prompt")
				}
				if !enabled && strings.Contains(prompt, "You have live web access") {
					t.Error("system prompt advertised unavailable search")
				}
				if found != enabled {
					t.Errorf("web_search offered=%t expected=%t", found, enabled)
				}
				data, _ := json.Marshal(req)
				if strings.Contains(string(data), "saved-exa-secret") {
					t.Error("key sent to chat provider")
				}
				if completions == 1 {
					w.Header().Set("Content-Type", "text/event-stream")
					calls := `[{"index":0,"id":"search-1","type":"function","function":{"name":"web_search","arguments":"{\"query\":\"Go releases\"}"}}]`
					if tc.budget {
						calls = strings.TrimSuffix(calls, "]") + `,{"index":1,"id":"search-2","type":"function","function":{"name":"web_search","arguments":"{\"query\":\"Go docs\"}"}}]`
					}
					fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"tool_calls\":%s},\"finish_reason\":\"tool_calls\"}],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":2,\"total_tokens\":12,\"cost\":0}}\n\ndata: [DONE]\n\n", calls)
				} else {
					last := req.Messages[len(req.Messages)-1]
					if last.Role != "tool" {
						t.Error("search tool result missing")
					}
					if enabled && !strings.Contains(last.Content, "https://go.dev") {
						t.Error("source URL missing from continuation")
					}
					if !enabled && !strings.Contains(last.Content, "unknown tool") {
						t.Error("unavailable search was not denied")
					}
					finalResponse(w)
				}
			})
			defer s.Close()
			if tc.key {
				a.cfg.ExaAPIKey = "saved-exa-secret"
			}
			if !tc.allowed {
				a.cfg.Safety.Projects = map[string][]string{a.cwd: {"openrouter"}}
			}
			a.cfg.Roles[0].DisableTools = tc.disableTools
			if tc.budget {
				a.cfg.Limits.Cost = 0.004
			}
			_ = a.SetMode("inspect")
			done, failed := false, false
			for ev := range a.Submit(context.Background(), "Search Go releases") {
				if ev.Approve != nil {
					t.Fatal("read-only search required approval")
				}
				if ev.Kind == Error {
					failed = true
					if !tc.disableTools && !(tc.budget && strings.Contains(ev.Text, "spending budget")) {
						t.Fatal(ev.Text)
					}
				}
				done = done || ev.Kind == TurnDone
			}
			wantSearches := 0
			if tc.key && tc.allowed && !tc.disableTools {
				wantSearches = 1
			}
			if searches != wantSearches {
				t.Fatalf("search requests=%d expected=%d", searches, wantSearches)
			}
			if tc.budget {
				if !failed || a.usedCost != 0.005 || a.saved.PendingTool != "" || completions != 1 {
					t.Fatal("budget failed to stop a second paid search", a.usedCost, a.saved.PendingTool, completions)
				}
			} else if tc.disableTools {
				if !failed {
					t.Fatal("disabled tools executed")
				}
			} else if !done {
				t.Fatal("tool loop did not complete")
			}
		})
	}
}
