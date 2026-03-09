package tools

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
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
func (t *MemorySetPinned) Description() string {
	return "Upsert a pinned memory entry (always included in prompts)."
}
func (t *MemorySetPinned) Parameters() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{
		"key":     map[string]any{"type": "string"},
		"content": map[string]any{"type": "string"},
		"scope":   map[string]any{"type": "string", "description": "Optional scope override: 'global' to share across sessions"},
	}, "required": []string{"key", "content"}}
}
func (t *MemorySetPinned) Schema() map[string]any {
	return t.SchemaFor(t.Name(), t.Description(), t.Parameters())
}
func (t *MemorySetPinned) Execute(ctx context.Context, params map[string]any) (string, error) {
	if t.DB == nil {
		return "", fmt.Errorf("db not set")
	}
	key := stringParam(params, "key")
	content := stringParam(params, "content")
	if key == "" || content == "" {
		return "", fmt.Errorf("missing key/content")
	}
	if err := t.DB.UpsertPinned(ctx, memoryScopeFromParams(ctx, params), key, content); err != nil {
		return "", err
	}
	return "ok", nil
}

type MemoryAddNote struct {
	Base
	DB         *db.DB
	Provider   *providers.Client
	EmbedModel string
}

func (t *MemoryAddNote) Name() string { return "memory_add_note" }
func (t *MemoryAddNote) Description() string {
	return "Add a semantic memory note to the indexed memory store."
}
func (t *MemoryAddNote) Parameters() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{
		"text":              map[string]any{"type": "string"},
		"tags":              map[string]any{"type": "string", "description": "comma-separated tags (optional)"},
		"source_message_id": map[string]any{"type": "integer", "description": "source message id (optional)"},
		"scope":             map[string]any{"type": "string", "description": "Optional scope override: 'global' to share across sessions"},
	}, "required": []string{"text"}}
}
func (t *MemoryAddNote) Schema() map[string]any {
	return t.SchemaFor(t.Name(), t.Description(), t.Parameters())
}
func (t *MemoryAddNote) Execute(ctx context.Context, params map[string]any) (string, error) {
	if t.DB == nil || t.Provider == nil {
		return "", fmt.Errorf("missing deps")
	}
	text := stringParam(params, "text")
	if text == "" {
		return "", fmt.Errorf("empty text")
	}
	tags := stringParam(params, "tags")
	var src sql.NullInt64
	if v, ok := params["source_message_id"].(float64); ok && int64(v) > 0 {
		src = sql.NullInt64{Int64: int64(v), Valid: true}
	}
	vec, err := t.Provider.Embed(ctx, t.EmbedModel, text)
	if err != nil {
		return "", err
	}
	blob := memory.PackFloat32(vec)
	id, err := t.DB.InsertMemoryNote(ctx, memoryScopeFromParams(ctx, params), text, blob, src, tags)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("ok: %d", id), nil
}

type MemorySearch struct {
	Base
	DB              *db.DB
	Provider        *providers.Client
	EmbedModel      string
	VectorK         int
	FTSK            int
	TopK            int
	VectorScanLimit int
}

func (t *MemorySearch) Name() string { return "memory_search" }
func (t *MemorySearch) Description() string {
	return "Search long-term memory (hybrid semantic + keyword) and return top results."
}
func (t *MemorySearch) Parameters() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{
		"query": map[string]any{"type": "string"},
		"topK":  map[string]any{"type": "integer"},
		"scope": map[string]any{"type": "string", "description": "Optional scope override: 'global' to search only shared memory"},
	}, "required": []string{"query"}}
}
func (t *MemorySearch) Schema() map[string]any {
	return t.SchemaFor(t.Name(), t.Description(), t.Parameters())
}
func (t *MemorySearch) Execute(ctx context.Context, params map[string]any) (string, error) {
	if t.DB == nil || t.Provider == nil {
		return "", fmt.Errorf("missing deps")
	}
	q := stringParam(params, "query")
	if q == "" {
		return "", fmt.Errorf("empty query")
	}
	topK := t.TopK
	if v, ok := params["topK"].(float64); ok && int(v) > 0 {
		topK = int(v)
	}
	vec, err := t.Provider.Embed(ctx, t.EmbedModel, q)
	if err != nil {
		return "", err
	}
	r := memory.NewRetriever(t.DB)
	r.VectorScanLimit = t.VectorScanLimit
	got, err := r.Retrieve(ctx, memoryScopeFromParams(ctx, params), q, vec, t.VectorK, t.FTSK, topK)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for i, m := range got {
		b.WriteString(fmt.Sprintf("%d. [%s] %.4f %s\n", i+1, m.Source, m.Score, m.Text))
	}
	return b.String(), nil
}

