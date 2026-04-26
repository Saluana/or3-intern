package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"or3-intern/internal/artifacts"
	"or3-intern/internal/db"
	"or3-intern/internal/security"
)

const testPublicFetchURLBase = "http://203.0.113.10"

// ---- StripHTML ----

func TestStripHTML_NoTags(t *testing.T) {
	in := "plain text"
	out := StripHTML(in)
	if out != "plain text" {
		t.Errorf("expected 'plain text', got %q", out)
	}
}

func TestStripHTML_WithTags(t *testing.T) {
	in := "<p>Hello <b>World</b></p>"
	out := StripHTML(in)
	if out != "Hello World" {
		t.Errorf("expected 'Hello World', got %q", out)
	}
}

func TestStripHTML_Empty(t *testing.T) {
	out := StripHTML("")
	if out != "" {
		t.Errorf("expected empty string, got %q", out)
	}
}

func TestStripHTML_OnlyTags(t *testing.T) {
	out := StripHTML("<br><br/>")
	if out != "" {
		t.Errorf("expected empty, got %q", out)
	}
}

// ---- WebFetch ----

func TestWebFetch_InvalidURL(t *testing.T) {
	tool := &WebFetch{}
	_, err := tool.Execute(context.Background(), map[string]any{
		"url": "ftp://not-http",
	})
	if err == nil {
		t.Fatal("expected error for non-http URL")
	}
}

func TestWebFetch_EmptyURL(t *testing.T) {
	tool := &WebFetch{}
	_, err := tool.Execute(context.Background(), map[string]any{
		"url": "",
	})
	if err == nil {
		t.Fatal("expected error for empty URL")
	}
}

func TestWebFetch_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "hello from server")
	}))
	defer srv.Close()

	tool := &WebFetch{HTTP: newPinnedTestClient(t, srv.URL, nil)}
	out, err := tool.Execute(context.Background(), map[string]any{
		"url": testPublicFetchURLBase + "/test",
	})
	if err != nil {
		t.Fatalf("WebFetch: %v", err)
	}
	var result ToolResult
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("parse result: %v", err)
	}
	if !strings.Contains(result.Preview, "hello from server") {
		t.Errorf("expected server response in preview, got %q", out)
	}
	if fmt.Sprint(result.Stats["status"]) != "200 OK" {
		t.Errorf("expected status 200 in stats, got %#v", result.Stats)
	}
}

func TestWebFetch_MaxBytes(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, strings.Repeat("x", 1000))
	}))
	defer srv.Close()

	tool := &WebFetch{HTTP: newPinnedTestClient(t, srv.URL, nil)}
	out, err := tool.Execute(context.Background(), map[string]any{
		"url":      testPublicFetchURLBase + "/large",
		"maxBytes": float64(50),
	})
	if err != nil {
		t.Fatalf("WebFetch: %v", err)
	}
	var result ToolResult
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("parse result: %v", err)
	}
	if len(result.Preview) != 50 {
		t.Fatalf("expected 50-byte preview, got %d bytes in %q", len(result.Preview), out)
	}
}

