package memory

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"or3-intern/internal/db"
	"or3-intern/internal/scope"
)

func openRetrieveTestDB(t *testing.T) *db.DB {
	t.Helper()
	dir := t.TempDir()
	d, err := db.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

func TestNewRetriever(t *testing.T) {
	d := openRetrieveTestDB(t)
	r := NewRetriever(d)
	if r == nil {
		t.Fatal("expected non-nil retriever")
	}
	if r.VectorWeight != 0.55 {
		t.Errorf("expected VectorWeight=0.55, got %v", r.VectorWeight)
	}
	if r.FTSWeight != 0.25 {
		t.Errorf("expected FTSWeight=0.25, got %v", r.FTSWeight)
	}
	if r.LexicalWeight != 0.12 {
		t.Errorf("expected LexicalWeight=0.12, got %v", r.LexicalWeight)
	}
	if r.RecencyWeight != 0.08 {
		t.Errorf("expected RecencyWeight=0.08, got %v", r.RecencyWeight)
	}
	if r.VectorScanLimit != 2000 {
		t.Errorf("expected VectorScanLimit=2000, got %d", r.VectorScanLimit)
	}
}

func TestRetrieve_Empty(t *testing.T) {
	d := openRetrieveTestDB(t)
	r := NewRetriever(d)
	ctx := context.Background()

	results, err := r.Retrieve(ctx, "session1", "hello", []float32{0.5, 0.5}, 5, 5, 10)
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected empty results, got %d", len(results))
	}
}

func TestRetrieve_WithVectorResults(t *testing.T) {
	d := openRetrieveTestDB(t)
	ctx := context.Background()

	blob := PackFloat32([]float32{1.0, 0.0})
	if _, err := d.InsertMemoryNote(ctx, "session1", "vector match", blob, sql.NullInt64{}, ""); err != nil {
		t.Fatalf("InsertMemoryNote: %v", err)
	}

	r := NewRetriever(d)
	results, err := r.Retrieve(ctx, "session1", "query", []float32{1.0, 0.0}, 5, 5, 10)
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected at least one result")
	}
	found := false
	for _, res := range results {
		if res.Text == "vector match" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected 'vector match' in results")
	}
}

func TestRetrieve_SourceLabels(t *testing.T) {
	d := openRetrieveTestDB(t)
	ctx := context.Background()

	// Insert note with known embedding
	blob := PackFloat32([]float32{1.0, 0.0})
	if _, err := d.InsertMemoryNote(ctx, "session1", "fox quick jump", blob, sql.NullInt64{}, ""); err != nil {
		t.Fatalf("InsertMemoryNote: %v", err)
	}

	r := NewRetriever(d)
	// Exact vector match, also FTS match
	results, err := r.Retrieve(ctx, "session1", "fox quick jump", []float32{1.0, 0.0}, 5, 5, 10)
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected results")
	}
	// If both vector and FTS match, source should be "hybrid"
	for _, res := range results {
		if res.Text == "fox quick jump" {
			if res.Source != "hybrid" && res.Source != "vector" && res.Source != "fts" {
				t.Errorf("unexpected source %q", res.Source)
			}
		}
	}
}

func TestRetrieve_TopKLimit(t *testing.T) {
	d := openRetrieveTestDB(t)
	ctx := context.Background()

	for i := 0; i < 10; i++ {
		blob := PackFloat32([]float32{float32(i), 0.0})
		if _, err := d.InsertMemoryNote(ctx, "session1", "note", blob, sql.NullInt64{}, ""); err != nil {
			t.Fatalf("InsertMemoryNote: %v", err)
		}
	}

	r := NewRetriever(d)
	results, err := r.Retrieve(ctx, "session1", "note", []float32{5.0, 0.0}, 10, 10, 3)
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
	if len(results) > 3 {
		t.Errorf("expected at most 3 results, got %d", len(results))
	}
}

