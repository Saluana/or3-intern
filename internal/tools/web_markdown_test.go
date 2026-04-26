package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"or3-intern/internal/artifacts"
)

func TestWebFetchMarkdownStoresMarkdownArtifact(t *testing.T) {
	d := openArtifactToolTestDB(t)
	store := &artifacts.Store{Dir: t.TempDir(), DB: d}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<html><head><title>Example Page</title><style>.x{}</style><script>alert("bad")</script></head><body><h1>Hello</h1><p>Readable body.</p></body></html>`)
	}))
	defer srv.Close()

	tool := &WebFetchMarkdown{HTTP: newPinnedTestClient(t, srv.URL, nil), Timeout: 2 * time.Second, Store: store}
	out, err := tool.Execute(ContextWithSession(context.Background(), "sess"), map[string]any{
		"url":          testPublicFetchURLBase + "/page",
		"previewBytes": float64(80),
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var result ToolResult
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("parse result: %v", err)
	}
	if result.Kind != "web_fetch_markdown" || result.ArtifactID == "" {
		t.Fatalf("expected markdown artifact result, got %#v", result)
	}
	if strings.Contains(result.Preview, "alert") || strings.Contains(result.Preview, ".x") {
		t.Fatalf("expected script/style stripped from preview, got %q", result.Preview)
	}
	if !strings.Contains(result.Preview, "Hello") {
		t.Fatalf("expected markdown preview, got %q", result.Preview)
	}
	read, err := (&ReadArtifact{Store: store, MaxReadBytes: 2000}).Execute(ContextWithSession(context.Background(), "sess"), map[string]any{"artifact_id": result.ArtifactID})
	if err != nil {
		t.Fatalf("read artifact: %v", err)
	}
	if !strings.Contains(read, "Readable body") || strings.Contains(read, "alert") {
		t.Fatalf("expected cleaned markdown artifact, got %q", read)
	}
}

func TestWebFetchMarkdownRejectsNonHTML(t *testing.T) {
	d := openArtifactToolTestDB(t)
	store := &artifacts.Store{Dir: t.TempDir(), DB: d}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"ok":true}`)
	}))
	defer srv.Close()

	tool := &WebFetchMarkdown{HTTP: newPinnedTestClient(t, srv.URL, nil), Timeout: 2 * time.Second, Store: store}
	_, err := tool.Execute(ContextWithSession(context.Background(), "sess"), map[string]any{"url": testPublicFetchURLBase + "/data"})
	if err == nil || !strings.Contains(err.Error(), "unsupported content type") {
		t.Fatalf("expected unsupported content type error, got %v", err)
	}
}

func TestWebFetchMarkdownHTMLFailureDoesNotStoreArtifact(t *testing.T) {
	d := openArtifactToolTestDB(t)
	store := &artifacts.Store{Dir: t.TempDir(), DB: d}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `<html><body><main><h1>Not Found</h1><p>Missing page.</p></main></body></html>`)
	}))
	defer srv.Close()

	tool := &WebFetchMarkdown{HTTP: newPinnedTestClient(t, srv.URL, nil), Timeout: 2 * time.Second, Store: store}
	out, err := tool.Execute(ContextWithSession(context.Background(), "sess"), map[string]any{"url": testPublicFetchURLBase + "/missing"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var result ToolResult
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("parse result: %v", err)
	}
	if result.OK || result.ArtifactID != "" {
		t.Fatalf("expected failed result without artifact, got %#v", result)
	}
	if fmt.Sprint(result.Stats["mode"]) != "html_text" || !strings.Contains(result.Preview, "Not Found Missing page.") {
		t.Fatalf("expected cleaned failure preview, got %#v / %q", result.Stats, result.Preview)
	}
}
