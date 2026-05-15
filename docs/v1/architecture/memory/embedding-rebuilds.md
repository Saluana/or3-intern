# Embedding Rebuilds

When the embedding model changes (different provider, different model, etc.), the vector index must be rebuilt to match the new dimensionality and fingerprint. OR3 Intern supports both automatic and manual rebuilds.

Sources: `internal/db/db.go` (vector index functions), `internal/db/store.go`

## When Rebuilds Happen

### Automatic (on write failure)

During consolidation, `writeConsolidatedTranscript()` catches a dimension/fingerprint mismatch error and triggers a rebuild automatically (`internal/memory/consolidate.go:420-427`):

```go
if err != nil && len(embedding) >= 4 && isMemoryVectorProfileMismatchError(err) {
    wantDims := len(embedding) / 4
    if rebuildErr := c.DB.RebuildMemoryVecIndexWithProfile(ctx, wantDims, c.EmbedFingerprint); rebuildErr != nil {
        return fmt.Errorf("consolidation write: %w (rebuild failed: %v)", err, rebuildErr)
    }
    _, err = c.DB.WriteConsolidation(ctx, w) // retry
}
```

### Automatic (on migration)

`ensureMemoryVecIndexForExisting()` (`db.go:694-713`) runs during DB migration. If `memory_vec_meta.dims` is 0, it tries to infer dimensions from existing embeddings and creates the index.

### Manual

`RebuildMemoryVecIndexWithDim()` (`db.go:785-787`) and `RebuildMemoryVecIndexWithProfile()` (`db.go:792-794`) are public functions that force a rebuild.

## The Rebuild Process

`initMemoryVecIndex()` (`db.go:796-859`) handles the actual rebuild:

1. **Checks compatibility** — If `force=false`, verifies that the new dims and fingerprint match existing metadata. If they don't match, returns an error (preventing accidental overwrites unless force=true).

2. **Drops the old index** — `DROP TABLE IF EXISTS memory_vec`.

3. **Creates a new index** — Creates a new `memory_vec` virtual table with the new dimensions:

   ```sql
   CREATE VIRTUAL TABLE memory_vec USING vec0(
       note_id integer primary key,
       session_key text partition key,
       embedding float[<new_dims>] distance_metric=cosine,
       +text text
   )
   ```

4. **Repopulates** — Copies rows from `memory_notes` that match the new dimensions (and fingerprint, if specified):

   ```sql
   INSERT INTO memory_vec(note_id, session_key, embedding, text)
   SELECT id, session_key, embedding, text
   FROM memory_notes
   WHERE typeof(embedding)='blob' AND length(embedding)=?
       AND embed_fingerprint=?
   ```

5. **Updates metadata** — Upserts into `memory_vec_meta` with the new dims, fingerprint, and timestamp.

## Re-embedding Notes

For bulk re-embedding (changing the actual embedding values, not just the index), `ListMemoryNotesForReembed()` (`store.go:882-898`) returns all note IDs and their text. The caller generates new embeddings, then `ReplaceMemoryNoteEmbedding()` (`store.go:903-909`) updates each note's embedding blob and fingerprint. After bulk re-embedding, the caller should rebuild the vector index.

## Mismatch Detection

Two functions detect when a rebuild is needed:

- `isMemoryVectorProfileMismatchError()` (`consolidate.go:489-495`) — Checks error strings for "memory vector dims mismatch" or "memory embedding fingerprint mismatch".

- `providerRejectedToolChoice()` (`consolidate.go:475-487`) — Unrelated to embeddings, but handles the case where the LLM provider doesn't support `tool_choice: "required"`.

## Validation on Write

Before writing any embedding, `validateMemoryEmbeddingProfile()` (`store.go:399-427`) checks:
1. If no index exists, creates one with the new embedding's dimensions
2. If dimensions don't match, returns an error
3. If fingerprint is set and doesn't match, returns an error
