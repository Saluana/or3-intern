package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"net/url"
	"or3-intern/internal/artifacts"
	"or3-intern/internal/security"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type WebFetch struct {
	Base
	HTTP               *http.Client
	Timeout            time.Duration
	DefaultMaxBytes    int
	HTMLSourceMaxBytes int
	HostPolicy         security.HostPolicy
	Renderer           WebRenderer
	Store              *artifacts.Store
	Converter          *HTMLConverter
}

func (t *WebFetch) Capability() CapabilityLevel { return CapabilityGuarded }

const (
	defaultWebTimeout            = 20 * time.Second
	defaultWebFetchMaxBytes      = 12000
	defaultWebFetchMaxRedirects  = 10
	defaultWebSearchMaxCount     = 10
	defaultWebSearchReadMaxBytes = 1 << 20
	defaultWebRenderWaitMS       = 0
	maxWebRenderWaitMS           = 15000
)

type WebRenderOptions struct {
	MaxBytes  int
	WaitUntil string
	WaitMS    int
	Selector  string
}

type WebRenderer interface {
	Render(ctx context.Context, target string, opts WebRenderOptions) (string, error)
}

func (t *WebFetch) Name() string { return "web_fetch" }
func (t *WebFetch) Description() string {
	return "Fetch a known HTTP(S) URL and return bounded page/file text. This is guarded network access. Use web_search first if you do not already have the URL. HTML pages are converted to Markdown artifacts by default to avoid blowing up context; set raw=true only when you explicitly need literal response bytes. Set render=true only for JavaScript-rendered pages when plain fetching misses the content."
}
func (t *WebFetch) Parameters() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{
		"url":       map[string]any{"type": "string", "description": "Full http:// or https:// URL to fetch. Do not pass search terms here; use web_search for discovery."},
		"maxBytes":  map[string]any{"type": "integer", "description": "Maximum preview bytes returned directly. Omit for default 12000; larger HTML output may be saved as an artifact instead."},
		"raw":       map[string]any{"type": "boolean", "description": "When true, bypass Markdown conversion and return literal response bytes, including raw HTML. Leave false unless raw source is specifically needed."},
		"render":    map[string]any{"type": "boolean", "description": "When true, use a browser to execute JavaScript before extracting text. Slower and more fragile; use only after normal fetch does not expose the needed content."},
		"waitUntil": map[string]any{"type": "string", "enum": []string{"domcontentloaded", "load", "networkidle"}, "description": "For render=true only: browser navigation wait strategy. Omit for networkidle."},
		"waitMs":    map[string]any{"type": "integer", "description": "For render=true only: extra milliseconds to wait after navigation, capped at 15000."},
		"selector":  map[string]any{"type": "string", "description": "For render=true only: optional CSS selector to wait for before extracting text, such as main or article."},
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
	if err := validateFetchURL(parsed); err != nil {
		return "", err
	}
	max := t.DefaultMaxBytes
	if max <= 0 {
		max = defaultWebFetchMaxBytes
	}
	if v, ok := params["maxBytes"].(float64); ok && int(v) > 0 {
		max = int(v)
	}
	raw, _ := params["raw"].(bool)
	render, _ := params["render"].(bool)
	if render {
		if err := validateURLAgainstPolicies(ctx, parsed, t.HostPolicy, profile); err != nil {
			return "", err
		}
		renderer := t.Renderer
		if renderer == nil {
			renderer = PlaywrightRenderer{Timeout: t.effectiveTimeout()}
		}
		text, err := renderer.Render(ctx, parsed.String(), WebRenderOptions{
			MaxBytes:  max,
			WaitUntil: webFetchWaitUntil(params),
			WaitMS:    webFetchWaitMS(params),
			Selector:  strings.TrimSpace(fmt.Sprint(params["selector"])),
		})
		if err != nil {
			return "", err
		}
		preview, truncated := PreviewString(text, max)
		return EncodeToolResult(ToolResult{
			Kind:    "web_fetch",
			OK:      true,
			Summary: fmt.Sprintf("Rendered %s with JavaScript enabled", parsed.String()),
			Preview: preview,
			Stats: map[string]any{
				"url":        parsed.String(),
				"mode":       "render",
				"max_bytes":  max,
				"truncated":  truncated,
				"wait_until": webFetchWaitUntil(params),
				"wait_ms":    webFetchWaitMS(params),
			},
		}), nil
	}
	reqCtx, err := prepareWebFetchRequestContext(ctx, parsed, t.HostPolicy, profile)
	if err != nil {
		return "", err
	}
	var client *http.Client
	if t.HTTP == nil {
		client = &http.Client{Timeout: t.effectiveTimeout()}
	} else {
		copyClient := *t.HTTP
		client = &copyClient
	}
	originalCheckRedirect := client.CheckRedirect
	client = security.WrapHTTPClient(client, t.HostPolicy)
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= defaultWebFetchMaxRedirects {
			return fmt.Errorf("stopped after %d redirects", defaultWebFetchMaxRedirects)
		}
		if originalCheckRedirect != nil {
			if err := originalCheckRedirect(req, via); err != nil {
				return err
			}
		}
		if err := validateFetchURL(req.URL); err != nil {
			return err
		}
		redirectCtx, err := prepareWebFetchRequestContext(req.Context(), req.URL, t.HostPolicy, profile)
		if err != nil {
			return err
		}
		*req = *req.WithContext(redirectCtx)
		return nil
	}
	r, err := http.NewRequestWithContext(reqCtx, "GET", parsed.String(), nil)
	if err != nil {
		return "", err
	}
	resp, err := client.Do(r)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	contentType := resp.Header.Get("Content-Type")
	readLimit := int64(max) + 1
	info := StreamInfo{MIMEType: contentType, Extension: strings.ToLower(filepath.Ext(parsed.Path)), Filename: filepath.Base(parsed.Path), Charset: htmlCharsetFromContentType(contentType), URL: parsed.String()}
	if !raw && t.shouldAutoConvertHTML(info) {
		readLimit = int64(t.htmlSourceLimit()) + 1
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, readLimit))
	sourceTruncated := int64(len(body)) >= readLimit
	if sourceTruncated {
		body = body[:readLimit-1]
	}
	if !raw && t.shouldAutoConvertHTML(info) {
		result, err := buildMarkdownFetchResult(ctx, t.Store, t.Converter, parsed, resp.Status, resp.StatusCode, contentType, body, sourceTruncated, max, "web_fetch")
		if err == nil {
			return EncodeToolResult(result), nil
		}
	}
	previewSource := string(body)
	mode := "http"
	if isHTMLContent(info) && !raw {
		previewSource = CleanHTMLForLLM(previewSource)
		mode = "html_text"
	} else if raw {
		mode = "raw"
	}
	preview, previewTruncated := PreviewString(previewSource, max)
	return EncodeToolResult(ToolResult{
		Kind:    "web_fetch",
		OK:      resp.StatusCode < 400,
		Summary: fmt.Sprintf("Fetched %s with HTTP status %s", parsed.String(), resp.Status),
		Preview: preview,
		Stats: map[string]any{
			"url":          parsed.String(),
			"mode":         mode,
			"status":       resp.Status,
			"status_code":  resp.StatusCode,
			"content_type": contentType,
			"max_bytes":    max,
			"truncated":    previewTruncated || sourceTruncated,
		},
	}), nil
}

