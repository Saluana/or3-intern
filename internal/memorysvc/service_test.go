package memorysvc

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"or3-intern/internal/config"
	"or3-intern/internal/db"
	"or3-intern/internal/memory"
	"or3-intern/internal/providers"
	"or3-intern/internal/scope"
)

func TestAddNoteRejectsSecretLikeContent(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "mem.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	svc := New(config.Default(), database, providers.New("http://127.0.0.1:9", "k", 0), "fp")
	_, err = svc.AddNote(context.Background(), AddNoteRequest{
		SessionKey: "sess:1",
		Text:       "api_key=sk-live-" + strings.Repeat("x", 40),
	})
	if err == nil {
		t.Fatal("expected secret-like note to be rejected")
	}
}

func TestSetPinnedScopeIsolation(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "mem.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	svc := New(config.Default(), database, nil, "fp")
	if err := svc.SetPinned(context.Background(), SetPinnedRequest{
		SessionKey: "sess:a",
		Key:        "pref",
		Content:    "session only",
	}); err != nil {
		t.Fatal(err)
	}
	entries, err := svc.GetPinned(context.Background(), "sess:b", "", false)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Key == "pref" {
			t.Fatal("expected pinned memory isolated to session")
		}
	}
	global, err := svc.GetPinned(context.Background(), "", "pref", true)
	if err != nil || len(global) != 0 {
		t.Fatalf("global scope should not see session pin, got %#v err=%v", global, err)
	}
}

func TestSearchWithoutProviderUsesKeywordRecall(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "mem.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	if _, err := database.InsertMemoryNote(context.Background(), "sess:kw", "runner keyword note", nil, sql.NullInt64{}, ""); err != nil {
		t.Fatal(err)
	}
	svc := New(config.Default(), database, nil, "fp")
	resp, err := svc.Search(context.Background(), SearchRequest{SessionKey: "sess:kw", Query: "keyword"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Warning == "" {
		t.Fatal("expected keyword-only warning")
	}
	if len(resp.Hits) == 0 {
		t.Fatal("expected FTS hit without embedding provider")
	}
}

func TestSearchBoundsTopK(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "mem.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	vec := []float32{0.1, 0.2}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(providers.EmbeddingResponse{Data: []struct {
			Embedding []float32 `json:"embedding"`
		}{{Embedding: vec}}})
	}))
	t.Cleanup(srv.Close)
	prov := providers.New(srv.URL, "k", time.Second)
	svc := &Service{
		DB: database, Provider: prov, EmbedModel: "m", EmbedFingerprint: "fp",
		VectorK: 5, FTSK: 5, TopK: 3, VectorScanLimit: 10,
	}
	blob := memory.PackFloat32(vec)
	if _, err := database.InsertMemoryNoteTyped(context.Background(), scope.GlobalMemoryScope, db.TypedNoteInput{
		Text: "alpha", Embedding: blob, EmbedFingerprint: "fp",
	}); err != nil {
		t.Fatal(err)
	}
	resp, err := svc.Search(context.Background(), SearchRequest{SessionKey: scope.GlobalMemoryScope, Query: "alpha", TopK: 99})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Hits) > maxSearchTopK {
		t.Fatalf("expected topK capped at %d, got %d", maxSearchTopK, len(resp.Hits))
	}
}
