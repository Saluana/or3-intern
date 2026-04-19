package memory

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"or3-intern/internal/db"
	"or3-intern/internal/providers"
)

func openConsolidateTestDB(t *testing.T) *db.DB {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

type callCounts struct {
	Chat  *int32
	Embed *int32
}

func buildConsolidationProvider(t *testing.T, chatBody string, embedOK bool) (*providers.Client, callCounts) {
	t.Helper()
	var chatCalls int32
	var embedCalls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/chat/completions":
			atomic.AddInt32(&chatCalls, 1)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"choices": []map[string]any{
					{"message": map[string]any{"role": "assistant", "content": chatBody}},
				},
			})
		case "/embeddings":
			atomic.AddInt32(&embedCalls, 1)
			if !embedOK {
				http.Error(w, "embed fail", http.StatusBadGateway)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": []map[string]any{{"embedding": []float32{0.1, 0.2}}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	p := providers.New(srv.URL, "test-key", 5*time.Second)
	p.HTTP = srv.Client()
	return p, callCounts{Chat: &chatCalls, Embed: &embedCalls}
}

func TestConsolidator_NilProvider(t *testing.T) {
	d := openConsolidateTestDB(t)
	c := &Consolidator{DB: d}
	if err := c.MaybeConsolidate(context.Background(), "sess", 10); err != nil {
		t.Fatalf("expected nil error for nil provider, got: %v", err)
	}
}

func TestConsolidator_TooFewMessages_NoProviderCall(t *testing.T) {
	d := openConsolidateTestDB(t)
	ctx := context.Background()
	for i := 0; i < 4; i++ {
		if _, err := d.AppendMessage(ctx, "sess", "user", "msg", nil); err != nil {
			t.Fatalf("AppendMessage: %v", err)
		}
	}
	prov, calls := buildConsolidationProvider(t, `{"summary":"x","canonical_memory":"- x"}`, true)
	c := &Consolidator{DB: d, Provider: prov, WindowSize: 5}
	if err := c.MaybeConsolidate(ctx, "sess", 2); err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}
	if atomic.LoadInt32(calls.Chat) != 0 {
		t.Fatalf("expected no chat calls, got %d", atomic.LoadInt32(calls.Chat))
	}
}

func TestConsolidator_RunOnce_PersistsNoteCursorAndCanonical(t *testing.T) {
	d := openConsolidateTestDB(t)
	ctx := context.Background()
	for i := 0; i < 14; i++ {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		if _, err := d.AppendMessage(ctx, "sess", role, "message "+string(rune('a'+i)), nil); err != nil {
			t.Fatalf("AppendMessage: %v", err)
		}
	}
	prov, calls := buildConsolidationProvider(t, `{"summary":"Short summary.","preferences":["prefers concise output"],"facts":[],"goals":[],"procedures":[]}`, true)
	c := &Consolidator{
		DB:                 d,
		Provider:           prov,
		WindowSize:         5,
		MaxMessages:        50,
		MaxInputChars:      12000,
		EmbedModel:         "embed-model",
		CanonicalPinnedKey: "long_term_memory",
	}
	didWork, err := c.RunOnce(ctx, "sess", 5, RunMode{})
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if !didWork {
		t.Fatal("expected consolidation work")
	}
	if atomic.LoadInt32(calls.Chat) != 1 {
		t.Fatalf("expected 1 chat call, got %d", atomic.LoadInt32(calls.Chat))
	}
	if atomic.LoadInt32(calls.Embed) != 1 {
		t.Fatalf("expected 1 embed call, got %d", atomic.LoadInt32(calls.Embed))
	}

	lastID, _, err := d.GetConsolidationRange(ctx, "sess", 5)
	if err != nil {
		t.Fatalf("GetConsolidationRange: %v", err)
	}
	if lastID == 0 {
		t.Fatal("expected cursor to advance")
	}
	pinned, ok, err := d.GetPinnedValue(ctx, "sess", "long_term_memory")
	if err != nil {
		t.Fatalf("GetPinnedValue: %v", err)
	}
	if !ok || !strings.Contains(pinned, "concise output") {
		t.Fatalf("expected canonical memory update, got ok=%v value=%q", ok, pinned)
	}
}