func (t *WebFetch) shouldAutoConvertHTML(info StreamInfo) bool {
	if t == nil || t.Store == nil {
		return false
	}
	return isHTMLContent(info)
}

func isHTMLContent(info StreamInfo) bool {
	return NewHTMLConverter().Accepts(info)
}

func (t *WebFetch) htmlSourceLimit() int {
	limit := t.HTMLSourceMaxBytes
	if limit <= 0 {
		limit = defaultWebMarkdownMaxSourceBytes
	}
	if limit > maxWebMarkdownSourceBytes {
		limit = maxWebMarkdownSourceBytes
	}
	return limit
}

func (t *WebFetch) effectiveTimeout() time.Duration {
	if t.Timeout > 0 {
		return t.Timeout
	}
	return defaultWebTimeout
}

func webFetchWaitUntil(params map[string]any) string {
	waitUntil := strings.ToLower(strings.TrimSpace(fmt.Sprint(params["waitUntil"])))
	switch waitUntil {
	case "domcontentloaded", "load", "networkidle":
		return waitUntil
	default:
		return "networkidle"
	}
}

func webFetchWaitMS(params map[string]any) int {
	waitMS := defaultWebRenderWaitMS
	if v, ok := params["waitMs"].(float64); ok && int(v) > 0 {
		waitMS = int(v)
	}
	if waitMS > maxWebRenderWaitMS {
		waitMS = maxWebRenderWaitMS
	}
	return waitMS
}

