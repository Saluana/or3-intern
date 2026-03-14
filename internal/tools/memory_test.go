package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"or3-intern/internal/db"
	"or3-intern/internal/providers"
	"or3-intern/internal/scope"
)

func makeMemoryTestDB(t *testing.T) *db.DB {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

func makeEmbedServer(t *testing.T, vec []float32) (*httptest.Server, *providers.Client) {
	t.Helper()
	resp := map[string]any{
		"data": []map[string]any{
			{"embedding": vec},
		},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			t.Fatalf("Encode: %v", err)
		}
	}))
	t.Cleanup(srv.Close)
	c := providers.New(srv.URL, "test-key", 10*time.Second)
	c.HTTP = srv.Client()
	return srv, c
}

// ---- MemorySetPinned ----

func TestMemorySetPinned_NoDB(t *testing.T) {
	tool := &MemorySetPinned{}
	_, err := tool.Execute(context.Background(), map[string]any{
		"key":     "k",
		"content": "v",
	})
	if err == nil {
		t.Fatal("expected error when DB is nil")
	}
}

func TestMemorySetPinned_EmptyKey(t *testing.T) {
	d := makeMemoryTestDB(t)
	tool := &MemorySetPinned{DB: d}
	_, err := tool.Execute(context.Background(), map[string]any{
		"key":     "",
		"content": "value",
	})
	if err == nil {
		t.Fatal("expected error for empty key")
	}
}

func TestMemorySetPinned_EmptyContent(t *testing.T) {
	d := makeMemoryTestDB(t)
	tool := &MemorySetPinned{DB: d}
	_, err := tool.Execute(context.Background(), map[string]any{
		"key":     "mykey",
		"content": "",
	})
	if err == nil {
		t.Fatal("expected error for empty content")
	}
}

func TestMemorySetPinned_Success(t *testing.T) {
	d := makeMemoryTestDB(t)
	tool := &MemorySetPinned{DB: d}
	ctx := ContextWithSession(context.Background(), "session-a")
	out, err := tool.Execute(ctx, map[string]any{
		"key":     "name",
		"content": "Alice",
	})
	if err != nil {
		t.Fatalf("MemorySetPinned: %v", err)
	}
	if out != "ok" {
		t.Errorf("expected 'ok', got %q", out)
	}

	// Verify pinned memory was stored
	pinned, _ := d.GetPinned(context.Background(), "session-a")
	if pinned["name"] != "Alice" {
		t.Errorf("expected pinned['name']='Alice', got %q", pinned["name"])
	}
}

func TestMemorySetPinned_Overwrite(t *testing.T) {
	d := makeMemoryTestDB(t)
	tool := &MemorySetPinned{DB: d}

	ctx := ContextWithSession(context.Background(), "session-a")
	if _, err := tool.Execute(ctx, map[string]any{"key": "k", "content": "first"}); err != nil {
		t.Fatalf("MemorySetPinned first: %v", err)
	}
	if _, err := tool.Execute(ctx, map[string]any{"key": "k", "content": "second"}); err != nil {
		t.Fatalf("MemorySetPinned second: %v", err)
	}

	pinned, _ := d.GetPinned(context.Background(), "session-a")
	if pinned["k"] != "second" {
		t.Errorf("expected 'second', got %q", pinned["k"])
	}
}

func TestMemorySetPinned_GlobalScope(t *testing.T) {
	d := makeMemoryTestDB(t)
	tool := &MemorySetPinned{DB: d}
	if _, err := tool.Execute(context.Background(), map[string]any{"key": "shared", "content": "value", "scope": scope.GlobalScopeAlias}); err != nil {
		t.Fatalf("MemorySetPinned: %v", err)
	}
	pinned, _ := d.GetPinned(context.Background(), "other-session")
	if pinned["shared"] != "value" {
		t.Fatalf("expected global pinned memory, got %#v", pinned)
	}
}

func TestMemorySetPinned_Name(t *testing.T) {
	tool := &MemorySetPinned{}
	if tool.Name() != "memory_set_pinned" {
		t.Errorf("expected 'memory_set_pinned', got %q", tool.Name())
	}
}