func TestConsolidator_EmptyTranscript_AdvancesCursor(t *testing.T) {
	d := openConsolidateTestDB(t)
	ctx := context.Background()
	var ids []int64
	for i := 0; i < 6; i++ {
		id, err := d.AppendMessage(ctx, "sess", "tool", "tool output", nil)
		if err != nil {
			t.Fatalf("AppendMessage: %v", err)
		}
		ids = append(ids, id)
	}
	prov, calls := buildConsolidationProvider(t, `{"summary":"unused","canonical_memory":"unused"}`, true)
	c := &Consolidator{DB: d, Provider: prov, WindowSize: 1, MaxMessages: 50, MaxInputChars: 12000}
	didWork, err := c.RunOnce(ctx, "sess", 1, RunMode{ArchiveAll: true})
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if !didWork {
		t.Fatal("expected didWork true for cursor advancement")
	}
	if atomic.LoadInt32(calls.Chat) != 0 {
		t.Fatalf("expected no chat call for empty transcript, got %d", atomic.LoadInt32(calls.Chat))
	}
	lastID, _, err := d.GetConsolidationRange(ctx, "sess", 1)
	if err != nil {
		t.Fatalf("GetConsolidationRange: %v", err)
	}
	if lastID != ids[len(ids)-1] {
		t.Fatalf("expected cursor=%d, got %d", ids[len(ids)-1], lastID)
	}
}

func TestConsolidator_ArchiveAll_MultiPass(t *testing.T) {
	d := openConsolidateTestDB(t)
	ctx := context.Background()
	for i := 0; i < 120; i++ {
		if _, err := d.AppendMessage(ctx, "sess", "user", "line", nil); err != nil {
			t.Fatalf("AppendMessage: %v", err)
		}
	}
	prov, calls := buildConsolidationProvider(t, `{"summary":"pass summary","preferences":["memory"],"facts":[],"goals":[],"procedures":[]}`, false)
	c := &Consolidator{
		DB:                 d,
		Provider:           prov,
		WindowSize:         10,
		MaxMessages:        25,
		MaxInputChars:      2000,
		CanonicalPinnedKey: "long_term_memory",
	}
	if err := c.ArchiveAll(ctx, "sess", 40); err != nil {
		t.Fatalf("ArchiveAll: %v", err)
	}
	lastID, oldestID, err := d.GetConsolidationRange(ctx, "sess", 40)
	if err != nil {
		t.Fatalf("GetConsolidationRange: %v", err)
	}
	if oldestID != 0 && lastID < oldestID {
		t.Fatalf("expected cursor to move through archive-all range, got last=%d oldest=%d", lastID, oldestID)
	}
	if atomic.LoadInt32(calls.Chat) < 2 {
		t.Fatalf("expected multiple chat calls for multipass archive, got %d", atomic.LoadInt32(calls.Chat))
	}
}

func TestConsolidator_MaxInputCharsBoundsPromptAndSkipsEmbedOnFailure(t *testing.T) {
	d := openConsolidateTestDB(t)
	ctx := context.Background()
	for i := 0; i < 12; i++ {
		if _, err := d.AppendMessage(ctx, "sess", "user", strings.Repeat("x", 400), nil); err != nil {
			t.Fatalf("AppendMessage: %v", err)
		}
	}
	prov, calls := buildConsolidationProvider(t, `{"summary":"bounded summary","preferences":["bounded"],"facts":[],"goals":[],"procedures":[]}`, false)
	c := &Consolidator{
		DB:            d,
		Provider:      prov,
		WindowSize:    5,
		MaxMessages:   50,
		MaxInputChars: 500,
		EmbedModel:    "embed-model",
	}
	didWork, err := c.RunOnce(ctx, "sess", 5, RunMode{})
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if !didWork {
		t.Fatal("expected work to be done")
	}
	if atomic.LoadInt32(calls.Embed) != 1 {
		t.Fatalf("expected embed attempt, got %d", atomic.LoadInt32(calls.Embed))
	}
}

