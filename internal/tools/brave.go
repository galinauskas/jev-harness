package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"jevharness/internal/redact"
)

// NewBrave uses Brave's Web Search API; credentials never enter tool arguments.
func NewBrave(key string) *WebSearch {
	key = strings.TrimSpace(key)
	if key == "" {
		return nil
	}
	return &WebSearch{provider: "Brave", key: key, endpoint: "https://api.search.brave.com/res/v1/web/search", client: &http.Client{
		Timeout:       30 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("Brave redirects are disabled") },
	}}
}

// Domain filters use Brave's site operators. Accept only bare host names so
// filters cannot inject additional operators into the query.
func braveDomain(domain string) (string, error) {
	domain = strings.TrimSpace(domain)
	if len(domain) == 0 || len(domain) > 253 {
		return "", errors.New("domain filters must contain bare host names")
	}
	for _, label := range strings.Split(domain, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return "", errors.New("domain filters must contain bare host names")
		}
		for _, c := range label {
			if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-') {
				return "", errors.New("domain filters must contain bare host names")
			}
		}
	}
	return domain, nil
}

func (e *WebSearch) searchBrave(ctx context.Context, args json.RawMessage) (string, error) {
	p, err := parseWebSearch(args)
	if err != nil {
		return "", err
	}
	query := redact.New(e.key).Text(p.Query)
	var includes []string
	for _, domain := range p.IncludeDomains {
		domain, err = braveDomain(domain)
		if err != nil {
			return "", err
		}
		includes = append(includes, "site:"+domain)
	}
	if len(includes) > 0 {
		query += " (" + strings.Join(includes, " OR ") + ")"
	}
	for _, domain := range p.ExcludeDomains {
		domain, err = braveDomain(domain)
		if err != nil {
			return "", err
		}
		query += " -site:" + domain
	}
	if utf8.RuneCountInString(query) > 600 || len(strings.Fields(query)) > 75 {
		return "", errors.New("Brave query including domain filters exceeds 600 characters or 75 words")
	}
	endpoint, err := url.Parse(e.endpoint)
	if err != nil {
		return "", errors.New("cannot create Brave request")
	}
	params := endpoint.Query()
	params.Set("q", query)
	params.Set("count", strconv.Itoa(p.NumResults))
	params.Set("extra_snippets", "true")
	params.Set("text_decorations", "false")
	params.Set("result_filter", "web")
	endpoint.RawQuery = params.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return "", errors.New("cannot create Brave request")
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Subscription-Token", e.key)
	resp, err := e.client.Do(req)
	// Brave does not return per-request dollar cost. Report unknown cost rather
	// than inventing a price, including on failures that may have been billed.
	if e.OnUsage != nil {
		defer e.OnUsage(nil)
	}
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", errors.New("Brave request failed (network error or timeout)")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		switch resp.StatusCode {
		case 401, 403:
			return "", fmt.Errorf("Brave authentication failed (HTTP %d); check BRAVE_API_KEY or the saved Brave key", resp.StatusCode)
		case 402:
			return "", errors.New("Brave credits exhausted (HTTP 402)")
		case 429:
			return "", errors.New("Brave rate limit reached (HTTP 429); try again later")
		default:
			return "", fmt.Errorf("Brave search failed (HTTP %d)", resp.StatusCode)
		}
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, (1<<20)+1))
	if err != nil {
		return "", errors.New("cannot read Brave response")
	}
	if len(data) > 1<<20 {
		return "", errors.New("Brave response exceeds 1 MiB")
	}
	var response struct {
		Type string `json:"type"`
		Web  *struct {
			Results []struct {
				Title         string   `json:"title"`
				URL           string   `json:"url"`
				Description   string   `json:"description"`
				PageAge       string   `json:"page_age"`
				ExtraSnippets []string `json:"extra_snippets"`
			} `json:"results"`
		} `json:"web"`
	}
	if err := json.Unmarshal(data, &response); err != nil || response.Web == nil && response.Type != "search" {
		return "", errors.New("invalid Brave response")
	}
	type result struct {
		Title         string   `json:"title"`
		URL           string   `json:"url"`
		PublishedDate string   `json:"publishedDate,omitempty"`
		Highlights    []string `json:"highlights,omitempty"`
	}
	results := make([]result, 0, p.NumResults)
	if response.Web != nil {
		for _, r := range response.Web.Results {
			highlights := append([]string(nil), r.ExtraSnippets...)
			if r.Description != "" {
				highlights = append([]string{r.Description}, highlights...)
			}
			results = append(results, result{r.Title, r.URL, r.PageAge, highlights})
			if len(results) == p.NumResults {
				break
			}
		}
	}
	out, err := json.Marshal(struct {
		Results []result `json:"results"`
	}{results})
	if err != nil {
		return "", errors.New("cannot encode Brave results")
	}
	return redact.New(e.key).Text(string(out)), nil
}
