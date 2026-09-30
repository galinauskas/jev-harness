package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"jevharness/internal/openrouter"
)

func testExa(t *testing.T, h http.HandlerFunc) *Exa {
	t.Helper()
	s := httptest.NewServer(h)
	t.Cleanup(s.Close)
	e := NewExa("test-exa-secret")
	e.endpoint = s.URL
	return e
}

func TestExaRequestAndExecutor(t *testing.T) {
	e := testExa(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.Header.Get("x-api-key") != "test-exa-secret" || r.Header.Get("Content-Type") != "application/json" {
			t.Error("incorrect request authentication or method")
		}
		var p map[string]any
		if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
			t.Fatal(err)
		}
		if p["query"] != "Go releases" || p["type"] != "auto" || p["numResults"] != float64(2) || p["contents"].(map[string]any)["highlights"] != true {
			t.Errorf("incorrect search body: %v", p)
		}
		if p["includeDomains"].([]any)[0] != "go.dev" || p["excludeDomains"].([]any)[0] != "example.org" {
			t.Error("domain filters not forwarded")
		}
		fmt.Fprint(w, `{"results":[{"title":"Go","url":"https://go.dev/doc/devel/release","publishedDate":"2026-09-01","highlights":["Release notes"]}],"costDollars":{"total":0.005}}`)
	})
	var usage *openrouter.Usage
	e.OnUsage = func(u *openrouter.Usage) { usage = u }
	executor := executor(t, "inspect")
	executor.WebSearch = e
	p, err := executor.Prepare("web_search", json.RawMessage(`{"query":"Go releases","num_results":2,"include_domains":["go.dev"],"exclude_domains":["example.org"]}`))
	if err != nil || p.NeedsApproval || !strings.Contains(p.Review, "Exa: Go releases") {
		t.Fatal(p, err)
	}
	out, err := executor.Run(context.Background(), p)
	if err != nil || !strings.Contains(out, "Release notes") || !strings.Contains(out, "https://go.dev/") || !json.Valid([]byte(out)) {
		t.Fatal(out, err)
	}
	if usage == nil || !usage.CostKnown || usage.Cost != 0.005 {
		t.Fatal("missing search cost", usage)
	}
	executor.WebSearch = nil
	if _, err := executor.Run(context.Background(), p); err == nil {
		t.Fatal("stale prepared search bypassed disabled search")
	}
}

func TestExaValidationAndAvailability(t *testing.T) {
	if NewExa("  ") != nil {
		t.Fatal("blank credential enabled search")
	}
	if _, ok := Find(t.TempDir(), "web_search"); ok {
		t.Fatal("search available without key")
	}
	for _, args := range []string{`{}`, `{"query":"  "}`, `{"query":"x","num_results":-1}`, `{"query":"x","num_results":11}`, `{"query":3}`, `null`, `{`} {
		if _, err := parseWebSearch(json.RawMessage(args)); err == nil {
			t.Errorf("accepted %s", args)
		}
	}
	p, err := parseWebSearch(json.RawMessage(`{"query":" x "}`))
	if err != nil || p.Query != "x" || p.NumResults != 5 {
		t.Fatal(p, err)
	}
}

func TestExaFailures(t *testing.T) {
	for _, code := range []int{400, 401, 402, 403, 429, 500} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			e := testExa(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(code)
				fmt.Fprint(w, "test-exa-secret provider detail")
			})
			called := false
			e.OnUsage = func(u *openrouter.Usage) {
				called = true
				if u != nil {
					t.Error("failed request claimed known cost")
				}
			}
			out, err := e.tool().Run(context.Background(), json.RawMessage(`{"query":"x"}`))
			if err != nil || !strings.Contains(out, fmt.Sprint(code)) || strings.Contains(out, "test-exa-secret") || !called {
				t.Fatal(out, err, called)
			}
		})
	}
	for _, body := range []string{`not json`, `{}`, `{"results":null}`, strings.Repeat("x", (1<<20)+1)} {
		e := testExa(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, body) })
		if _, err := e.search(context.Background(), json.RawMessage(`{"query":"x"}`)); err == nil {
			t.Fatal("accepted invalid or oversized response")
		}
	}
}

func TestExaCancellationRedactionAndUnknownCost(t *testing.T) {
	e := testExa(t, func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		if strings.Contains(string(data), "test-exa-secret") {
			t.Error("credential leaked in query")
		}
		fmt.Fprint(w, `{"results":[{"title":"test-exa-secret","url":"https://example.com"}]}`)
	})
	called := false
	e.OnUsage = func(u *openrouter.Usage) {
		called = true
		if u != nil {
			t.Error("missing cost treated as known")
		}
	}
	out, err := e.search(context.Background(), json.RawMessage(`{"query":"test-exa-secret"}`))
	if err != nil || strings.Contains(out, "test-exa-secret") || !called {
		t.Fatal(out, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = e.search(ctx, json.RawMessage(`{"query":"x"}`)); err != context.Canceled {
		t.Fatal("cancellation not propagated", err)
	}
}

func TestExaRedirectDoesNotForwardKey(t *testing.T) {
	reached := false
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { reached = true }))
	defer target.Close()
	e := testExa(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	})
	if _, err := e.search(context.Background(), json.RawMessage(`{"query":"x"}`)); err == nil || reached {
		t.Fatal("credential-bearing redirect followed", err)
	}
}

// Opt in to one real, billable request without logging credentials or contents.
func TestLiveExa(t *testing.T) {
	if os.Getenv("JEV_EXA_TEST") != "1" {
		t.Skip("set JEV_EXA_TEST=1 and EXA_API_KEY for a live search")
	}
	e := NewExa(os.Getenv("EXA_API_KEY"))
	if e == nil {
		t.Fatal("EXA_API_KEY is required")
	}
	out, err := e.search(context.Background(), json.RawMessage(`{"query":"site:go.dev Go release notes","num_results":1}`))
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		Results []struct {
			URL string `json:"url"`
		} `json:"results"`
	}
	if json.Unmarshal([]byte(out), &result) != nil || len(result.Results) != 1 || result.Results[0].URL == "" {
		t.Fatal("live search did not return a source URL")
	}
}