func TestConsolidator_RunOnce_OnlyAdvancesThroughIncludedMessages(t *testing.T) {
	d := openConsolidateTestDB(t)
	ctx := context.Background()
	ids := make([]int64, 0, 3)
	for _, content := range []string{"short one", "short two", strings.Repeat("z", 300)} {
		id, err := d.AppendMessage(ctx, "sess", "user", content, nil)
		if err != nil {
			t.Fatalf("AppendMessage: %v", err)
		}
		ids = append(ids, id)
	}
	prov, _ := buildConsolidationProvider(t, `{"summary":"bounded summary","facts":[],"preferences":["bounded"],"goals":[],"procedures":[]}`, true)
	c := &Consolidator{
		DB:            d,
		Provider:      prov,
		WindowSize:    1,
		MaxMessages:   10,
		MaxInputChars: 40,
	}
	didWork, err := c.RunOnce(ctx, "sess", 1, RunMode{ArchiveAll: true})
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if !didWork {
		t.Fatal("expected work to be done")
	}
	lastID, _, err := d.GetConsolidationRange(ctx, "sess", 1)
	if err != nil {
		t.Fatalf("GetConsolidationRange: %v", err)
	}
	if lastID != ids[1] {
		t.Fatalf("expected cursor to stop at second included message %d, got %d", ids[1], lastID)
	}
	rows, err := d.StreamMemoryNotesScopeLimit(ctx, "sess", 10)
	if err != nil {
		t.Fatalf("StreamMemoryNotesScopeLimit: %v", err)
	}
	defer rows.Close()
	var note db.MemoryNoteRow
	if !rows.Next() {
		t.Fatal("expected at least one memory note")
	}
	if err := rows.Scan(&note.ID, &note.Text, &note.Embedding, &note.SourceMessageID, &note.Tags, &note.CreatedAt); err != nil {
		t.Fatalf("rows.Scan: %v", err)
	}
	// The primary summary note must reference the last included message.
	if !note.SourceMessageID.Valid || note.SourceMessageID.Int64 != ids[1] {
		t.Fatalf("expected source message id %d, got %+v", ids[1], note.SourceMessageID)
	}
	// Drain any additional typed notes written by structured output.
	for rows.Next() {
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows.Err: %v", err)
	}
	remaining, err := d.GetConsolidationMessages(ctx, "sess", lastID, 0, 10)
	if err != nil {
		t.Fatalf("GetConsolidationMessages: %v", err)
	}
	if len(remaining) != 1 || remaining[0].ID != ids[2] {
		t.Fatalf("expected only third message to remain unconsolidated, got %#v", remaining)
	}
}

func TestConsolidator_RunOnce_FirstOversizeMessageStillAdvancesSafely(t *testing.T) {
	d := openConsolidateTestDB(t)
	ctx := context.Background()
	firstID, err := d.AppendMessage(ctx, "sess", "user", strings.Repeat("x", 200), nil)
	if err != nil {
		t.Fatalf("AppendMessage first: %v", err)
	}
	secondID, err := d.AppendMessage(ctx, "sess", "user", "tail", nil)
	if err != nil {
		t.Fatalf("AppendMessage second: %v", err)
	}
	prov, calls := buildConsolidationProvider(t, `{"summary":"trimmed summary","canonical_memory":"- trimmed"}`, true)
	c := &Consolidator{
		DB:            d,
		Provider:      prov,
		WindowSize:    1,
		MaxMessages:   10,
		MaxInputChars: 20,
	}
	didWork, err := c.RunOnce(ctx, "sess", 1, RunMode{ArchiveAll: true})
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if !didWork {
		t.Fatal("expected work to be done")
	}
	if atomic.LoadInt32(calls.Chat) != 1 {
		t.Fatalf("expected one chat call, got %d", atomic.LoadInt32(calls.Chat))
	}
	lastID, _, err := d.GetConsolidationRange(ctx, "sess", 1)
	if err != nil {
		t.Fatalf("GetConsolidationRange: %v", err)
	}
	if lastID != firstID {
		t.Fatalf("expected cursor to advance through first truncated message %d, got %d", firstID, lastID)
	}
	remaining, err := d.GetConsolidationMessages(ctx, "sess", lastID, 0, 10)
	if err != nil {
		t.Fatalf("GetConsolidationMessages: %v", err)
	}
	if len(remaining) != 1 || remaining[0].ID != secondID {
		t.Fatalf("expected second message to remain unconsolidated, got %#v", remaining)
	}
}