func TestMemorySetPinned_Schema(t *testing.T) {
	tool := &MemorySetPinned{}
	schema := tool.Schema()
	if schema["type"] != "function" {
		t.Errorf("expected 'function', got %v", schema["type"])
	}
}

// ---- MemoryAddNote ----

func TestMemoryAddNote_NoDB(t *testing.T) {
	tool := &MemoryAddNote{}
	_, err := tool.Execute(context.Background(), map[string]any{"text": "hello"})
	if err == nil {
		t.Fatal("expected error when DB is nil")
	}
}

func TestMemoryAddNote_EmptyText(t *testing.T) {
	d := makeMemoryTestDB(t)
	_, c := makeEmbedServer(t, []float32{0.1, 0.2})
	tool := &MemoryAddNote{DB: d, Provider: c, EmbedModel: "model"}
	_, err := tool.Execute(context.Background(), map[string]any{"text": ""})
	if err == nil {
		t.Fatal("expected error for empty text")
	}
}

func TestMemoryAddNote_Success(t *testing.T) {
	d := makeMemoryTestDB(t)
	_, c := makeEmbedServer(t, []float32{0.1, 0.2})
	tool := &MemoryAddNote{DB: d, Provider: c, EmbedModel: "model"}
	out, err := tool.Execute(context.Background(), map[string]any{
		"text": "This is a memory note",
		"tags": "important",
	})
	if err != nil {
		t.Fatalf("MemoryAddNote: %v", err)
	}
	if !strings.HasPrefix(out, "ok:") {
		t.Errorf("expected 'ok: ...' output, got %q", out)
	}
}

func TestMemoryAddNote_WithSourceMessageID(t *testing.T) {
	d := makeMemoryTestDB(t)
	_, c := makeEmbedServer(t, []float32{0.1, 0.2})
	tool := &MemoryAddNote{DB: d, Provider: c, EmbedModel: "model"}
	out, err := tool.Execute(context.Background(), map[string]any{
		"text":              "memory with source",
		"source_message_id": float64(42),
	})
	if err != nil {
		t.Fatalf("MemoryAddNote: %v", err)
	}
	if !strings.HasPrefix(out, "ok:") {
		t.Errorf("expected 'ok: ...' output, got %q", out)
	}
}

func TestMemoryAddNote_EmbedFails(t *testing.T) {
	d := makeMemoryTestDB(t)
	// Create a server that returns an error
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, "embed error")
	}))
	defer srv.Close()
	c := providers.New(srv.URL, "key", 10*time.Second)
	c.HTTP = srv.Client()

	tool := &MemoryAddNote{DB: d, Provider: c, EmbedModel: "model"}
	_, err := tool.Execute(context.Background(), map[string]any{"text": "hello"})
	if err == nil {
		t.Fatal("expected error when embed fails")
	}
}

func TestMemoryAddNote_Name(t *testing.T) {
	tool := &MemoryAddNote{}
	if tool.Name() != "memory_add_note" {
		t.Errorf("expected 'memory_add_note', got %q", tool.Name())
	}
}

func TestMemoryAddNote_Schema(t *testing.T) {
	tool := &MemoryAddNote{}
	schema := tool.Schema()
	if schema["type"] != "function" {
		t.Errorf("expected 'function', got %v", schema["type"])
	}
}

// ---- MemorySearch ----

func TestMemorySearch_NoDB(t *testing.T) {
	tool := &MemorySearch{}
	_, err := tool.Execute(context.Background(), map[string]any{"query": "test"})
	if err == nil {
		t.Fatal("expected error when DB is nil")
	}
}

func TestMemorySearch_EmptyQuery(t *testing.T) {
	d := makeMemoryTestDB(t)
	_, c := makeEmbedServer(t, []float32{0.1, 0.2})
	tool := &MemorySearch{DB: d, Provider: c, EmbedModel: "model"}
	_, err := tool.Execute(context.Background(), map[string]any{"query": ""})
	if err == nil {
		t.Fatal("expected error for empty query")
	}
}

