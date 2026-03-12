package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"or3-intern/internal/security"
	"strings"
	"time"
)

type WebFetch struct {
	Base
	HTTP            *http.Client
	Timeout         time.Duration
	DefaultMaxBytes int
	HostPolicy      security.HostPolicy
}

func (t *WebFetch) Capability() CapabilityLevel { return CapabilityGuarded }

const (
	defaultWebTimeout            = 20 * time.Second
	defaultWebFetchMaxBytes      = 200000
	defaultWebFetchMaxRedirects  = 10
	defaultWebSearchMaxCount     = 10
	defaultWebSearchReadMaxBytes = 1 << 20
)

func (t *WebFetch) Name() string        { return "web_fetch" }
func (t *WebFetch) Description() string { return "Fetch a URL (GET) and return text (truncated)." }
func (t *WebFetch) Parameters() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{
		"url":      map[string]any{"type": "string"},
		"maxBytes": map[string]any{"type": "integer"},
	}, "required": []string{"url"}}
}
func (t *WebFetch) Schema() map[string]any {
	return t.SchemaFor(t.Name(), t.Description(), t.Parameters())
}
func (t *WebFetch) Execute(ctx context.Context, params map[string]any) (string, error) {
	profile := ActiveProfileFromContext(ctx)
	u := fmt.Sprint(params["url"])
	if !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
		return "", fmt.Errorf("invalid url")
	}
	parsed, err := url.Parse(u)
	if err != nil {
		return "", err
	}
	if err := validateFetchURL(ctx, parsed); err != nil {
		return "", err
	}
	if err := validateURLAgainstPolicies(ctx, parsed, t.HostPolicy, profile); err != nil {
		return "", err
	}
	max := t.DefaultMaxBytes
	if max <= 0 {
		max = defaultWebFetchMaxBytes
	}
	if v, ok := params["maxBytes"].(float64); ok && int(v) > 0 {
		max = int(v)
	}
	client := t.HTTP
	if t.HTTP == nil {
		to := t.Timeout
		if to <= 0 {
			to = defaultWebTimeout
		}
		client = &http.Client{Timeout: to}
	} else {
		copyClient := *t.HTTP
		client = &copyClient
	}
	client = security.WrapHTTPClient(client, t.HostPolicy)
	prevCheckRedirect := client.CheckRedirect
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= defaultWebFetchMaxRedirects {
			return fmt.Errorf("stopped after %d redirects", defaultWebFetchMaxRedirects)
		}
		if err := validateURLAgainstPolicies(req.Context(), req.URL, t.HostPolicy, profile); err != nil {
			return err
		}
		if prevCheckRedirect != nil {
			if err := prevCheckRedirect(req, via); err != nil {
				return err
			}
		}
		return validateFetchURL(req.Context(), req.URL)
	}
	r, err := http.NewRequestWithContext(ctx, "GET", parsed.String(), nil)
	if err != nil {
		return "", err
	}
	resp, err := client.Do(r)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, int64(max)))
	return fmt.Sprintf("status: %s\n\n%s", resp.Status, string(body)), nil
}

func validateFetchURL(ctx context.Context, target *url.URL) error {
	if target == nil {
		return fmt.Errorf("invalid url")
	}
	hostname := strings.TrimSpace(strings.ToLower(target.Hostname()))
	if hostname == "" {
		return fmt.Errorf("missing host")
	}
	if isBlockedFetchHostname(hostname) {
		return fmt.Errorf("blocked fetch target")
	}
	if ip, err := netip.ParseAddr(hostname); err == nil {
		if isBlockedFetchAddr(ip.Unmap()) {
			return fmt.Errorf("blocked fetch target")
		}
		return nil
	}
	addrs, err := net.DefaultResolver.LookupIPAddr(ctx, hostname)
	if err != nil {
		return err
	}
	if len(addrs) == 0 {
		return fmt.Errorf("host did not resolve")
	}
	for _, addr := range addrs {
		if ip, ok := netip.AddrFromSlice(addr.IP); ok && isBlockedFetchAddr(ip.Unmap()) {
			return fmt.Errorf("blocked fetch target")
		}
	}
	return nil
}

