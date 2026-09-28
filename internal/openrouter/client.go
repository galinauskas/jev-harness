// Package openrouter calls OpenRouter for chat, routing, and model metadata.
package openrouter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Client calls OpenRouter.
type Client struct {
	mu   sync.RWMutex
	key  string
	http *http.Client
	// base is "https://openrouter.ai" normally; tests override it.
	base string
}

// New returns a Client with a bounded stream lifetime and no redirects.
// Rejecting redirects prevents credentials being forwarded to another endpoint.
func New(apiKey string) *Client {
	return &Client{
		key:  apiKey,
		http: &http.Client{Timeout: 10 * time.Minute, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
		base: "https://openrouter.ai",
	}
}

// SetKey swaps the API key after a settings change.
func (c *Client) SetKey(key string) { c.mu.Lock(); defer c.mu.Unlock(); c.key = key }

func (c *Client) apiKey() string { c.mu.RLock(); defer c.mu.RUnlock(); return c.key }

// SetBaseURL overrides the API root (tests only).
func (c *Client) SetBaseURL(base string) { c.base = base }

// ContextLength returns the model's context window in tokens.
func (c *Client) ContextLength(ctx context.Context, model string) (int, error) {
	parts := strings.SplitN(model, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return 0, fmt.Errorf("invalid model id %q", model)
	}
	endpoint := c.base + "/api/v1/model/" + url.PathEscape(parts[0]) + "/" + url.PathEscape(parts[1])
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey())
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return 0, decodeError(resp)
	}
	var out struct {
		Data struct {
			ContextLength int `json:"context_length"`
		} `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&out); err != nil {
		return 0, err
	}
	if out.Data.ContextLength <= 0 {
		return 0, fmt.Errorf("model %q has no context length", model)
	}
	return out.Data.ContextLength, nil
}

func (c *Client) newReq(ctx context.Context, url string, body any) (*http.Request, error) {
	buf, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(buf))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey())
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Title", "jev-harness")
	return req, nil
}

func decodeError(resp *http.Response) error {
	var env struct {
		Err apiError `json:"error"`
	}
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	resp.Body.Close()
	if err := json.Unmarshal(data, &env); err == nil && env.Err.Message != "" {
		return fmt.Errorf("%v: %s", env.Err.Code, env.Err.Message)
	}
	return fmt.Errorf("http %d: %s", resp.StatusCode, truncate(string(data), 200))
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}

// ChatStream starts a streaming chat completion. It returns an error
// immediately on non-2xx; otherwise the returned channel carries deltas and
// one terminal Done event, then closes.
func (c *Client) ChatStream(ctx context.Context, req ChatRequest) (<-chan StreamEvent, error) {
	req.Stream = true
	r, err := c.newReq(ctx, c.base+"/api/v1/chat/completions", req)
	if err != nil {
		return nil, err
	}
	started := time.Now()
	resp, err := c.http.Do(r)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode/100 != 2 {
		return nil, decodeError(resp)
	}
	ch := make(chan StreamEvent, 64)
	go readStream(ctx, resp.Body, started, ch)
	return ch, nil
}

// Decide calls the Decisions API.
func (c *Client) Decide(ctx context.Context, req DecisionsRequest) (*DecisionsResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	r, err := c.newReq(ctx, c.base+"/api/alpha/decisions", req)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(r)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("decisions: %w", decodeError(resp))
	}
	var out DecisionsResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&out); err != nil {
		return nil, fmt.Errorf("decisions: decode: %w", err)
	}
	return &out, nil
}