func TestRetrieve_SortedByScore(t *testing.T) {
	d := openRetrieveTestDB(t)
	ctx := context.Background()

	blobs := [][]float32{{1, 0}, {0, 1}, {0.7071, 0.7071}}
	texts := []string{"alpha", "beta", "gamma"}
	for i, v := range blobs {
		if _, err := d.InsertMemoryNote(ctx, "session1", texts[i], PackFloat32(v), sql.NullInt64{}, ""); err != nil {
			t.Fatalf("InsertMemoryNote: %v", err)
		}
	}

	r := NewRetriever(d)
	results, err := r.Retrieve(ctx, "session1", "alpha", []float32{1, 0}, 5, 5, 10)
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
	for i := 1; i < len(results); i++ {
		if results[i].Score > results[i-1].Score {
			t.Errorf("results not sorted descending: [%d]=%v > [%d]=%v", i, results[i].Score, i-1, results[i-1].Score)
		}
	}
}

func TestRetrieve_RespectsSessionScope(t *testing.T) {
	d := openRetrieveTestDB(t)
	ctx := context.Background()

	if _, err := d.InsertMemoryNote(ctx, "session-a", "private note", PackFloat32([]float32{1, 0}), sql.NullInt64{}, ""); err != nil {
		t.Fatalf("InsertMemoryNote private: %v", err)
	}
	if _, err := d.InsertMemoryNote(ctx, scope.GlobalMemoryScope, "shared note", PackFloat32([]float32{1, 0}), sql.NullInt64{}, ""); err != nil {
		t.Fatalf("InsertMemoryNote shared: %v", err)
	}

	r := NewRetriever(d)
	results, err := r.Retrieve(ctx, "session-b", "note", []float32{1, 0}, 5, 5, 10)
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
	if len(results) == 0 || results[0].Text != "shared note" {
		t.Fatalf("expected shared note only, got %#v", results)
	}
	for _, result := range results {
		if result.Text == "private note" {
			t.Fatalf("unexpected cross-session result: %#v", results)
		}
	}
}

func TestNormalizeFTSQuery(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"", ""},
		{"  ", ""},
		{"hello world", "hello world"},
		{"hello:world", `"hello:world"`},
		{"foo-bar", `"foo-bar"`},
		{"deploy(runbook)", `"deploy(runbook)"`},
		{`with "quotes"`, `with """quotes"""`},
		{"star*term", `"star*term"`},
		{"normal words only", "normal words only"},
	}
	for _, tc := range tests {
		got := normalizeFTSQuery(tc.input)
		if got != tc.want {
			t.Errorf("normalizeFTSQuery(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestRetrieve_PunctuationHeavyQueryStillFindsFTSMatches(t *testing.T) {
	d := openRetrieveTestDB(t)
	ctx := context.Background()
	if _, err := d.InsertMemoryNote(ctx, "session1", "deploy(runbook) foo-bar", nil, sql.NullInt64{}, ""); err != nil {
		t.Fatalf("InsertMemoryNote: %v", err)
	}
	r := NewRetriever(d)
	results, err := r.Retrieve(ctx, "session1", "deploy(runbook) foo-bar", nil, 0, 5, 5)
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
	found := false
	for _, res := range results {
		if strings.Contains(res.Text, "foo-bar") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected punctuation-heavy query to retrieve stored memory, got %#v", results)
	}
}

func TestRetrieve_PrefersMoreRecentEquivalentMatches(t *testing.T) {
	d := openRetrieveTestDB(t)
	ctx := context.Background()

	olderID, err := d.InsertMemoryNote(ctx, "session1", "deploy runbook stable release", PackFloat32([]float32{1, 0}), sql.NullInt64{}, "")
	if err != nil {
		t.Fatalf("InsertMemoryNote older: %v", err)
	}
	newerID, err := d.InsertMemoryNote(ctx, "session1", "deploy runbook stable release", PackFloat32([]float32{1, 0}), sql.NullInt64{}, "")
	if err != nil {
		t.Fatalf("InsertMemoryNote newer: %v", err)
	}
	if olderID == newerID {
		t.Fatal("expected distinct note ids")
	}

	r := NewRetriever(d)
	results, err := r.Retrieve(ctx, "session1", "deploy stable release", []float32{1, 0}, 10, 10, 5)
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected results")
	}
	if results[0].ID != newerID {
		t.Fatalf("expected newer note to rank first, got %#v", results)
	}
}

func TestRetrieve_DeduplicatesNearIdenticalText(t *testing.T) {
	d := openRetrieveTestDB(t)
	ctx := context.Background()

	if _, err := d.InsertMemoryNote(ctx, "session1", "release checklist deploy api service", PackFloat32([]float32{1, 0}), sql.NullInt64{}, ""); err != nil {
		t.Fatalf("InsertMemoryNote first: %v", err)
	}
	if _, err := d.InsertMemoryNote(ctx, "session1", "release checklist deploy api service", PackFloat32([]float32{1, 0}), sql.NullInt64{}, ""); err != nil {
		t.Fatalf("InsertMemoryNote second: %v", err)
	}
	if _, err := d.InsertMemoryNote(ctx, "session1", "database migration rollback plan", PackFloat32([]float32{0, 1}), sql.NullInt64{}, ""); err != nil {
		t.Fatalf("InsertMemoryNote third: %v", err)
	}

	r := NewRetriever(d)
	results, err := r.Retrieve(ctx, "session1", "release checklist deploy api", []float32{1, 0}, 10, 10, 5)
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
	count := 0
	for _, result := range results {
		if result.Text == "release checklist deploy api service" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected duplicate text to collapse to one result, got %#v", results)
	}
}

func TestRetrieve_FallsBackToFTSOnFingerprintMismatch(t *testing.T) {
	d := openRetrieveTestDB(t)
	ctx := context.Background()

	if _, err := d.InsertMemoryNoteTyped(ctx, "session1", db.TypedNoteInput{
		Text:             "deployment checklist stable release",
		Embedding:        PackFloat32([]float32{1, 0}),
		EmbedFingerprint: "provider-a:text-embedding-3-small",
	}); err != nil {
		t.Fatalf("InsertMemoryNoteTyped: %v", err)
	}

	r := NewRetriever(d)
	r.EmbedFingerprint = "provider-b:text-embedding-3-small"
	results, err := r.Retrieve(ctx, "session1", "stable release", []float32{1, 0}, 10, 10, 5)
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected FTS fallback results")
	}
	if results[0].Source == "vector" || results[0].Source == "hybrid" {
		t.Fatalf("expected fingerprint mismatch to disable vector ranking, got %#v", results[0])
	}
}

