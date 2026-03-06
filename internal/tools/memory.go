package tools

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"or3-intern/internal/db"
	"or3-intern/internal/memory"
	"or3-intern/internal/providers"
	"or3-intern/internal/scope"
)

type MemorySetPinned struct {
	Base
	DB *db.DB
}
func (t *MemorySetPinned) Name() string { return "memory_set_pinned" }
func (t *MemorySetPinned) Description() string { return "Upsert a pinned memory entry (always included in prompts)." }
func (t *MemorySetPinned) Parameters() map[string]any {
	return map[string]any{"type":"object","properties":map[string]any{
		"key": map[string]any{"type":"string"},
		"content": map[string]any{"type":"string"},
		"scope": map[string]any{"type":"string", "description":"Optional scope override: 'global' to share across sessions"},
	},"required":[]string{"key","content"}}
}
func (t *MemorySetPinned) Schema() map[string]any { return t.SchemaFor(t.Name(), t.Description(), t.Parameters()) }
func (t *MemorySetPinned) Execute(ctx context.Context, params map[string]any) (string, error) {
	if t.DB == nil { return "", fmt.Errorf("db not set") }
	key := strings.TrimSpace(fmt.Sprint(params["key"]))
	content := strings.TrimSpace(fmt.Sprint(params["content"]))
	if key == "" || content == "" { return "", fmt.Errorf("missing key/content") }
	if err := t.DB.UpsertPinned(ctx, memoryScopeFromParams(ctx, params), key, content); err != nil { return "", err }
	return "ok", nil
}

type MemoryAddNote struct {
	Base
	DB *db.DB
	Provider *providers.Client
	EmbedModel string
}
func (t *MemoryAddNote) Name() string { return "memory_add_note" }
func (t *MemoryAddNote) Description() string { return "Add a semantic memory note to the indexed memory store." }
func (t *MemoryAddNote) Parameters() map[string]any {
	return map[string]any{"type":"object","properties":map[string]any{
		"text": map[string]any{"type":"string"},
		"tags": map[string]any{"type":"string","description":"comma-separated tags (optional)"},
		"source_message_id": map[string]any{"type":"integer","description":"source message id (optional)"},
		"scope": map[string]any{"type":"string", "description":"Optional scope override: 'global' to share across sessions"},
	},"required":[]string{"text"}}
}
func (t *MemoryAddNote) Schema() map[string]any { return t.SchemaFor(t.Name(), t.Description(), t.Parameters()) }
func (t *MemoryAddNote) Execute(ctx context.Context, params map[string]any) (string, error) {
	if t.DB == nil || t.Provider == nil { return "", fmt.Errorf("missing deps") }
	text := strings.TrimSpace(fmt.Sprint(params["text"]))
	if text == "" { return "", fmt.Errorf("empty text") }
	tags := strings.TrimSpace(fmt.Sprint(params["tags"]))
	var src sql.NullInt64
	if v, ok := params["source_message_id"].(float64); ok && int64(v) > 0 {
		src = sql.NullInt64{Int64: int64(v), Valid: true}
	}
	vec, err := t.Provider.Embed(ctx, t.EmbedModel, text)
	if err != nil { return "", err }
	blob := memory.PackFloat32(vec)
	id, err := t.DB.InsertMemoryNote(ctx, memoryScopeFromParams(ctx, params), text, blob, src, tags)
	if err != nil { return "", err }
	return fmt.Sprintf("ok: %d", id), nil
}

type MemorySearch struct {
	Base
	DB *db.DB
	Provider *providers.Client
	EmbedModel string
	VectorK int
	FTSK int
	TopK int
	VectorScanLimit int
}
func (t *MemorySearch) Name() string { return "memory_search" }
func (t *MemorySearch) Description() string { return "Search long-term memory (hybrid semantic + keyword) and return top results." }
func (t *MemorySearch) Parameters() map[string]any {
	return map[string]any{"type":"object","properties":map[string]any{
		"query": map[string]any{"type":"string"},
		"topK": map[string]any{"type":"integer"},
		"scope": map[string]any{"type":"string", "description":"Optional scope override: 'global' to search only shared memory"},
	},"required":[]string{"query"}}
}
func (t *MemorySearch) Schema() map[string]any { return t.SchemaFor(t.Name(), t.Description(), t.Parameters()) }
func (t *MemorySearch) Execute(ctx context.Context, params map[string]any) (string, error) {
	if t.DB == nil || t.Provider == nil { return "", fmt.Errorf("missing deps") }
	q := strings.TrimSpace(fmt.Sprint(params["query"]))
	if q == "" { return "", fmt.Errorf("empty query") }
	topK := t.TopK
	if v, ok := params["topK"].(float64); ok && int(v) > 0 { topK = int(v) }
	vec, err := t.Provider.Embed(ctx, t.EmbedModel, q)
	if err != nil { return "", err }
	r := memory.NewRetriever(t.DB)
	r.VectorScanLimit = t.VectorScanLimit
	got, err := r.Retrieve(ctx, memoryScopeFromParams(ctx, params), q, vec, t.VectorK, t.FTSK, topK)
	if err != nil { return "", err }
	var b strings.Builder
	for i, m := range got {
		b.WriteString(fmt.Sprintf("%d. [%s] %.4f %s\n", i+1, m.Source, m.Score, m.Text))
	}
	return b.String(), nil
}

func memoryScopeFromParams(ctx context.Context, params map[string]any) string {
	if requestedScope := strings.TrimSpace(fmt.Sprint(params["scope"])); scope.IsGlobalScopeRequest(requestedScope) {
		return scope.GlobalMemoryScope
	}
	return SessionFromContext(ctx)
}
