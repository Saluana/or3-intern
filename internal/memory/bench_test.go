package memory

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

// BenchmarkDocIndexSync measures steady-state doc indexing refresh when a file
// changes between sync passes.
func BenchmarkDocIndexSync(b *testing.B) {
	b.ReportAllocs()
	d := openBenchDB(b)
	ctx := context.Background()
	root := b.TempDir()
	paths := make([]string, 0, 12)
	for i := 0; i < 12; i++ {
		path := filepath.Join(root, fmt.Sprintf("doc-%02d.md", i))
		if err := os.WriteFile(path, []byte(fmt.Sprintf("# Doc %d\nseed\n", i)), 0o644); err != nil {
			b.Fatalf("WriteFile: %v", err)
		}
		paths = append(paths, path)
	}
	indexer := &DocIndexer{
		DB: d,
		Config: DocIndexConfig{
			Roots:         []string{root},
			MaxFiles:      32,
			MaxChunks:     32,
			MaxFileBytes:  8 * 1024,
			EmbedMaxBytes: 0,
		},
	}
	if err := indexer.SyncRoots(ctx, "bench-scope"); err != nil {
		b.Fatalf("initial SyncRoots: %v", err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		path := paths[i%len(paths)]
		modTime := time.Unix(0, int64(i+1)*int64(time.Millisecond))
		b.StopTimer()
		body := []byte(fmt.Sprintf("# Doc %d\niteration %d\n%s\n", i%len(paths), i, strings.Repeat("x", (i%5)+1)))
		if err := os.WriteFile(path, body, 0o644); err != nil {
			b.Fatalf("WriteFile: %v", err)
		}
		if err := os.Chtimes(path, modTime, modTime); err != nil {
			b.Fatalf("Chtimes: %v", err)
		}
		b.StartTimer()
		if err := indexer.SyncRoots(ctx, "bench-scope"); err != nil {
			b.Fatalf("SyncRoots: %v", err)
		}
	}
}