func TestConsolidator_RunOnce_AdaptiveTriggerOnLargeTranscript(t *testing.T) {
	d := openConsolidateTestDB(t)
	ctx := context.Background()
	for _, content := range []string{
		strings.Repeat("a", 30),
		strings.Repeat("b", 30),
		"active",
	} {
		if _, err := d.AppendMessage(ctx, "sess", "user", content, nil); err != nil {
			t.Fatalf("AppendMessage: %v", err)
		}
	}
	prov, calls := buildConsolidationProvider(t, `{"summary":"adaptive summary","canonical_memory":"- adaptive"}`, true)
	c := &Consolidator{
		DB:            d,
		Provider:      prov,
		WindowSize:    5,
		MaxMessages:   10,
		MaxInputChars: 80,
	}
	didWork, err := c.RunOnce(ctx, "sess", 1, RunMode{})
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if !didWork {
		t.Fatal("expected adaptive trigger to consolidate")
	}
	if atomic.LoadInt32(calls.Chat) != 1 {
		t.Fatalf("expected one chat call, got %d", atomic.LoadInt32(calls.Chat))
	}
	lastID, _, err := d.GetConsolidationRange(ctx, "sess", 1)
	if err != nil {
		t.Fatalf("GetConsolidationRange: %v", err)
	}
	remaining, err := d.GetConsolidationMessages(ctx, "sess", lastID, 0, 10)
	if err != nil {
		t.Fatalf("GetConsolidationMessages: %v", err)
	}
	if len(remaining) != 1 || remaining[0].Content != "active" {
		t.Fatalf("expected only active-window message to remain, got %#v", remaining)
	}
}

func TestContentToStr_Other(t *testing.T) {
	got := contentToStr(42)
	if !strings.Contains(got, "42") {
		t.Errorf("expected '42' in output, got %q", got)
	}
}

// ---- parseConsolidationOutput tests ----

func TestParseConsolidationOutput_NewFormat(t *testing.T) {
raw := `{"summary":"session summary","facts":["golang one two two"],"preferences":["dark mode"],"goals":["ship v2"],"procedures":["run make test"]}`
out := parseConsolidationOutput(raw)
if out.Summary != "session summary" {
t.Errorf("expected summary, got %q", out.Summary)
}
if len(out.Facts) != 1 || out.Facts[0] != "golang one two two" {
t.Errorf("expected facts=[golang one two two], got %v", out.Facts)
}
if len(out.Preferences) != 1 || out.Preferences[0] != "dark mode" {
t.Errorf("expected preferences=[dark mode], got %v", out.Preferences)
}
if len(out.Goals) != 1 || out.Goals[0] != "ship v2" {
t.Errorf("expected goals=[ship v2], got %v", out.Goals)
}
if len(out.Procedures) != 1 || out.Procedures[0] != "run make test" {
t.Errorf("expected procedures=[run make test], got %v", out.Procedures)
}
}

func TestParseConsolidationOutput_LegacyFormatFallback(t *testing.T) {
// Old-format responses (only summary + canonical_memory) should be absorbed.
raw := `{"summary":"old summary","canonical_memory":"- prefers concise output"}`
out := parseConsolidationOutput(raw)
if out.Summary != "old summary" {
t.Errorf("expected old summary, got %q", out.Summary)
}
// canonical_memory should become a preference.
if len(out.Preferences) == 0 {
t.Fatal("expected canonical_memory absorbed as preference")
}
if !strings.Contains(out.Preferences[0], "concise output") {
t.Errorf("expected preference to contain 'concise output', got %v", out.Preferences)
}
}

