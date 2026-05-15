# Vector Index

OR3 Intern uses sqlite-vec for vector similarity search on memory notes. Embeddings are stored as float32 blobs and indexed in a virtual table for fast cosine-distance lookups.

Sources: `internal/memory/vector.go`, `internal/db/db.go` (vector index functions), `internal/db/store.go`

## Embedding Storage

Embeddings are stored in the `memory_notes.embedding` column as binary blobs. Each float32 value is packed in little-endian format:

```go
// internal/memory/vector.go:13-17
func PackFloat32(vec []float32) []byte {
    var b bytes.Buffer
    _ = binary.Write(&b, binary.LittleEndian, vec)
    return b.Bytes()
}
```

To unpack:

```go
// internal/memory/vector.go:19-28
func UnpackFloat32(blob []byte) ([]float32, error) {
    // checks len(blob) % 4 == 0, then reads LittleEndian
}
```

## The Vector Index: memory_vec

The vector index lives in a `memory_vec` virtual table using sqlite-vec's `vec0` extension (`internal/db/db.go:824-829`):

```sql
CREATE VIRTUAL TABLE memory_vec USING vec0(
    note_id integer primary key,
    session_key text partition key,
    embedding float[<dims>] distance_metric=cosine,
    +text text
)
```

Key properties:
- **Partition key** — `session_key` allows scoping searches to a specific session or the global scope
- **Distance metric** — Cosine distance
- **Auxiliary column** — `+text text` stores the note text alongside the vector for direct retrieval

## Vector Search

`SearchMemoryVectors()` (`internal/db/store.go:540-579`) searches across both the global scope and session-specific scope:

1. Tries `SearchVecScope()` which queries `memory_vec` using `MATCH` and `k` parameters (`store.go:581-610`)
2. Falls back to `SearchVecScopeFallback()` which computes cosine distance directly on `memory_notes` rows (`store.go:612-632`)
3. Deduplicates results across scopes
4. Sorts by distance ascending, caps at `k` results

## Adding Vectors to the Index

When a memory note is inserted via `InsertMemoryNoteTyped()` (`store.go:347-359`), the embedding is also upserted into `memory_vec`:

```go
INSERT OR REPLACE INTO memory_vec(note_id, session_key, embedding, text)
VALUES(?, ?, ?, ?)
```

## Profile Validation

`validateMemoryEmbeddingProfile()` (`store.go:399-427`) checks that new embeddings match the index's configured dimensionality and fingerprint:

- If no index exists yet, auto-creates one via `EnsureMemoryVecIndexWithProfile()`
- If dimensions mismatch, returns an error (which triggers a rebuild in consolidation)
- If the fingerprint is set and doesn't match, returns an error

## Vector Metadata

The `memory_vec_meta` table (`db.go:237-245`) stores:

```sql
CREATE TABLE memory_vec_meta(
    id INTEGER PRIMARY KEY CHECK(id=1),
    dims INTEGER NOT NULL DEFAULT 0,
    embed_fingerprint TEXT NOT NULL DEFAULT '',
    updated_at INTEGER NOT NULL DEFAULT 0
)
```

A single row (id=1) tracks the current vector dimensionality and embedding model fingerprint. This is used to detect mismatches and decide whether a rebuild is needed.

## Cosine Similarity (Software Fallback)

`memory/vector.go:30-47` provides a software cosine similarity implementation used for tests and as a reference:

```go
func Cosine(a, b []float32) float64 {
    // dot product / (||a|| * ||b||)
}
```

## Vector Candidate Type

Search results from the vector index are returned as `VecCandidate` structs (`vector.go:49-61`):

```go
type VecCandidate struct {
    ID         int64
    Text       string
    Score      float64   // 1.0 / (1.0 + distance)
    CreatedAt  int64
    Kind       string
    Status     string
    Importance float64
    Confidence float64
    ExpiresAt  int64
    UseCount   int
    LastUsedAt int64
}
```
