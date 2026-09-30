package openrouter

import (
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Retry only explicit transient HTTP rejections, before any stream is consumed.
// Transport failures and partial streams have uncertain billing/completion and are not replayed.
func (c *Client) do(req *http.Request) (*http.Response, error) {
	for attempt := 0; ; attempt++ {
		resp, err := c.http.Do(req)
		if err != nil {
			return nil, err
		}
		if attempt >= 2 || (resp.StatusCode != 429 && resp.StatusCode != 502 && resp.StatusCode != 503 && resp.StatusCode != 504) {
			return resp, nil
		}
		resp.Body.Close()
		delay := time.Duration(250*(1<<attempt)) * time.Millisecond
		if n, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil && n > 0 {
			delay = min(time.Duration(n)*time.Second, 10*time.Second)
		}
		timer := time.NewTimer(delay)
		select {
		case <-req.Context().Done():
			timer.Stop()
			return nil, req.Context().Err()
		case <-timer.C:
		}
		next := req.Clone(req.Context())
		if req.GetBody != nil {
			next.Body, err = req.GetBody()
			if err != nil {
				return nil, err
			}
		}
		req = next
	}
}
func ContextOverflow(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "context_length_exceeded") || strings.Contains(s, "maximum context length") || strings.Contains(s, "context window exceeded")
}