func TestParseConsolidationOutput_EmptyInput(t *testing.T) {
out := parseConsolidationOutput("")
if out.Summary != "" || len(out.Facts)+len(out.Preferences)+len(out.Goals)+len(out.Procedures) != 0 {
t.Errorf("expected empty output for empty input, got %+v", out)
}
}

func TestParseConsolidationOutput_MalformedJSON(t *testing.T) {
out := parseConsolidationOutput("not json at all")
// Fallback: raw text used as summary.
if out.Summary == "" {
t.Error("expected fallback summary from malformed input")
}
}

func TestParseConsolidationOutput_ExtraProseAroundJSON(t *testing.T) {
raw := `Here is the output: {"summary":"foo","preferences":["pref1"]} hope that helps`
out := parseConsolidationOutput(raw)
if out.Summary != "foo" {
t.Errorf("expected 'foo', got %q", out.Summary)
}
if len(out.Preferences) == 0 || out.Preferences[0] != "pref1" {
t.Errorf("expected preferences=[pref1], got %v", out.Preferences)
}
}

func TestParseConsolidationOutput_ItemLengthCapped(t *testing.T) {
longItem := strings.Repeat("x", maxConsolidationItemLen+100)
raw := `{"summary":"s","facts":["` + longItem + `"],"preferences":[],"goals":[],"procedures":[]}`
out := parseConsolidationOutput(raw)
if len(out.Facts) == 0 {
t.Fatal("expected truncated fact")
}
if len(out.Facts[0]) > maxConsolidationItemLen+3 { // +3 for "…"
t.Errorf("expected fact capped to ~%d chars, got %d", maxConsolidationItemLen, len(out.Facts[0]))
}
}

func TestParseConsolidationOutput_ItemCountCapped(t *testing.T) {
items := make([]string, maxConsolidationItems+5)
for i := range items {
items[i] = strings.Repeat("x", 10)
}
b, _ := json.Marshal(map[string]any{
"summary":    "s",
"facts":      items,
"preferences": []string{},
"goals":       []string{},
"procedures":  []string{},
})
out := parseConsolidationOutput(string(b))
if len(out.Facts) > maxConsolidationItems {
t.Errorf("expected facts capped to %d, got %d", maxConsolidationItems, len(out.Facts))
}
}

// ---- buildCanonicalPinnedText tests ----

func TestBuildCanonicalPinnedText_CombinesPrefsAndFacts(t *testing.T) {
text := buildCanonicalPinnedText("- existing", []string{"dark mode"}, []string{"golang one two two"})
if !strings.Contains(text, "existing") {
t.Error("expected existing to be preserved")
}
if !strings.Contains(text, "dark mode") {
t.Error("expected dark mode preference")
}
if !strings.Contains(text, "golang one two two") {
t.Error("expected golang one two two fact")
}
}

func TestBuildCanonicalPinnedText_EmptyInputs(t *testing.T) {
text := buildCanonicalPinnedText("", nil, nil)
if text != "" {
t.Errorf("expected empty output for all-empty inputs, got %q", text)
}
}

// ---- buildExtraNotes tests ----

func TestBuildExtraNotes_AllKinds(t *testing.T) {
parsed := consolidationOutput{
Facts:       []string{"golang one two two"},
Preferences: []string{"dark mode"},
Goals:       []string{"ship v2"},
Procedures:  []string{"run tests"},
}
notes := buildExtraNotes(parsed, sql.NullInt64{Int64: 42, Valid: true})
if len(notes) != 4 {
t.Fatalf("expected 4 notes, got %d", len(notes))
}
kinds := make(map[string]int)
for _, n := range notes {
kinds[n.Kind]++
}
for _, k := range []string{db.MemoryKindFact, db.MemoryKindPreference, db.MemoryKindGoal, db.MemoryKindProcedure} {
if kinds[k] != 1 {
t.Errorf("expected 1 note of kind %q, got %d", k, kinds[k])
}
}
}