func TestMemorySearch_EmptyDB(t *testing.T) {
	d := makeMemoryTestDB(t)
	_, c := makeEmbedServer(t, []float32{0.1, 0.2})
	tool := &MemorySearch{
		DB:              d,
		Provider:        c,
		EmbedModel:      "model",
		VectorK:         5,
		FTSK:            5,
		TopK:            5,
		VectorScanLimit: 100,
	}
	out, err := tool.Execute(context.Background(), map[string]any{"query": "hello"})
	if err != nil {
		t.Fatalf("MemorySearch: %v", err)
	}
	if out != "" {
		t.Errorf("expected empty output for empty db, got %q", out)
	}
}

func TestMemorySearch_WithTopKParam(t *testing.T) {
	d := makeMemoryTestDB(t)
	_, c := makeEmbedServer(t, []float32{0.1, 0.2})
	tool := &MemorySearch{
		DB:         d,
		Provider:   c,
		EmbedModel: "model",
		TopK:       3,
	}
	out, err := tool.Execute(context.Background(), map[string]any{
		"query": "test",
		"topK":  float64(2),
	})
	if err != nil {
		t.Fatalf("MemorySearch: %v", err)
	}
	_ = out
}

func TestMemorySearch_EmbedFails(t *testing.T) {
	d := makeMemoryTestDB(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, "error")
	}))
	defer srv.Close()
	c := providers.New(srv.URL, "key", 10*time.Second)
	c.HTTP = srv.Client()

	tool := &MemorySearch{DB: d, Provider: c, EmbedModel: "model", TopK: 5}
	_, err := tool.Execute(context.Background(), map[string]any{"query": "test"})
	if err == nil {
		t.Fatal("expected error when embed fails")
	}
}

func TestMemorySearch_Name(t *testing.T) {
	tool := &MemorySearch{}
	if tool.Name() != "memory_search" {
		t.Errorf("expected 'memory_search', got %q", tool.Name())
	}
}

func TestMemorySearch_Schema(t *testing.T) {
	tool := &MemorySearch{}
	schema := tool.Schema()
	if schema["type"] != "function" {
		t.Errorf("expected 'function', got %v", schema["type"])
	}
}

// ---- MemoryRecent ----

func TestMemoryRecent_NoDB(t *testing.T) {
	tool := &MemoryRecent{}
	_, err := tool.Execute(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error when DB is nil")
	}
}

