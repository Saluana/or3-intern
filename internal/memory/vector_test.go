package memory

import (
	"context"
	"database/sql"
	"math"
	"path/filepath"
	"testing"

	"or3-intern/internal/db"
	"or3-intern/internal/scope"
)

// ---- PackFloat32 / UnpackFloat32 ----

func TestPackUnpackFloat32_RoundTrip(t *testing.T) {
	orig := []float32{1.0, 2.5, -3.0, 0.0, 1e10}
	packed := PackFloat32(orig)
	if len(packed) != len(orig)*4 {
		t.Errorf("expected %d bytes, got %d", len(orig)*4, len(packed))
	}
	got, err := UnpackFloat32(packed)
	if err != nil {
		t.Fatalf("UnpackFloat32: %v", err)
	}
	if len(got) != len(orig) {
		t.Fatalf("expected %d values, got %d", len(orig), len(got))
	}
	for i := range orig {
		if got[i] != orig[i] {
			t.Errorf("index %d: expected %v, got %v", i, orig[i], got[i])
		}
	}
}

func TestPackFloat32_Empty(t *testing.T) {
	packed := PackFloat32(nil)
	if len(packed) != 0 {
		t.Errorf("expected empty bytes for nil input, got %d bytes", len(packed))
	}
}

func TestUnpackFloat32_Invalid(t *testing.T) {
	_, err := UnpackFloat32([]byte{1, 2, 3}) // 3 bytes is not a multiple of 4
	if err == nil {
		t.Fatal("expected error for invalid blob size")
	}
}