func TestBuildExtraNotes_EmptyListsProduceZeroNotes(t *testing.T) {
parsed := consolidationOutput{}
notes := buildExtraNotes(parsed, sql.NullInt64{})
if len(notes) != 0 {
t.Errorf("expected no notes from empty output, got %d", len(notes))
}
}

func TestConsolidator_RunOnce_WritesTypedNotes(t *testing.T) {
d := openConsolidateTestDB(t)
ctx := context.Background()
for i := 0; i < 14; i++ {
role := "user"
if i%2 == 1 {
role = "assistant"
}
if _, err := d.AppendMessage(ctx, "sess", role, "message", nil); err != nil {
t.Fatalf("AppendMessage: %v", err)
}
}
prov, _ := buildConsolidationProvider(t,
`{"summary":"A summary.","facts":["golang one two two is used"],"preferences":["dark mode"],"goals":[],"procedures":[]}`,
false)
c := &Consolidator{
DB:            d,
Provider:      prov,
WindowSize:    5,
MaxMessages:   50,
MaxInputChars: 12000,
EmbedModel:    "",
}
didWork, err := c.RunOnce(ctx, "sess", 5, RunMode{})
if err != nil {
t.Fatalf("RunOnce: %v", err)
}
if !didWork {
t.Fatal("expected consolidation work")
}

// Verify that typed notes were written via FTS.
factRows, err := d.SearchFTS(ctx, "sess", "golang one two two", 5)
if err != nil {
t.Fatalf("SearchFTS facts: %v", err)
}
if len(factRows) == 0 {
t.Fatal("expected fact note via FTS")
}
if factRows[0].Kind != db.MemoryKindFact {
t.Errorf("expected kind=fact, got %q", factRows[0].Kind)
}

prefRows, err := d.SearchFTS(ctx, "sess", "dark mode", 5)
if err != nil {
t.Fatalf("SearchFTS prefs: %v", err)
}
if len(prefRows) == 0 {
t.Fatal("expected preference note via FTS")
}
if prefRows[0].Kind != db.MemoryKindPreference {
t.Errorf("expected kind=preference, got %q", prefRows[0].Kind)
}
}

func TestConsolidator_RunOnce_CanonicalUsesPrefsAndFacts(t *testing.T) {
d := openConsolidateTestDB(t)
ctx := context.Background()
for i := 0; i < 14; i++ {
role := "user"
if i%2 == 1 {
role = "assistant"
}
if _, err := d.AppendMessage(ctx, "sess", role, "hi", nil); err != nil {
t.Fatalf("AppendMessage: %v", err)
}
}
prov, _ := buildConsolidationProvider(t,
`{"summary":"Sess summary.","facts":["uses sqlite database"],"preferences":["prefers minimal diffs"],"goals":[],"procedures":[]}`,
false)
c := &Consolidator{
DB:                 d,
Provider:           prov,
WindowSize:         5,
MaxMessages:        50,
MaxInputChars:      12000,
CanonicalPinnedKey: "long_term_memory",
}
_, err := c.RunOnce(ctx, "sess", 5, RunMode{})
if err != nil {
t.Fatalf("RunOnce: %v", err)
}
pinned, ok, err := d.GetPinnedValue(ctx, "sess", "long_term_memory")
if err != nil {
t.Fatalf("GetPinnedValue: %v", err)
}
if !ok {
t.Fatal("expected canonical pinned memory to be written")
}
// Pinned memory should contain preferences and facts, not just the summary.
if !strings.Contains(pinned, "minimal diffs") {
t.Errorf("expected preference in pinned memory, got %q", pinned)
}
if !strings.Contains(pinned, "sqlite database") {
t.Errorf("expected fact in pinned memory, got %q", pinned)
}
}