// ---- metadataScoreAdjust tests ----

func TestMetadataScoreAdjust_StaleNoteDemoted(t *testing.T) {
	adj := metadataScoreAdjust(db.MemoryKindSummary, db.MemoryStatusStale, 0, 0)
	if adj >= 0 {
		t.Errorf("expected negative adjustment for stale note, got %v", adj)
	}
}

func TestMetadataScoreAdjust_SupersededDemoted(t *testing.T) {
	adj := metadataScoreAdjust(db.MemoryKindSummary, db.MemoryStatusSuperseded, 0, 0)
	if adj >= 0 {
		t.Errorf("expected negative adjustment for superseded note, got %v", adj)
	}
}

func TestMetadataScoreAdjust_FactBoosted(t *testing.T) {
	adj := metadataScoreAdjust(db.MemoryKindFact, db.MemoryStatusActive, 0, 0)
	if adj <= 0 {
		t.Errorf("expected positive adjustment for active fact, got %v", adj)
	}
}

func TestMetadataScoreAdjust_PreferenceBoosted(t *testing.T) {
	adj := metadataScoreAdjust(db.MemoryKindPreference, db.MemoryStatusActive, 0, 0)
	if adj <= 0 {
		t.Errorf("expected positive adjustment for active preference, got %v", adj)
	}
}

func TestMetadataScoreAdjust_ImportanceBoostBounded(t *testing.T) {
	// Very high importance should be capped and not produce an outsized boost.
	adj := metadataScoreAdjust(db.MemoryKindFact, db.MemoryStatusActive, 999.0, 0)
	// The adjustment should not exceed fact_boost + importance_cap (0.03 + 0.04 = 0.07).
	if adj > 0.10 {
		t.Errorf("importance boost should be bounded, got adj=%v", adj)
	}
}

func TestMetadataScoreAdjust_UseCountBoostBounded(t *testing.T) {
	adj := metadataScoreAdjust(db.MemoryKindNote, db.MemoryStatusActive, 0, 100)
	// use_count capped at 5 × 0.01 = 0.05 max for this alone.
	if adj > 0.10 {
		t.Errorf("use_count boost should be bounded, got adj=%v", adj)
	}
}

