package openrouter

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestTransientRejectionRetryAndWireHistory(t *testing.T) {
	var requests atomic.Int32
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requests.Add(1) == 1 {
			w.WriteHeader(503)
			return
		}
		var body ChatRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body.Messages[0].Provider != "" || body.Messages[0].Model != "" {
			t.Error("provenance leaked onto wire")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"))
	}))
	defer s.Close()
	c := New("test-key")
	c.SetBaseURL(s.URL)
	ch, err := c.ChatStream(context.Background(), ChatRequest{Model: "x/y", Messages: []Message{{Role: "user", Content: "hi", Provider: "openrouter", Model: "x/y"}}})
	if err != nil {
		t.Fatal(err)
	}
	for ev := range ch {
		if ev.Err != nil {
			t.Fatal(ev.Err)
		}
	}
	if requests.Load() != 2 {
		t.Fatal("retry count", requests.Load())
	}
}
func TestRetryCancellation(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(429) }))
	defer s.Close()
	c := New("test")
	c.SetBaseURL(s.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	started := time.Now()
	if _, err := c.ChatStream(ctx, ChatRequest{}); err == nil {
		t.Fatal("retry ignored cancellation")
	}
	if time.Since(started) > time.Second {
		t.Fatal("slow cancellation")
	}
}
func TestCostPresenceAndMalformedCalls(t *testing.T) {
	for _, test := range []struct {
		raw   string
		known bool
	}{{`{"cost":0}`, true}, {`{}`, false}, {`{"cost":null}`, false}} {
		var u Usage
		if err := json.Unmarshal([]byte(test.raw), &u); err != nil {
			t.Fatal(err)
		}
		if u.CostKnown != test.known {
			t.Fatal(test.raw)
		}
	}
	call := ToolCall{ID: "id", Type: "function"}
	call.Function.Name = "bash"
	call.Function.Arguments = `{"command":"true"}`
	if err := validateToolCalls([]ToolCall{call, call}, "tool_calls"); err == nil {
		t.Fatal("duplicate accepted")
	}
	ch := make(chan StreamEvent, 64)
	go readStream(context.Background(), ioReadCloser{strings.NewReader("data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"tool_calls\"}]}\n")}, time.Now(), ch)
	for ev := range ch {
		if ev.Done && (ev.Err == nil || len(ev.ToolCalls) > 0) {
			t.Fatal("incomplete calls executed")
		}
	}
}

type ioReadCloser struct{ *strings.Reader }

func (ioReadCloser) Close() error { return nil }
