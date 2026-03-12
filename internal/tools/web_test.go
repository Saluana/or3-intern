package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"or3-intern/internal/security"
)

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

	tool := &WebFetch{HTTP: &http.Client{Transport: &urlRewriteTransport{base: srv.URL}}}
	out, err := tool.Execute(context.Background(), map[string]any{
		"url": "https://example.com/test",
	})
	if err != nil {
		t.Fatalf("WebFetch: %v", err)
	}
	if !strings.Contains(out, "hello from server") {
		t.Errorf("expected server response in output, got %q", out)
	}
	if !strings.Contains(out, "200") {
		t.Errorf("expected status 200 in output, got %q", out)
	}
}

func TestWebFetch_MaxBytes(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, strings.Repeat("x", 1000))
	}))
	defer srv.Close()

	tool := &WebFetch{HTTP: &http.Client{Transport: &urlRewriteTransport{base: srv.URL}}}
	out, err := tool.Execute(context.Background(), map[string]any{
		"url":      "https://example.com/large",
		"maxBytes": float64(50),
	})
	if err != nil {
		t.Fatalf("WebFetch: %v", err)
	}
	// Body should be limited to 50 bytes
	_ = out
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
	_, err := tool.Execute(context.Background(), map[string]any{"url": "https://api.openai.com/v1"})
	if err == nil {
		t.Fatal("expected host policy denial")
	}
}

func TestWebFetch_PinsValidatedHostIntoDial(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "pinned")
	}))
	defer srv.Close()

	serverURL, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}

	var dialedAddr string
	tool := &WebFetch{
		HTTP: &http.Client{
			Transport: &http.Transport{
				DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
					dialedAddr = addr
					return (&net.Dialer{}).DialContext(ctx, network, serverURL.Host)
				},
			},
		},
		Timeout: 2 * time.Second,
	}

	out, err := tool.Execute(context.Background(), map[string]any{"url": "http://example.com/pinned"})
	if err != nil {
		t.Fatalf("WebFetch: %v", err)
	}
	if dialedAddr == "" {
		t.Fatal("expected dial target to be recorded")
	}
	if strings.HasPrefix(dialedAddr, "example.com:") {
		t.Fatalf("expected fetch dial to use a validated IP, got %q", dialedAddr)
	}
	if !strings.Contains(out, "pinned") {
		t.Fatalf("expected test server response, got %q", out)
	}
}

func TestWebFetch_ActiveProfileWithNoHostsDeniesByDefault(t *testing.T) {
	tool := &WebFetch{}
	ctx := ContextWithActiveProfile(context.Background(), ActiveProfile{Name: "no-network"})
	_, err := tool.Execute(ctx, map[string]any{"url": "https://example.com"})
	if err == nil || !strings.Contains(err.Error(), "host denied by policy") {
		t.Fatalf("expected profile host denial, got %v", err)
	}
}

func TestWebFetch_StopsAfterDefaultRedirectLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://example.com/loop", http.StatusFound)
	}))
	defer srv.Close()

	tool := &WebFetch{
		HTTP:    &http.Client{Transport: &urlRewriteTransport{base: srv.URL}},
		Timeout: 2 * time.Second,
	}
	_, err := tool.Execute(context.Background(), map[string]any{"url": "https://example.com/loop"})
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
		json.NewEncoder(w).Encode(response)
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
		json.NewEncoder(w).Encode(map[string]any{"web": map[string]any{"results": []any{}}})
	}))
	defer srv.Close()

	tool := &WebSearch{
		APIKey: "test-key",
		HTTP: &http.Client{
			Transport: &urlRewriteTransport{base: srv.URL},
		},
	}

	tool.Execute(context.Background(), map[string]any{"query": "test"})
}

func TestWebSearch_MaxCountCapped(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := r.URL.Query().Get("count")
		if count != "10" {
			t.Errorf("expected count capped at 10, got %q", count)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"web": map[string]any{"results": []any{}}})
	}))
	defer srv.Close()

	tool := &WebSearch{
		APIKey: "test-key",
		HTTP: &http.Client{
			Transport: &urlRewriteTransport{base: srv.URL},
		},
	}

	tool.Execute(context.Background(), map[string]any{
		"query": "test",
		"count": float64(100), // exceeds default max of 10
	})
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
