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
		json.NewEncoder(w).Encode(resp)
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
	tool.Execute(ctx, map[string]any{"key": "k", "content": "first"})
	tool.Execute(ctx, map[string]any{"key": "k", "content": "second"})

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
