package db

import (
	"context"
	"strings"
	"sync/atomic"
)

func isMissingMemoryVecTable(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "no such table") && strings.Contains(msg, "memory_vec")
}

// MemoryEmbeddingHealth summarizes memory notes, vector index, and doc index state.
type MemoryEmbeddingHealth struct {
	NoteCount                int
	EmbeddedNoteCount        int
	VectorRowCount           int
	MissingVectorCount       int
	FingerprintMismatchCount int
	DirtyVectorCount         int
	ActiveDocCount           int
	InactiveDocCount         int
	LastDocSyncAt            int64
}

var lastVectorIndexError atomic.Value

func init() {
	lastVectorIndexError.Store("")
}

// SetLastVectorIndexError records the latest vector index failure for status reporting.
func SetLastVectorIndexError(err error) {
	if err == nil {
		lastVectorIndexError.Store("")
		return
	}
	lastVectorIndexError.Store(err.Error())
}

// LastVectorIndexError returns the latest vector index failure message.
func LastVectorIndexError() string {
	if v, ok := lastVectorIndexError.Load().(string); ok {
		return v
	}
	return ""
}

// CollectMemoryEmbeddingHealth gathers counts used by the embeddings status endpoint.
func (d *DB) CollectMemoryEmbeddingHealth(ctx context.Context, currentFingerprint string) (MemoryEmbeddingHealth, error) {
	out := MemoryEmbeddingHealth{}
	if d == nil || d.SQL == nil {
		return out, nil
	}
	currentFingerprint = strings.TrimSpace(currentFingerprint)
	row := d.SQL.QueryRowContext(ctx, `
		SELECT
			(SELECT COUNT(*) FROM memory_notes),
			(SELECT COUNT(*) FROM memory_notes WHERE typeof(embedding)='blob' AND length(embedding) >= 4 AND (length(embedding) % 4)=0),
			(SELECT COUNT(*) FROM memory_notes WHERE vector_index_dirty=1),
			(SELECT COUNT(*) FROM memory_notes
				WHERE typeof(embedding)='blob' AND length(embedding) >= 4 AND (length(embedding) % 4)=0
				AND embed_fingerprint != '' AND embed_fingerprint != ?),
			(SELECT COUNT(*) FROM memory_docs WHERE active=1),
			(SELECT COUNT(*) FROM memory_docs WHERE active=0),
			(SELECT COALESCE(MAX(updated_at), 0) FROM memory_docs WHERE active=1)`,
		currentFingerprint)
	if err := row.Scan(
		&out.NoteCount,
		&out.EmbeddedNoteCount,
		&out.DirtyVectorCount,
		&out.FingerprintMismatchCount,
		&out.ActiveDocCount,
		&out.InactiveDocCount,
		&out.LastDocSyncAt,
	); err != nil {
		return out, err
	}
	if d.VecSQL != nil {
		if err := d.VecSQL.QueryRowContext(ctx, `SELECT COUNT(*) FROM memory_vec`).Scan(&out.VectorRowCount); err != nil {
			if !isMissingMemoryVecTable(err) {
				return out, err
			}
			out.VectorRowCount = 0
		}
		if out.EmbeddedNoteCount > out.VectorRowCount {
			out.MissingVectorCount = out.EmbeddedNoteCount - out.VectorRowCount
		}
	} else if out.EmbeddedNoteCount > 0 {
		out.MissingVectorCount = out.EmbeddedNoteCount
	}
	return out, nil
}
