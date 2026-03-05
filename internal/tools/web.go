package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type WebFetch struct{
	Base
	HTTP *http.Client
}

func (t *WebFetch) Name() string { return "web_fetch" }
func (t *WebFetch) Description() string { return "Fetch a URL (GET) and return text (truncated)." }
func (t *WebFetch) Parameters() map[string]any {
	return map[string]any{"type":"object","properties":map[string]any{
		"url": map[string]any{"type":"string"},
		"maxBytes": map[string]any{"type":"integer"},
	},"required":[]string{"url"}}
}
func (t *WebFetch) Schema() map[string]any { return t.SchemaFor(t.Name(), t.Description(), t.Parameters()) }
func (t *WebFetch) Execute(ctx context.Context, params map[string]any) (string, error) {
	u := fmt.Sprint(params["url"])
	if !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
		return "", fmt.Errorf("invalid url")
	}
	max := 200000
	if v, ok := params["maxBytes"].(float64); ok && int(v) > 0 { max = int(v) }
	if t.HTTP == nil { t.HTTP = &http.Client{Timeout: 20*time.Second} }
	r, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil { return "", err }
	resp, err := t.HTTP.Do(r)
	if err != nil { return "", err }
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, int64(max)))
	return fmt.Sprintf("status: %s\n\n%s", resp.Status, string(body)), nil
}

type WebSearch struct{
	Base
	APIKey string
	HTTP *http.Client
}

func (t *WebSearch) Name() string { return "web_search" }
func (t *WebSearch) Description() string {
	return "Search the web (Brave Search API) and return top results."
}
func (t *WebSearch) Parameters() map[string]any {
	return map[string]any{"type":"object","properties":map[string]any{
		"query": map[string]any{"type":"string"},
		"count": map[string]any{"type":"integer","description":"max results (default 5)"},
	},"required":[]string{"query"}}
}
func (t *WebSearch) Schema() map[string]any { return t.SchemaFor(t.Name(), t.Description(), t.Parameters()) }

func (t *WebSearch) Execute(ctx context.Context, params map[string]any) (string, error) {
	if strings.TrimSpace(t.APIKey) == "" {
		return "", fmt.Errorf("Brave API key not configured (set BRAVE_API_KEY)")
	}
	q := fmt.Sprint(params["query"])
	count := 5
	if v, ok := params["count"].(float64); ok && int(v) > 0 { count = int(v) }
	if count > 10 { count = 10 }
	if t.HTTP == nil { t.HTTP = &http.Client{Timeout: 20*time.Second} }

	endpoint := "https://api.search.brave.com/res/v1/web/search?q=" + url.QueryEscape(q) + "&count=" + fmt.Sprint(count)
	r, err := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
	if err != nil { return "", err }
	r.Header.Set("Accept", "application/json")
	r.Header.Set("X-Subscription-Token", t.APIKey)

	resp, err := t.HTTP.Do(r)
	if err != nil { return "", err }
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("search error %s: %s", resp.Status, string(body))
	}

	// Reduce response to stable subset
	var raw map[string]any
	_ = json.Unmarshal(body, &raw)
	out := map[string]any{"query": q, "results": []any{}}
	web, _ := raw["web"].(map[string]any)
	results, _ := web["results"].([]any)
	for _, it := range results {
		m, _ := it.(map[string]any)
		out["results"] = append(out["results"].([]any), map[string]any{
			"title": m["title"],
			"url": m["url"],
			"description": m["description"],
		})
	}
	b, _ := json.MarshalIndent(out, "", "  ")
	return string(b), nil
}

// Optional: simple text extract from HTML (very rough)
func StripHTML(s string) string {
	var b bytes.Buffer
	in := false
	for _, r := range s {
		if r == '<' { in = true; continue }
		if r == '>' { in = false; continue }
		if !in { b.WriteRune(r) }
	}
	return b.String()
}
