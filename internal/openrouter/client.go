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
	base       string
	chatPath   string
	direct     bool
	goProvider bool
	sessionID  string
}

// New returns a Client with a bounded stream lifetime and no redirects.
// Rejecting redirects prevents credentials being forwarded to another endpoint.
func New(apiKey string) *Client {
	return &Client{
		key:      apiKey,
		http:     &http.Client{Timeout: 10 * time.Minute, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
		base:     "https://openrouter.ai",
		chatPath: "/api/v1/chat/completions",
	}
}

// NewDeepSeek uses the direct DeepSeek chat-completions API.
func NewDeepSeek(apiKey string) *Client {
	c := New(apiKey)
	c.base, c.chatPath, c.direct = "https://api.deepseek.com", "/chat/completions", true
	return c
}

// SetKey swaps the API key after a settings change.
func (c *Client) SetKey(key string) { c.mu.Lock(); defer c.mu.Unlock(); c.key = key }

func (c *Client) apiKey() string { c.mu.RLock(); defer c.mu.RUnlock(); return c.key }

// SetBaseURL overrides the API root (tests only).
func (c *Client) SetBaseURL(base string) { c.base = base }

// ContextLength returns the model's context window in tokens.
func (c *Client) ContextLength(ctx context.Context, model string) (int, error) {
	if c.goProvider {
		return c.goContextLength(model)
	}
	if c.direct {
		// Published limits, rather than an OpenRouter-only metadata endpoint.
		// https://api-docs.deepseek.com/quick_start/pricing/ (2026-09-30)
		// Use 1,000,000 tokens conservatively for the documented 1M window.
		switch deepSeekModelID(model) {
		case "deepseek-flash", "deepseek-v4-pro", "deepseek-v4-flash", "deepseek-v4-flash-vision-exp":
			return 1_000_000, nil
		default:
			return 0, fmt.Errorf("context window is not configured for DeepSeek model %q", model)
		}
	}
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
	resp, err := c.do(req)
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
	if c.goProvider {
		req.Header.Set("User-Agent", "jev-harness/1.0")
		c.mu.RLock()
		id := c.sessionID
		c.mu.RUnlock()
		req.Header.Set("x-opencode-session", id)
	}
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
	if strings.TrimSpace(c.apiKey()) == "" {
		return nil, fmt.Errorf("API key is not set for the selected provider")
	}
	if c.goProvider {
		return c.goChatStream(ctx, req)
	}
	req.Messages = append([]Message(nil), req.Messages...)
	for i := range req.Messages {
		if req.Messages[i].NativeProvider != "" {
			req.Messages[i].ReasoningContent = ""
		}
		req.Messages[i].NativeProvider, req.Messages[i].NativeItems = "", nil
		req.Messages[i].Provider, req.Messages[i].Model = "", ""
	}
	req.Stream = true
	req.StreamOptions = &StreamOptions{IncludeUsage: true}
	if c.direct {
		req.Model = deepSeekModelID(req.Model)
		req.StreamOptions = &StreamOptions{IncludeUsage: true}
		req.Thinking = &ThinkingOptions{Type: "disabled"}
		// Reasoning from other backends is not part of a non-thinking request.
		req.Messages = append([]Message(nil), req.Messages...)
		for i := range req.Messages {
			req.Messages[i].ReasoningContent = ""
		}
	}
	r, err := c.newReq(ctx, c.base+c.chatPath, req)
	if err != nil {
		return nil, err
	}
	started := time.Now()
	resp, err := c.do(r)
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

// Accept an OpenRouter-style prefix on direct DeepSeek roles while keeping
// OpenRouter requests and their model IDs unchanged.
func deepSeekModelID(model string) string {
	return strings.TrimPrefix(strings.TrimSpace(model), "deepseek/")
}

// Decide calls the Decisions API.
func (c *Client) Decide(ctx context.Context, req DecisionsRequest) (*DecisionsResponse, error) {
	if strings.TrimSpace(c.apiKey()) == "" {
		return nil, fmt.Errorf("OpenRouter routing API key is not set")
	}
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	r, err := c.newReq(ctx, c.base+"/api/alpha/decisions", req)
	if err != nil {
		return nil, err
	}
	resp, err := c.do(r)
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

// ModelKey keeps context estimates separate across providers and native aliases.
func (c *Client) ModelKey(model string) string {
	if c.goProvider {
		return "opencode-go:" + goModelID(model)
	}
	if c.direct {
		return "deepseek:" + deepSeekModelID(model)
	}
	return "openrouter:" + model
}
