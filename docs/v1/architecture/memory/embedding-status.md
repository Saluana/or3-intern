# Embedding Status

The embedding status tells you whether the vector index is configured and what embedding model is currently in use.

Sources: `internal/db/db.go`, `internal/memory/retrieve.go`

## Checking Dimensionality

`MemoryVectorDims()` (`internal/db/db.go:716-726`) queries the `memory_vec_meta` table:

```sql
SELECT dims FROM memory_vec_meta WHERE id=1
```

Returns `0` if no index has been configured yet. A positive value means the index exists with that many dimensions per vector.

## Checking the Fingerprint

`MemoryVectorFingerprint()` (`internal/db/db.go:730-740`) queries the same table:

```sql
SELECT embed_fingerprint FROM memory_vec_meta WHERE id=1
```

Returns an empty string if no fingerprint was stored. The fingerprint identifies which embedding model (and potentially which provider configuration) generated the vectors. When the retriever's `EmbedFingerprint` doesn't match the stored fingerprint, vector search is skipped (`internal/memory/retrieve.go:70-86`).

## How the Index Is Initialized

The vector index is created lazily:

1. **On migration** — `ensureMemoryVecIndexForExisting()` (`db.go:694-713`) checks if the `memory_vec_meta` table has dims. If not, it looks for the first embedding in `memory_notes` to infer the dimension count. If found, it initializes the index.

2. **On first write** — `validateMemoryEmbeddingProfile()` (`store.go:399-427`) auto-creates the index if `dims == 0` and a valid embedding is being written.

3. **Explicitly** — `EnsureMemoryVecIndexWithDim()` or `EnsureMemoryVecIndexWithProfile()` can be called to set up the index proactively.

## Detecting Mismatches

The retriever checks the fingerprint at search time (`retrieve.go:70-86`). If:
- `EmbedFingerprint` is empty → vector search runs unconditionally
- `EmbedFingerprint` is set but doesn't match the stored fingerprint → vector search is skipped (no error, just no vector results)

On write, `validateMemoryEmbeddingProfile()` rejects embeddings that don't match the stored dimensionality or fingerprint. The consolidation flow catches this error and triggers an automatic rebuild.

## Related Functions

| Function | File | Purpose |
|----------|------|---------|
| `MemoryVectorDims()` | `db/db.go:716` | Get configured dimensions |
| `MemoryVectorFingerprint()` | `db/db.go:730` | Get configured fingerprint |
| `firstMemoryVectorDim()` | `db/db.go:742` | Infer dims from existing embeddings |
| `ensureMemoryVecIndexForExisting()` | `db/db.go:694` | Auto-init the index on migration |
| `validateMemoryEmbeddingProfile()` | `db/store.go:399` | Validate before write |