func TestUnpackFloat32_Empty(t *testing.T) {
	got, err := UnpackFloat32([]byte{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty slice, got %d elements", len(got))
	}
}

// ---- Cosine ----

func TestCosine_Identical(t *testing.T) {
	v := []float32{1, 2, 3}
	score := Cosine(v, v)
	if math.Abs(score-1.0) > 1e-6 {
		t.Errorf("expected cosine similarity of identical vectors ≈1.0, got %v", score)
	}
}

func TestCosine_Orthogonal(t *testing.T) {
	a := []float32{1, 0}
	b := []float32{0, 1}
	score := Cosine(a, b)
	if math.Abs(score) > 1e-6 {
		t.Errorf("expected cosine similarity of orthogonal vectors ≈0.0, got %v", score)
	}
}

func TestCosine_Opposite(t *testing.T) {
	a := []float32{1, 0}
	b := []float32{-1, 0}
	score := Cosine(a, b)
	if math.Abs(score+1.0) > 1e-6 {
		t.Errorf("expected cosine similarity of opposite vectors ≈-1.0, got %v", score)
	}
}

func TestCosine_ZeroVectorA(t *testing.T) {
	a := []float32{0, 0}
	b := []float32{1, 2}
	score := Cosine(a, b)
	if score != 0 {
		t.Errorf("expected 0 when a is zero vector, got %v", score)
	}
}

func TestCosine_ZeroVectorB(t *testing.T) {
	a := []float32{1, 2}
	b := []float32{0, 0}
	score := Cosine(a, b)
	if score != 0 {
		t.Errorf("expected 0 when b is zero vector, got %v", score)
	}
}

func TestCosine_DifferentLengths(t *testing.T) {
	a := []float32{1, 0, 0}
	b := []float32{1, 0}
	// should use min length
	score := Cosine(a, b)
	if math.Abs(score-1.0) > 1e-6 {
		t.Errorf("expected ~1.0 for first-element match, got %v", score)
	}
}

// ---- heap ----

func TestCandMinHeap(t *testing.T) {
	h := &candMinHeap{
		{ID: 1, Score: 0.5},
		{ID: 2, Score: 0.9},
		{ID: 3, Score: 0.1},
	}
	if h.Len() != 3 {
		t.Errorf("expected Len 3, got %d", h.Len())
	}
	if !h.Less(2, 0) { // 0.1 < 0.5
		t.Error("expected Less(2,0) to be true")
	}
	h.Swap(0, 1)
	if (*h)[0].ID != 2 || (*h)[1].ID != 1 {
		t.Error("expected Swap to swap elements")
	}
}

// ---- VectorSearch ----

func openTestDB(t *testing.T) *db.DB {
	t.Helper()
	dir := t.TempDir()
	d, err := db.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

func TestVectorSearch_Empty(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	query := []float32{0.5, 0.5}
	results, err := VectorSearch(ctx, d, "session1", query, 5, 100)
	if err != nil {
		t.Fatalf("VectorSearch: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected empty results, got %d", len(results))
	}
}

func TestVectorSearch_Results(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	// Insert notes with embeddings
	vecs := [][]float32{
		{1, 0},
		{0, 1},
		{0.7071, 0.7071},
	}
	for i, v := range vecs {
		blob := PackFloat32(v)
		d.InsertMemoryNote(ctx, "session1", []string{"first", "second", "third"}[i], blob, sql.NullInt64{}, "")
	}

	// Query similar to {1, 0}
	results, err := VectorSearch(ctx, d, "session1", []float32{1, 0}, 3, 100)
	if err != nil {
		t.Fatalf("VectorSearch: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected results")
	}
	// "first" should appear in results (it has cosine=1.0 with {1,0})
	found := false
	for _, r := range results {
		if r.Text == "first" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected 'first' to be in results")
	}
	// VectorSearch returns results sorted ascending (min->max) by score
	// Just verify all results are present
	if len(results) != 3 {
		t.Errorf("expected 3 results, got %d", len(results))
	}
}

func TestVectorSearch_InvalidEmbeddingSkipped(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	// Insert a note with invalid embedding
	d.InsertMemoryNote(ctx, "session1", "bad note", []byte{1, 2, 3}, sql.NullInt64{}, "")
	// Insert a good one
	blob := PackFloat32([]float32{1, 0})
	d.InsertMemoryNote(ctx, "session1", "good note", blob, sql.NullInt64{}, "")

	results, err := VectorSearch(ctx, d, "session1", []float32{1, 0}, 5, 100)
	if err != nil {
		t.Fatalf("VectorSearch: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("expected 1 result (bad note skipped), got %d", len(results))
	}
	if results[0].Text != "good note" {
		t.Errorf("expected 'good note', got %q", results[0].Text)
	}
}

func TestVectorSearch_KLimit(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	// Insert 5 notes
	for i := 0; i < 5; i++ {
		blob := PackFloat32([]float32{float32(i), 0})
		d.InsertMemoryNote(ctx, "session1", "note", blob, sql.NullInt64{}, "")
	}

	results, err := VectorSearch(ctx, d, "session1", []float32{4, 0}, 3, 100)
	if err != nil {
		t.Fatalf("VectorSearch: %v", err)
	}
	if len(results) > 3 {
		t.Errorf("expected at most 3 results, got %d", len(results))
	}
}

func TestVectorSearch_PreservesSessionRowsWhenGlobalCorpusIsNewer(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	if _, err := d.InsertMemoryNote(ctx, "session-a", "session match", PackFloat32([]float32{1, 0}), sql.NullInt64{}, ""); err != nil {
		t.Fatalf("InsertMemoryNote session: %v", err)
	}
	for i := 0; i < 5; i++ {
		if _, err := d.InsertMemoryNote(ctx, scope.GlobalMemoryScope, "shared note", PackFloat32([]float32{0, 1}), sql.NullInt64{}, ""); err != nil {
			t.Fatalf("InsertMemoryNote shared: %v", err)
		}
	}

	results, err := VectorSearch(ctx, d, "session-a", []float32{1, 0}, 3, 2)
	if err != nil {
		t.Fatalf("VectorSearch: %v", err)
	}
	found := false
	for _, result := range results {
		if result.Text == "session match" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected session-scoped note to remain searchable, got %#v", results)
	}
}

func TestVectorSearch_UsesDBOwnedScopedSearch(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	if _, err := d.InsertMemoryNote(ctx, "session-a", "session vector", PackFloat32([]float32{1, 0}), sql.NullInt64{}, ""); err != nil {
		t.Fatalf("InsertMemoryNote session: %v", err)
	}
	if _, err := d.InsertMemoryNote(ctx, scope.GlobalMemoryScope, "shared vector", PackFloat32([]float32{0.9, 0.1}), sql.NullInt64{}, ""); err != nil {
		t.Fatalf("InsertMemoryNote shared: %v", err)
	}

	results, err := VectorSearch(ctx, d, "session-a", []float32{1, 0}, 5, 100)
	if err != nil {
		t.Fatalf("VectorSearch: %v", err)
	}
	if len(results) < 2 {
		t.Fatalf("expected both scoped and shared results, got %#v", results)
	}
}