func TestMemoryRecent_ScopedHistoryAndLimitCap(t *testing.T) {
	d := makeMemoryTestDB(t)
	ctx := context.Background()
	if err := d.LinkSession(ctx, "session-a", "scope-1", nil); err != nil {
		t.Fatalf("LinkSession a: %v", err)
	}
	if err := d.LinkSession(ctx, "session-b", "scope-1", nil); err != nil {
		t.Fatalf("LinkSession b: %v", err)
	}
	for _, msg := range []struct {
		session string
		role    string
		content string
	}{
		{"session-a", "user", "user one"},
		{"session-a", "assistant", "assistant one"},
		{"session-b", "user", "user two"},
		{"session-b", "assistant", "assistant two"},
		{"session-a", "user", "user three"},
		{"session-a", "assistant", "assistant three"},
	} {
		if _, err := d.AppendMessage(ctx, msg.session, msg.role, msg.content, nil); err != nil {
			t.Fatalf("AppendMessage(%s, %s): %v", msg.session, msg.role, err)
		}
	}
	tool := &MemoryRecent{DB: d, DefaultLimit: 2, MaxLimit: 4, MaxChars: 50}
	out, err := tool.Execute(ContextWithSession(ctx, "session-a"), map[string]any{"limit": float64(99)})
	if err != nil {
		t.Fatalf("MemoryRecent: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 4 {
		t.Fatalf("expected 4 lines after limit cap, got %d: %q", len(lines), out)
	}
	if strings.Contains(out, "user one") || strings.Contains(out, "assistant one") {
		t.Fatalf("expected oldest messages to be excluded by limit cap, got %q", out)
	}
	if !strings.Contains(out, "[session-b/user] user two") || !strings.Contains(out, "[session-a/assistant] assistant three") {
		t.Fatalf("expected scoped recent history in output, got %q", out)
	}
}

func TestMemoryRecent_Name(t *testing.T) {
	tool := &MemoryRecent{}
	if tool.Name() != "memory_recent" {
		t.Errorf("expected 'memory_recent', got %q", tool.Name())
	}
}

func TestMemoryRecent_Schema(t *testing.T) {
	tool := &MemoryRecent{}
	schema := tool.Schema()
	if schema["type"] != "function" {
		t.Errorf("expected 'function', got %v", schema["type"])
	}
}

// ---- MemoryGetPinned ----

func TestMemoryGetPinned_NoDB(t *testing.T) {
	tool := &MemoryGetPinned{}
	_, err := tool.Execute(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error when DB is nil")
	}
}

func TestMemoryGetPinned_MergesScopedAndGlobalPinned(t *testing.T) {
	d := makeMemoryTestDB(t)
	ctx := context.Background()
	if err := d.UpsertPinned(ctx, scope.GlobalMemoryScope, "shared", "global value"); err != nil {
		t.Fatalf("UpsertPinned global shared: %v", err)
	}
	if err := d.UpsertPinned(ctx, "session-a", "shared", "session value"); err != nil {
		t.Fatalf("UpsertPinned session shared: %v", err)
	}
	if err := d.UpsertPinned(ctx, "session-a", "local", "only here"); err != nil {
		t.Fatalf("UpsertPinned session local: %v", err)
	}
	tool := &MemoryGetPinned{DB: d, MaxChars: 100}
	out, err := tool.Execute(ContextWithSession(ctx, "session-a"), nil)
	if err != nil {
		t.Fatalf("MemoryGetPinned: %v", err)
	}
	if !strings.Contains(out, "local: only here") {
		t.Fatalf("expected local pinned memory, got %q", out)
	}
	if !strings.Contains(out, "shared: session value") {
		t.Fatalf("expected session entry to override shared value, got %q", out)
	}
	if strings.Contains(out, "global value") {
		t.Fatalf("expected session override to hide global value, got %q", out)
	}
	if strings.Index(out, "local: only here") > strings.Index(out, "shared: session value") {
		t.Fatalf("expected sorted output, got %q", out)
	}
}

func TestMemoryGetPinned_KeyAndGlobalScopeOverride(t *testing.T) {
	d := makeMemoryTestDB(t)
	ctx := context.Background()
	if err := d.UpsertPinned(ctx, scope.GlobalMemoryScope, "shared", "global value"); err != nil {
		t.Fatalf("UpsertPinned global shared: %v", err)
	}
	if err := d.UpsertPinned(ctx, "session-a", "shared", "session value"); err != nil {
		t.Fatalf("UpsertPinned session shared: %v", err)
	}
	tool := &MemoryGetPinned{DB: d, MaxChars: 100}
	out, err := tool.Execute(ContextWithSession(ctx, "session-a"), map[string]any{"key": "shared"})
	if err != nil {
		t.Fatalf("MemoryGetPinned session key: %v", err)
	}
	if out != "shared: session value" {
		t.Fatalf("expected session-scoped key value, got %q", out)
	}
	out, err = tool.Execute(ContextWithSession(ctx, "session-a"), map[string]any{"key": "shared", "scope": scope.GlobalScopeAlias})
	if err != nil {
		t.Fatalf("MemoryGetPinned global key: %v", err)
	}
	if out != "shared: global value" {
		t.Fatalf("expected global-scoped key value, got %q", out)
	}
	out, err = tool.Execute(ContextWithSession(ctx, "session-a"), map[string]any{"key": "missing"})
	if err != nil {
		t.Fatalf("MemoryGetPinned missing key: %v", err)
	}
	if out != "" {
		t.Fatalf("expected empty output for missing key, got %q", out)
	}
}

func TestMemoryGetPinned_Name(t *testing.T) {
	tool := &MemoryGetPinned{}
	if tool.Name() != "memory_get_pinned" {
		t.Errorf("expected 'memory_get_pinned', got %q", tool.Name())
	}
}

func TestMemoryGetPinned_Schema(t *testing.T) {
	tool := &MemoryGetPinned{}
	schema := tool.Schema()
	if schema["type"] != "function" {
		t.Errorf("expected 'function', got %v", schema["type"])
	}
}
