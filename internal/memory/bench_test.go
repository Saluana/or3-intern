package memory

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"

	"or3-intern/internal/db"
)

func openBenchDB(b *testing.B) *db.DB {
	b.Helper()
	dir := b.TempDir()
	d, err := db.Open(filepath.Join(dir, "bench.db"))
	if err != nil {
		b.Fatalf("db.Open: %v", err)
	}
	b.Cleanup(func() { d.Close() })
	return d
}

// BenchmarkHybridRetrieval measures hybrid memory lookup combining vector and FTS scores.
func BenchmarkHybridRetrieval(b *testing.B) {
	b.ReportAllocs()
	d := openBenchDB(b)
	ctx := context.Background()
	r := NewRetriever(d)

	embedding := make([]byte, 4*8)
	queryVec := make([]float32, 8)
	for i := 0; i < 30; i++ {
		if _, err := d.InsertMemoryNote(ctx, "bench-session",
			fmt.Sprintf("memory note %d about retrieval benchmarking", i),
			embedding, sql.NullInt64{}, "bench"); err != nil {
			b.Fatalf("InsertMemoryNote: %v", err)
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := r.Retrieve(ctx, "bench-session", "retrieval benchmarking", queryVec, 10, 10, 5); err != nil {
			b.Fatalf("Retrieve: %v", err)
		}
	}
}

// BenchmarkFTSRetrieval measures full-text-search memory lookup via BM25.
func BenchmarkFTSRetrieval(b *testing.B) {
	b.ReportAllocs()
	d := openBenchDB(b)
	ctx := context.Background()

	embedding := make([]byte, 4*8)
	for i := 0; i < 30; i++ {
		if _, err := d.InsertMemoryNote(ctx, "bench-session",
			fmt.Sprintf("note %d about full text search benchmarking", i),
			embedding, sql.NullInt64{}, "bench"); err != nil {
			b.Fatalf("InsertMemoryNote: %v", err)
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := d.SearchFTS(ctx, "bench-session", "full text search", 10); err != nil {
			b.Fatalf("SearchFTS: %v", err)
		}
	}
}

// BenchmarkNoteStorage measures note insert throughput.
func BenchmarkNoteStorage(b *testing.B) {
	b.ReportAllocs()
	d := openBenchDB(b)
	ctx := context.Background()

	embedding := make([]byte, 4*8)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := d.InsertMemoryNote(ctx, "bench-session",
			fmt.Sprintf("benchmark note %d", i),
			embedding, sql.NullInt64{}, "bench"); err != nil {
			b.Fatalf("InsertMemoryNote: %v", err)
		}
	}
}
