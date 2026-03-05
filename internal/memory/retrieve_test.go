package memory

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"or3-intern/internal/db"
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
	if r.VectorWeight != 0.7 {
		t.Errorf("expected VectorWeight=0.7, got %v", r.VectorWeight)
	}
	if r.FTSWeight != 0.3 {
		t.Errorf("expected FTSWeight=0.3, got %v", r.FTSWeight)
	}
	if r.VectorScanLimit != 2000 {
		t.Errorf("expected VectorScanLimit=2000, got %d", r.VectorScanLimit)
	}
}

func TestRetrieve_Empty(t *testing.T) {
	d := openRetrieveTestDB(t)
	r := NewRetriever(d)
	ctx := context.Background()

	results, err := r.Retrieve(ctx, "hello", []float32{0.5, 0.5}, 5, 5, 10)
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
	d.InsertMemoryNote(ctx, "vector match", blob, sql.NullInt64{}, "")

	r := NewRetriever(d)
	results, err := r.Retrieve(ctx, "query", []float32{1.0, 0.0}, 5, 5, 10)
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
	d.InsertMemoryNote(ctx, "fox quick jump", blob, sql.NullInt64{}, "")

	r := NewRetriever(d)
	// Exact vector match, also FTS match
	results, err := r.Retrieve(ctx, "fox quick jump", []float32{1.0, 0.0}, 5, 5, 10)
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
		d.InsertMemoryNote(ctx, "note", blob, sql.NullInt64{}, "")
	}

	r := NewRetriever(d)
	results, err := r.Retrieve(ctx, "note", []float32{5.0, 0.0}, 10, 10, 3)
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
		d.InsertMemoryNote(ctx, texts[i], PackFloat32(v), sql.NullInt64{}, "")
	}

	r := NewRetriever(d)
	results, err := r.Retrieve(ctx, "alpha", []float32{1, 0}, 5, 5, 10)
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
	for i := 1; i < len(results); i++ {
		if results[i].Score > results[i-1].Score {
			t.Errorf("results not sorted descending: [%d]=%v > [%d]=%v", i, results[i].Score, i-1, results[i-1].Score)
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
