package memory

import (
	"context"
	"database/sql"
	"testing"
)

func TestVectorSearch_ScanLimitCapsK(t *testing.T) {
	d := openRetrieveTestDB(t)
	ctx := context.Background()
	emb := PackFloat32([]float32{1, 0})
	for i := 0; i < 5; i++ {
		if _, err := d.InsertMemoryNote(ctx, "sess", "note", emb, sqlNullInt64(), ""); err != nil {
			t.Fatalf("InsertMemoryNote: %v", err)
		}
	}
	got, err := VectorSearch(ctx, d, "sess", []float32{1, 0}, 10, 2)
	if err != nil {
		t.Fatalf("VectorSearch: %v", err)
	}
	if len(got) > 2 {
		t.Fatalf("expected scanLimit to cap k=2, got %d results", len(got))
	}
}

func sqlNullInt64() sql.NullInt64 {
	return sql.NullInt64{}
}