type PlaywrightRenderer struct {
	NodePath string
	Timeout  time.Duration
}

func (r PlaywrightRenderer) Render(ctx context.Context, target string, opts WebRenderOptions) (string, error) {
	nodePath := strings.TrimSpace(r.NodePath)
	if nodePath == "" {
		var err error
		nodePath, err = exec.LookPath("node")
		if err != nil {
			return "", fmt.Errorf("render=true requires Node.js and the playwright package: %w", err)
		}
	}
	timeout := r.Timeout
	if timeout <= 0 {
		timeout = defaultWebTimeout
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	dir, err := os.MkdirTemp("", "or3-web-render-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(dir)
	scriptPath := filepath.Join(dir, "render.js")
	if err := os.WriteFile(scriptPath, []byte(playwrightRenderScript), 0o600); err != nil {
		return "", err
	}
	args := []string{scriptPath, target, opts.WaitUntil, fmt.Sprint(opts.WaitMS), strings.TrimSpace(opts.Selector)}
	cmd := exec.CommandContext(runCtx, nodePath, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		errText := strings.TrimSpace(stderr.String())
		if errText == "" {
			errText = err.Error()
		}
		if strings.Contains(errText, "Cannot find module 'playwright'") {
			return "", fmt.Errorf("render=true requires the playwright npm package and installed browser binaries")
		}
		return "", fmt.Errorf("render failed: %s", errText)
	}
	return stdout.String(), nil
}

const playwrightRenderScript = `
const target = process.argv[2];
const waitUntil = process.argv[3] || 'networkidle';
const waitMs = Math.max(0, Number(process.argv[4] || 0));
const selector = process.argv[5] || '';

(async () => {
  const { chromium } = require('playwright');
  const browser = await chromium.launch({ headless: true });
  try {
    const page = await browser.newPage();
    await page.goto(target, { waitUntil, timeout: 20000 });
    if (selector) {
      await page.waitForSelector(selector, { timeout: 10000 });
    }
    if (waitMs > 0) {
      await page.waitForTimeout(waitMs);
    }
    const text = await page.evaluate(() => document.body ? document.body.innerText : document.documentElement.innerText);
    process.stdout.write(text || '');
  } finally {
    await browser.close();
  }
})().catch((err) => {
  process.stderr.write(err && err.stack ? err.stack : String(err));
  process.exit(1);
});
`

func validateFetchURL(target *url.URL) error {
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
	return "Search the public web and return a small list of candidate result URLs with titles/snippets. This is for URL discovery, not for reading page contents. After choosing a result, call web_fetch with that exact URL if content is needed. Keep queries specific and avoid using this when the answer is already available in the conversation or local files."
}
func (t *WebSearch) Parameters() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{
		"query": map[string]any{"type": "string", "description": "Search query text. Include the product/project/person name and the fact you need; for exact phrases or errors, include the exact words."},
		"count": map[string]any{"type": "integer", "description": "Maximum results to return. Omit for default 5; hard cap is 10. Use fewer results when one or two likely URLs are enough."},
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

// StripHTML removes tag text without parsing; it is only a fallback for malformed input.
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

func prepareWebFetchRequestContext(ctx context.Context, target *url.URL, policy security.HostPolicy, profile ActiveProfile) (context.Context, error) {
	policies := []security.HostPolicy{{Enabled: true}}
	if policy.EnabledPolicy() || policy.AllowLoopback || policy.AllowPrivate {
		policies = append(policies, policy)
	}
	if strings.TrimSpace(profile.Name) != "" {
		policies = append(policies, security.HostPolicy{Enabled: true, DefaultDeny: true, AllowedHosts: profile.AllowedHosts})
	}
	return security.PrepareURLRequestContext(ctx, target, policies...)
}