func TestWebFetch_HTMLAutoConvertsToMarkdownArtifact(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	store := &artifacts.Store{Dir: t.TempDir(), DB: d}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<html><head><title>Example</title><script>alert("bad")</script></head><body><h1>Hello</h1><p>Readable body.</p></body></html>`)
	}))
	defer srv.Close()

	tool := &WebFetch{HTTP: newPinnedTestClient(t, srv.URL, nil), Timeout: 2 * time.Second, Store: store}
	out, err := tool.Execute(ContextWithSession(context.Background(), "sess"), map[string]any{
		"url":      testPublicFetchURLBase + "/page",
		"maxBytes": float64(80),
	})
	if err != nil {
		t.Fatalf("WebFetch: %v", err)
	}
	var result ToolResult
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("parse result: %v", err)
	}
	if result.Kind != "web_fetch" || result.ArtifactID == "" {
		t.Fatalf("expected web_fetch artifact result, got %#v", result)
	}
	if fmt.Sprint(result.Stats["mode"]) != "markdown" {
		t.Fatalf("expected markdown mode, got %#v", result.Stats)
	}
	if strings.Contains(result.Preview, "alert") || !strings.Contains(result.Preview, "Hello") {
		t.Fatalf("expected cleaned markdown preview, got %q", result.Preview)
	}
	read, err := (&ReadArtifact{Store: store, MaxReadBytes: 2000}).Execute(ContextWithSession(context.Background(), "sess"), map[string]any{"artifact_id": result.ArtifactID})
	if err != nil {
		t.Fatalf("read artifact: %v", err)
	}
	if !strings.Contains(read, "Readable body") || strings.Contains(read, "alert") {
		t.Fatalf("expected cleaned markdown artifact, got %q", read)
	}
}

func TestWebFetch_HTMLRawBypass(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<html><body><main><h1>Hello</h1><script>alert("bad")</script><p>Readable body.</p></main></body></html>`)
	}))
	defer srv.Close()

	tool := &WebFetch{HTTP: newPinnedTestClient(t, srv.URL, nil), Timeout: 2 * time.Second}
	out, err := tool.Execute(context.Background(), map[string]any{
		"url": testPublicFetchURLBase + "/raw",
		"raw": true,
	})
	if err != nil {
		t.Fatalf("WebFetch: %v", err)
	}
	var result ToolResult
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("parse result: %v", err)
	}
	if result.ArtifactID != "" {
		t.Fatalf("expected no artifact for raw bypass, got %#v", result)
	}
	if fmt.Sprint(result.Stats["mode"]) != "raw" {
		t.Fatalf("expected raw mode, got %#v", result.Stats)
	}
	if !strings.Contains(result.Preview, "<h1>Hello</h1>") || !strings.Contains(result.Preview, `alert("bad")`) {
		t.Fatalf("expected literal raw html preview, got %q", result.Preview)
	}
}

func TestWebFetch_HTMLErrorDoesNotCreateArtifact(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	store := &artifacts.Store{Dir: t.TempDir(), DB: d}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `<html><body><main><h1>Not Found</h1><p>Missing page.</p></main></body></html>`)
	}))
	defer srv.Close()

	tool := &WebFetch{HTTP: newPinnedTestClient(t, srv.URL, nil), Timeout: 2 * time.Second, Store: store}
	out, err := tool.Execute(ContextWithSession(context.Background(), "sess"), map[string]any{
		"url":      testPublicFetchURLBase + "/missing",
		"maxBytes": float64(80),
	})
	if err != nil {
		t.Fatalf("WebFetch: %v", err)
	}
	var result ToolResult
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("parse result: %v", err)
	}
	if result.ArtifactID != "" || result.OK {
		t.Fatalf("expected failed fetch without artifact, got %#v", result)
	}
	if fmt.Sprint(result.Stats["mode"]) != "html_text" || !strings.Contains(result.Preview, "Not Found Missing page.") {
		t.Fatalf("expected cleaned failure preview, got %#v / %q", result.Stats, result.Preview)
	}
}

type fakeWebRenderer struct {
	gotURL  string
	gotOpts WebRenderOptions
}

func (r *fakeWebRenderer) Render(ctx context.Context, target string, opts WebRenderOptions) (string, error) {
	_ = ctx
	r.gotURL = target
	r.gotOpts = opts
	return "rendered javascript content", nil
}

func TestWebFetch_RenderedModeUsesRenderer(t *testing.T) {
	renderer := &fakeWebRenderer{}
	tool := &WebFetch{Renderer: renderer}
	out, err := tool.Execute(context.Background(), map[string]any{
		"url":       "https://example.com/app",
		"render":    true,
		"waitUntil": "load",
		"waitMs":    float64(250),
		"selector":  "#app",
	})
	if err != nil {
		t.Fatalf("WebFetch render: %v", err)
	}
	if renderer.gotURL != "https://example.com/app" {
		t.Fatalf("expected renderer URL, got %q", renderer.gotURL)
	}
	if renderer.gotOpts.WaitUntil != "load" || renderer.gotOpts.WaitMS != 250 || renderer.gotOpts.Selector != "#app" {
		t.Fatalf("unexpected render opts: %#v", renderer.gotOpts)
	}
	var result ToolResult
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("parse result: %v", err)
	}
	if result.Kind != "web_fetch" || result.Preview != "rendered javascript content" || fmt.Sprint(result.Stats["mode"]) != "render" {
		t.Fatalf("unexpected render result: %#v", result)
	}
}

