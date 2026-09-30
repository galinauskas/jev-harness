package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"jevharness/internal/openrouter"
)

func testBrave(t *testing.T, h http.HandlerFunc) *WebSearch {
	t.Helper()
	server := httptest.NewServer(h)
	t.Cleanup(server.Close)
	search := NewBrave("test-brave-secret")
	search.endpoint = server.URL
	return search
}

func TestBraveRequestAndExecutor(t *testing.T) {
	search := testBrave(t, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if r.Method != "GET" || r.Header.Get("X-Subscription-Token") != "test-brave-secret" || r.Header.Get("Accept") != "application/json" {
			t.Error("wrong method or credentials")
		}
		if q.Get("q") != "Go releases (site:go.dev OR site:golang.org) -site:example.org" || q.Get("count") != "2" || q.Get("extra_snippets") != "true" || q.Get("text_decorations") != "false" || q.Get("result_filter") != "web" {
			t.Error("incorrect query parameters", q)
		}
		fmt.Fprint(w, `{"type":"search","web":{"results":[{"title":"Go","url":"https://go.dev","description":"Release notes","page_age":"2026-09-01","extra_snippets":["New features"]},{"title":"Docs","url":"https://go.dev/doc"},{"title":"Extra","url":"https://go.dev/extra"}]},"diagnostic":"test-brave-secret"}`)
	})
	called := false
	search.OnUsage = func(u *openrouter.Usage) {
		called = true
		if u != nil {
			t.Error("Brave cost incorrectly known")
		}
	}
	executor := executor(t, "inspect")
	executor.WebSearch = search
	prepared, err := executor.Prepare("web_search", json.RawMessage(`{"query":"Go releases","num_results":2,"include_domains":["go.dev","golang.org"],"exclude_domains":["example.org"]}`))
	if err != nil || prepared.NeedsApproval || !strings.Contains(prepared.Review, "Brave: Go releases") {
		t.Fatal(prepared, err)
	}
	out, err := executor.Run(context.Background(), prepared)
	var results struct {
		Results []struct {
			Title, URL, PublishedDate string
			Highlights                []string
		}
	}
	if err != nil || json.Unmarshal([]byte(out), &results) != nil || len(results.Results) != 2 || results.Results[0].PublishedDate != "2026-09-01" || len(results.Results[0].Highlights) != 2 || !called || strings.Contains(out, "diagnostic") {
		t.Fatal(out, err)
	}
	executor.WebSearch = nil
	if _, err := executor.Run(context.Background(), prepared); err == nil {
		t.Fatal("stale prepared search bypassed disabled provider")
	}
}

func TestBraveFailuresAndEmptyResults(t *testing.T) {
	for _, code := range []int{400, 401, 402, 403, 429, 500} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			search := testBrave(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(code)
				fmt.Fprint(w, "test-brave-secret diagnostic")
			})
			called := false
			search.OnUsage = func(u *openrouter.Usage) {
				called = true
				if u != nil {
					t.Error("failed request claimed known cost")
				}
			}
			out, err := search.tool().Run(context.Background(), json.RawMessage(`{"query":"x"}`))
			if err != nil || !strings.Contains(out, fmt.Sprint(code)) || strings.Contains(out, "test-brave-secret") || !called {
				t.Fatal(out, err)
			}
		})
	}
	for _, body := range []string{`not json`, `{}`, `null`, strings.Repeat("x", (1<<20)+1)} {
		search := testBrave(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, body) })
		if _, err := search.search(context.Background(), json.RawMessage(`{"query":"x"}`)); err == nil {
			t.Fatal("accepted malformed or oversized response")
		}
	}
	for _, body := range []string{`{"type":"search"}`, `{"web":{"results":[]}}`} {
		search := testBrave(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, body) })
		out, err := search.search(context.Background(), json.RawMessage(`{"query":"x"}`))
		if err != nil || out != `{"results":[]}` {
			t.Fatal(out, err)
		}
	}
}

func TestBraveValidationRedactionCancellationAndRedirect(t *testing.T) {
	if NewBrave(" ") != nil {
		t.Fatal("blank key enabled Brave")
	}
	requests := 0
	search := testBrave(t, func(w http.ResponseWriter, r *http.Request) {
		requests++
		if strings.Contains(r.URL.RawQuery, "test-brave-secret") {
			t.Error("credential in query")
		}
		if r.URL.Query().Get("count") != "5" {
			t.Error("incorrect default result count")
		}
		fmt.Fprint(w, `{"web":{"results":[{"title":"test-brave-secret","url":"https://example.com"}]}}`)
	})
	for _, args := range []string{`{}`, `{"query":"x","num_results":11}`, `{"query":"x","include_domains":["example.com OR site:other.com"]}`, `{"query":"x","exclude_domains":["https://example.com"]}`, `{"query":"x","include_domains":["-example.com"]}`} {
		if _, err := search.search(context.Background(), json.RawMessage(args)); err == nil {
			t.Fatal("accepted invalid arguments", args)
		}
	}
	for _, query := range []string{strings.Repeat("é", 601), strings.Repeat("a ", 76)} {
		args, _ := json.Marshal(webSearchArgs{Query: query})
		if _, err := search.search(context.Background(), args); err == nil {
			t.Fatal("accepted oversized query")
		}
	}
	if requests != 0 {
		t.Fatal("validation sent requests")
	}
	out, err := search.search(context.Background(), json.RawMessage(`{"query":"test-brave-secret"}`))
	if err != nil || strings.Contains(out, "test-brave-secret") {
		t.Fatal(out, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := search.search(ctx, json.RawMessage(`{"query":"x"}`)); err != context.Canceled {
		t.Fatal(err)
	}
	reached := false
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { reached = true }))
	defer target.Close()
	redirect := testBrave(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	})
	if _, err := redirect.search(context.Background(), json.RawMessage(`{"query":"x"}`)); err == nil || reached {
		t.Fatal("credential-bearing redirect followed")
	}
}