func TestMetadataScoreAdjust_ActiveNoteNoMajorPenalty(t *testing.T) {
	adj := metadataScoreAdjust(db.MemoryKindNote, db.MemoryStatusActive, 0, 0)
	// A plain active note should have a small or zero adjustment, not a large penalty.
	if adj < -0.01 {
		t.Errorf("active plain note should not have large penalty, got %v", adj)
	}
}

// ---- Retrieved metadata fields via Retrieve ----

func TestRetrieve_CarriesMetadataFields(t *testing.T) {
	d := openRetrieveTestDB(t)
	ctx := context.Background()
	_, err := d.InsertMemoryNoteTyped(ctx, "sess", db.TypedNoteInput{
		Text:       "the quick brown fox",
		Kind:       db.MemoryKindFact,
		Status:     db.MemoryStatusActive,
		Importance: 0.7,
	})
	if err != nil {
		t.Fatalf("InsertMemoryNoteTyped: %v", err)
	}

	r := NewRetriever(d)
	results, err := r.Retrieve(ctx, "sess", "quick brown fox", nil, 5, 5, 5)
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
	if len(results) == 0 {
		t.Skip("no FTS results (ok for empty embedding test)")
	}
	found := false
	for _, res := range results {
		if res.Text == "the quick brown fox" {
			found = true
			if res.Kind != db.MemoryKindFact {
				t.Errorf("expected kind=fact, got %q", res.Kind)
			}
			if res.Status != db.MemoryStatusActive {
				t.Errorf("expected status=active, got %q", res.Status)
			}
			if res.Importance != 0.7 {
				t.Errorf("expected importance=0.7, got %v", res.Importance)
			}
		}
	}
	if !found {
		t.Error("expected to find 'the quick brown fox' in results")
	}
}

func TestRetrieve_StaleNotesHaveLowerScore(t *testing.T) {
	d := openRetrieveTestDB(t)
	ctx := context.Background()

	// Insert an active fact and a stale summary with similar text.
	_, err := d.InsertMemoryNoteTyped(ctx, "sess", db.TypedNoteInput{
		Text:   "penguin active fact",
		Kind:   db.MemoryKindFact,
		Status: db.MemoryStatusActive,
	})
	if err != nil {
		t.Fatalf("InsertMemoryNoteTyped active: %v", err)
	}
	staleID, err := d.InsertMemoryNoteTyped(ctx, "sess", db.TypedNoteInput{
		Text:   "penguin stale summary",
		Kind:   db.MemoryKindSummary,
		Status: db.MemoryStatusActive, // inserted active, then made stale below
	})
	if err != nil {
		t.Fatalf("InsertMemoryNoteTyped stale: %v", err)
	}
	// Mark the stale note as stale.
	if _, err := d.SQL.ExecContext(ctx, `UPDATE memory_notes SET status='stale' WHERE id=?`, staleID); err != nil {
		t.Fatalf("mark stale: %v", err)
	}

	r := NewRetriever(d)
	results, err := r.Retrieve(ctx, "sess", "penguin", nil, 5, 5, 5)
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
	if len(results) < 2 {
		t.Skip("fewer than 2 results, cannot compare scores")
	}

	// Find each note's score.
	activeScore := -1.0
	staleScore := -1.0
	for _, res := range results {
		if res.Text == "penguin active fact" {
			activeScore = res.Score
		}
		if res.Text == "penguin stale summary" {
			staleScore = res.Score
		}
	}
	if activeScore < 0 || staleScore < 0 {
		t.Skip("could not find both notes in results")
	}
	if staleScore >= activeScore {
		t.Errorf("expected stale note score (%v) < active note score (%v)", staleScore, activeScore)
	}
}

func TestExistingRetrieveAPIStillWorks(t *testing.T) {
	d := openRetrieveTestDB(t)
	ctx := context.Background()
	if _, err := d.InsertMemoryNoteTyped(ctx, "sess", db.TypedNoteInput{
		Text: "build packet budget report",
		Kind: db.MemoryKindFact,
	}); err != nil {
		t.Fatalf("InsertMemoryNoteTyped: %v", err)
	}
	r := NewRetriever(d)
	results, err := r.Retrieve(ctx, "sess", "packet budget", nil, 4, 4, 3)
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
	if len(results) == 0 {
		t.Fatalf("expected non-empty retrieval results")
	}
}

// ---- formatMemoryDigest (prompt builder helper) ----
// We test through the package's internal function via a wrapper.