func TestWebFetch_BlocksLocalhost(t *testing.T) {
	tool := &WebFetch{}
	_, err := tool.Execute(context.Background(), map[string]any{"url": "http://127.0.0.1:8080"})
	if err == nil {
		t.Fatal("expected localhost fetch to be blocked")
	}
}

func TestWebFetch_HostPolicyDeniesUnknownHost(t *testing.T) {
	tool := &WebFetch{HostPolicy: security.HostPolicy{Enabled: true, DefaultDeny: true, AllowedHosts: []string{"example.com"}}}
	_, err := tool.Execute(context.Background(), map[string]any{"url": testPublicFetchURLBase + "/v1"})
	if err == nil {
		t.Fatal("expected host policy denial")
	}
}

func TestWebFetch_HostPolicyDeniesUnknownLiteralIP(t *testing.T) {
	tool := &WebFetch{HostPolicy: security.HostPolicy{Enabled: true, DefaultDeny: true, AllowedHosts: []string{"example.com"}}}
	_, err := tool.Execute(context.Background(), map[string]any{"url": "http://203.0.113.10/v1"})
	if err == nil {
		t.Fatal("expected host policy denial")
	}
}

func TestWebFetch_PinsValidatedHostIntoDial(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "pinned")
	}))
	defer srv.Close()

	var dialedAddr string
	tool := &WebFetch{
		HTTP:    newPinnedTestClient(t, srv.URL, &dialedAddr),
		Timeout: 2 * time.Second,
	}

	out, err := tool.Execute(context.Background(), map[string]any{"url": testPublicFetchURLBase + "/pinned"})
	if err != nil {
		t.Fatalf("WebFetch: %v", err)
	}
	if dialedAddr == "" {
		t.Fatal("expected dial target to be recorded")
	}
	if !strings.HasPrefix(dialedAddr, "203.0.113.10:") {
		t.Fatalf("expected fetch dial to stay pinned to the validated request IP, got %q", dialedAddr)
	}
	if !strings.Contains(out, "pinned") {
		t.Fatalf("expected test server response in envelope, got %q", out)
	}
}

func TestWebFetch_ActiveProfileWithNoHostsDeniesByDefault(t *testing.T) {
	tool := &WebFetch{}
	ctx := ContextWithActiveProfile(context.Background(), ActiveProfile{Name: "no-network"})
	_, err := tool.Execute(ctx, map[string]any{"url": "http://example.com"})
	if err == nil || !strings.Contains(err.Error(), "host denied by policy") {
		t.Fatalf("expected profile host denial, got %v", err)
	}
}

func TestWebFetch_StopsAfterDefaultRedirectLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, testPublicFetchURLBase+"/loop", http.StatusFound)
	}))
	defer srv.Close()

	tool := &WebFetch{
		HTTP:    newPinnedTestClient(t, srv.URL, nil),
		Timeout: 2 * time.Second,
	}
	_, err := tool.Execute(context.Background(), map[string]any{"url": testPublicFetchURLBase + "/loop"})
	if err == nil {
		t.Fatal("expected redirect loop to fail")
	}
	if !strings.Contains(err.Error(), "stopped after 10 redirects") {
		t.Fatalf("expected redirect limit error, got %v", err)
	}
}

func TestWebFetch_Name(t *testing.T) {
	tool := &WebFetch{}
	if tool.Name() != "web_fetch" {
		t.Errorf("expected 'web_fetch', got %q", tool.Name())
	}
}

func TestWebFetch_Schema(t *testing.T) {
	tool := &WebFetch{}
	schema := tool.Schema()
	if schema["type"] != "function" {
		t.Errorf("expected 'function', got %v", schema["type"])
	}
}

// ---- WebSearch ----

func TestWebSearch_NoAPIKey(t *testing.T) {
	tool := &WebSearch{APIKey: ""}
	_, err := tool.Execute(context.Background(), map[string]any{
		"query": "test",
	})
	if err == nil {
		t.Fatal("expected error when API key is not set")
	}
	if !strings.Contains(err.Error(), "Brave API key") {
		t.Errorf("expected 'Brave API key' in error, got %q", err.Error())
	}
}