type MemoryRecent struct {
	Base
	DB           *db.DB
	DefaultLimit int
	MaxLimit     int
	MaxChars     int
}

func (t *MemoryRecent) Name() string { return "memory_recent" }
func (t *MemoryRecent) Description() string {
	return "Fetch recent conversation messages from the current linked session scope."
}
func (t *MemoryRecent) Parameters() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{
		"limit": map[string]any{"type": "integer"},
	}}
}
func (t *MemoryRecent) Schema() map[string]any {
	return t.SchemaFor(t.Name(), t.Description(), t.Parameters())
}
func (t *MemoryRecent) Execute(ctx context.Context, params map[string]any) (string, error) {
	if t.DB == nil {
		return "", fmt.Errorf("db not set")
	}
	limit := boundedPositiveInt(params["limit"], t.DefaultLimit, t.MaxLimit)
	msgs, err := t.DB.GetLastMessagesScoped(ctx, SessionFromContext(ctx), limit)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for i, msg := range msgs {
		b.WriteString(fmt.Sprintf("%d. [%s/%s] %s\n", i+1, msg.SessionKey, msg.Role, compactMemoryText(msg.Content, t.MaxChars)))
	}
	return b.String(), nil
}

type MemoryGetPinned struct {
	Base
	DB       *db.DB
	MaxChars int
}

func (t *MemoryGetPinned) Name() string { return "memory_get_pinned" }
func (t *MemoryGetPinned) Description() string {
	return "Read pinned memory entries for the current session, including shared global entries."
}
func (t *MemoryGetPinned) Parameters() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{
		"key":   map[string]any{"type": "string", "description": "Optional pinned memory key to fetch"},
		"scope": map[string]any{"type": "string", "description": "Optional scope override: 'global' to read only shared memory"},
	}}
}
func (t *MemoryGetPinned) Schema() map[string]any {
	return t.SchemaFor(t.Name(), t.Description(), t.Parameters())
}
func (t *MemoryGetPinned) Execute(ctx context.Context, params map[string]any) (string, error) {
	if t.DB == nil {
		return "", fmt.Errorf("db not set")
	}
	pinned, err := t.DB.GetPinned(ctx, memoryScopeFromParams(ctx, params))
	if err != nil {
		return "", err
	}
	key := stringParam(params, "key")
	if key != "" {
		value, ok := pinned[key]
		if !ok {
			return "", nil
		}
		return fmt.Sprintf("%s: %s", key, compactMemoryText(value, t.MaxChars)), nil
	}
	keys := make([]string, 0, len(pinned))
	for key := range pinned {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, key := range keys {
		b.WriteString(fmt.Sprintf("%s: %s\n", key, compactMemoryText(pinned[key], t.MaxChars)))
	}
	return b.String(), nil
}

func memoryScopeFromParams(ctx context.Context, params map[string]any) string {
	if requestedScope := stringParam(params, "scope"); scope.IsGlobalScopeRequest(requestedScope) {
		return scope.GlobalMemoryScope
	}
	return SessionFromContext(ctx)
}

func stringParam(params map[string]any, key string) string {
	if params == nil {
		return ""
	}
	value, ok := params[key]
	if !ok || value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func boundedPositiveInt(raw any, fallback, max int) int {
	value := fallback
	if v, ok := raw.(float64); ok && int(v) > 0 {
		value = int(v)
	}
	if max > 0 && value > max {
		return max
	}
	if value <= 0 {
		return 1
	}
	return value
}

func compactMemoryText(text string, maxChars int) string {
	text = strings.Join(strings.Fields(text), " ")
	if maxChars <= 0 || len(text) <= maxChars {
		return text
	}
	if maxChars <= 3 {
		return text[:maxChars]
	}
	return text[:maxChars-3] + "..."
}
