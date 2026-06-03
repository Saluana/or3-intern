# Memory Retrieval

Memory retrieval is the process of finding relevant stored memories when the agent needs context. It uses hybrid search combining four signals into a single ranked result set.

Source: `internal/memory/retrieve.go`

## The Retriever

The `Retriever` struct (`retrieve.go:34-45`) holds configuration:

```go
type Retriever struct {
    DB               *db.DB
    EmbedFingerprint string
    VectorWeight     float64  // default: 0.50
    FTSWeight        float64  // default: 0.22
    LexicalWeight    float64  // default: 0.12
    RecencyWeight    float64  // default: 0.08
    TaskWeight       float64  // default: 0.08
    VectorScanLimit  int      // default: 2000
    TaskContext      string
    LastRejected     []string
}
```

Default weights are set in `NewRetriever` (`retrieve.go:53`).

## How Retrieval Works

`Retrieve()` (`retrieve.go:57-64`) runs in two phases:

### Phase 1: Candidate Gathering

`retrieveCandidates()` (`retrieve.go:66-252`) collects candidates from three sources:

1. **Vector search** — Calls `VectorSearch()` which uses sqlite-vec to find rows by embedding similarity. The distance is converted to a score: `1.0 / (1.0 + distance)`. It only runs if the stored vector fingerprint matches the retriever's fingerprint (`retrieve.go:70-86`).

2. **FTS search** — Calls `searchFTSWithFallback()` (`retrieve.go:286-298`) which queries the `memory_fts` FTS5 virtual table. It first tries the normalized query, and if that fails, falls back to a quoted version. BM25 rank is converted to a positive score: `1.0 / (1.0 + rank)`.

3. **Document search** — If `ftsK > 0`, calls `retrieveDocCandidates()` (`retrieve.go:258-284`) which searches `memory_docs_fts` using BM25.

Candidates from all sources are merged into a map keyed by note ID. If the same note appears in both vector and FTS results, the signals are combined.

### Phase 2: Scoring

Each merged candidate receives a composite score (`retrieve.go:195`):

```
score = (vectorScore * VectorWeight)
      + (ftsScore * FTSWeight)
      + (docScore * FTSWeight)
      + (lexical * LexicalWeight)
      + (taskOverlap * TaskWeight)
      + (recency * RecencyWeight)
```

Additional adjustments:

- **Lexical overlap** (`retrieve.go:408-420`) — Fraction of non-stopword query tokens (>=3 chars) found in the text.
- **Task overlap** — Same as lexical but against the `TaskContext` string.
- **Recency** (`retrieve.go:422-431`) — Exponential decay: `exp(-ageHours / (24 * 14))`. A note's score halves roughly every 23 days.
- **Distinctive token penalty** — If a result has zero FTS, zero document, zero lexical, and zero task overlap but comes from vector search alone, it gets a `-0.12` penalty (`retrieve.go:190-198`). Results with vector score below `0.35` in this case are rejected entirely.
- **Metadata adjustment** (`retrieve.go:304-341`) — Small bounded adjustments:
  - Stale/superseded notes: `-0.10`
  - Facts/procedures: `+0.03`
  - Preferences/goals: `+0.02`
  - Summaries/episodes: `-0.01`
  - Importance (0-1): up to `+0.04`
  - Use count (capped at 5): up to `+0.05`

### Phase 3: Packing to Budget

`diversifyRetrieved()` (`retrieve.go:433-481`) selects the final top-K results:

1. Rejects notes with lifecycle issues (stale, superseded, expired, low confidence) via `retrievalRejectReason()` (`retrieve.go:483-494`).
2. Rejects near-duplicates using token-set Jaccard similarity >= 0.8 (`retrieve.go:512-543`).
3. Applies source diversity penalty: repeated hits from the same source (vector, fts) have their scores multiplied by `0.85 + 0.15 / count`.

## Distinctive Token Detection

`distinctiveRetrievalTokens()` (`retrieve.go:362-380`) identifies tokens that contain both letters and digits, have 2+ uppercase letters, or exhibit mixed case. These are used to penalize vector-only results that lack lexical support.

## FTS Query Normalization

`normalizeFTSQuery()` (`retrieve.go:545-558`) splits the query by spaces and wraps terms containing FTS-special characters (`"`, `:`, `*`, `(`, `)`, `-`) in double quotes to avoid FTS5 syntax errors.