func TestWebSearch_Success(t *testing.T) {
	// Mock Brave Search API
	response := map[string]any{
		"web": map[string]any{
			"results": []any{
				map[string]any{
					"title":       "Test Result",
					"url":         "https://example.com",
					"description": "A test result",
				},
			},
		},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(response); err != nil {
			t.Fatalf("Encode: %v", err)
		}
	}))
	defer srv.Close()

	tool := &WebSearch{
		APIKey: "test-key",
		HTTP:   srv.Client(),
	}
	// Override endpoint by changing the HTTP client transport
	// We can't easily override the URL, so let's test via a custom HTTP client
	// that redirects to the test server
	tool.HTTP = &http.Client{
		Transport: &urlRewriteTransport{
			base: srv.URL,
		},
	}

	out, err := tool.Execute(context.Background(), map[string]any{
		"query": "golang test",
	})
	if err != nil {
		t.Fatalf("WebSearch: %v", err)
	}
	if !strings.Contains(out, "Test Result") {
		t.Errorf("expected 'Test Result' in output, got %q", out)
	}
}

func TestWebSearch_ErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, "unauthorized")
	}))
	defer srv.Close()

	tool := &WebSearch{
		APIKey: "bad-key",
		HTTP: &http.Client{
			Transport: &urlRewriteTransport{base: srv.URL},
		},
	}

	_, err := tool.Execute(context.Background(), map[string]any{
		"query": "test",
	})
	if err == nil {
		t.Fatal("expected error for HTTP error response")
	}
}

func TestWebSearch_DefaultCount(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check that count param is in URL
		count := r.URL.Query().Get("count")
		if count != "5" {
			t.Errorf("expected count=5, got %q", count)
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]any{"web": map[string]any{"results": []any{}}}); err != nil {
			t.Fatalf("Encode: %v", err)
		}
	}))
	defer srv.Close()

	tool := &WebSearch{
		APIKey: "test-key",
		HTTP: &http.Client{
			Transport: &urlRewriteTransport{base: srv.URL},
		},
	}

	if _, err := tool.Execute(context.Background(), map[string]any{"query": "test"}); err != nil {
		t.Fatalf("WebSearch default count: %v", err)
	}
}

func TestWebSearch_MaxCountCapped(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := r.URL.Query().Get("count")
		if count != "10" {
			t.Errorf("expected count capped at 10, got %q", count)
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]any{"web": map[string]any{"results": []any{}}}); err != nil {
			t.Fatalf("Encode: %v", err)
		}
	}))
	defer srv.Close()

	tool := &WebSearch{
		APIKey: "test-key",
		HTTP: &http.Client{
			Transport: &urlRewriteTransport{base: srv.URL},
		},
	}

	if _, err := tool.Execute(context.Background(), map[string]any{
		"query": "test",
		"count": float64(100), // exceeds default max of 10
	}); err != nil {
		t.Fatalf("WebSearch capped count: %v", err)
	}
}

func TestWebSearch_Name(t *testing.T) {
	tool := &WebSearch{}
	if tool.Name() != "web_search" {
		t.Errorf("expected 'web_search', got %q", tool.Name())
	}
}

func TestWebSearch_Schema(t *testing.T) {
	tool := &WebSearch{}
	schema := tool.Schema()
	if schema["type"] != "function" {
		t.Errorf("expected 'function', got %v", schema["type"])
	}
}

// urlRewriteTransport rewrites all requests to a test server base URL
type urlRewriteTransport struct {
	base string
}

func (t *urlRewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req2 := req.Clone(req.Context())
	req2.URL.Scheme = "http"
	req2.URL.Host = strings.TrimPrefix(t.base, "http://")
	return http.DefaultTransport.RoundTrip(req2)
}

func newPinnedTestClient(t *testing.T, serverBase string, dialedAddr *string) *http.Client {
	t.Helper()
	serverURL, err := url.Parse(serverBase)
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}
	return &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				if dialedAddr != nil {
					*dialedAddr = addr
				}
				return (&net.Dialer{}).DialContext(ctx, network, serverURL.Host)
			},
		},
	}
}