func isBlockedFetchHostname(hostname string) bool {
	switch hostname {
	case "localhost", "ip6-localhost", "metadata.google.internal":
		return true
	default:
		return false
	}
}

func isBlockedFetchAddr(addr netip.Addr) bool {
	if !addr.IsValid() {
		return true
	}
	if addr.IsLoopback() || addr.IsPrivate() || addr.IsLinkLocalUnicast() || addr.IsLinkLocalMulticast() || addr.IsMulticast() || addr.IsUnspecified() {
		return true
	}
	return addr.String() == "169.254.169.254"
}

type WebSearch struct {
	Base
	APIKey       string
	HTTP         *http.Client
	Timeout      time.Duration
	ReadMaxBytes int
	HostPolicy   security.HostPolicy
}

func (t *WebSearch) Capability() CapabilityLevel { return CapabilitySafe }

func (t *WebSearch) Name() string { return "web_search" }
func (t *WebSearch) Description() string {
	return "Search the web (Brave Search API) and return top results."
}
func (t *WebSearch) Parameters() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{
		"query": map[string]any{"type": "string"},
		"count": map[string]any{"type": "integer", "description": "max results (default 5)"},
	}, "required": []string{"query"}}
}
func (t *WebSearch) Schema() map[string]any {
	return t.SchemaFor(t.Name(), t.Description(), t.Parameters())
}

func (t *WebSearch) Execute(ctx context.Context, params map[string]any) (string, error) {
	profile := ActiveProfileFromContext(ctx)
	if strings.TrimSpace(t.APIKey) == "" {
		return "", fmt.Errorf("Brave API key not configured (set BRAVE_API_KEY)")
	}
	q := fmt.Sprint(params["query"])
	count := 5
	if v, ok := params["count"].(float64); ok && int(v) > 0 {
		count = int(v)
	}
	if count > defaultWebSearchMaxCount {
		count = defaultWebSearchMaxCount
	}
	if t.HTTP == nil {
		to := t.Timeout
		if to <= 0 {
			to = defaultWebTimeout
		}
		t.HTTP = &http.Client{Timeout: to}
	}

	endpoint := "https://api.search.brave.com/res/v1/web/search?q=" + url.QueryEscape(q) + "&count=" + fmt.Sprint(count)
	parsedEndpoint, err := url.Parse(endpoint)
	if err != nil {
		return "", err
	}
	if err := validateURLAgainstPolicies(ctx, parsedEndpoint, t.HostPolicy, profile); err != nil {
		return "", err
	}
	r, err := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
	if err != nil {
		return "", err
	}
	r.Header.Set("Accept", "application/json")
	r.Header.Set("X-Subscription-Token", t.APIKey)

	resp, err := t.HTTP.Do(r)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	maxRead := t.ReadMaxBytes
	if maxRead <= 0 {
		maxRead = defaultWebSearchReadMaxBytes
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, int64(maxRead)))
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
			"title":       m["title"],
			"url":         m["url"],
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
		if r == '<' {
			in = true
			continue
		}
		if r == '>' {
			in = false
			continue
		}
		if !in {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func validateURLAgainstPolicies(ctx context.Context, target *url.URL, policy security.HostPolicy, profile ActiveProfile) error {
	if policy.EnabledPolicy() {
		if err := policy.ValidateURL(ctx, target); err != nil {
			return err
		}
	}
	if strings.TrimSpace(profile.Name) == "" {
		return nil
	}
	return (security.HostPolicy{Enabled: true, DefaultDeny: true, AllowedHosts: profile.AllowedHosts}).ValidateURL(ctx, target)
}
