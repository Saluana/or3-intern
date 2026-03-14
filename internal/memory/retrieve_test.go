package memory

import (
	"context"
	"database/sql"
	"path/filepath"
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
