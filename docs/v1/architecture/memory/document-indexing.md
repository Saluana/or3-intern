# Document Indexing

OR3 Intern can index workspace `.md` and `.txt` files into the `memory_docs` table for full-text search retrieval. This is separate from the memory notes system — it indexes actual files on disk.

Source: `internal/memory/docs.go`

## Configuration

`DocIndexConfig` (`docs.go:19-27`) controls what gets indexed:

```go
type DocIndexConfig struct {
    Roots          []string  // directories to scan
    MaxFiles       int       // max files to index (default: 100)
    MaxFileBytes   int       // max bytes per file (default: 64KB)
    MaxChunks      int       // max total chunks (default: 500, acts as total count)
    EmbedMaxBytes  int       // max bytes for embedding (default: 8KB)
    RefreshSeconds int       // refresh interval
    RetrieveLimit  int       // default retrieval limit (default: 5)
}
```

## The DocIndexer

`DocIndexer` (`docs.go:47-53`) holds the DB connection, provider client, embedding model, and config.

### SyncRoots

`SyncRoots()` (`docs.go:86-238`) is the main indexing function. For a given `scopeKey`:

1. **Loads existing state** — Reads all current `memory_docs` rows for the scope into a map of path → `indexedDocState` (hash, mtime, size, active, fingerprint).

2. **Walks configured roots** — For each root directory, walks files matching `.md` or `.txt` extensions. It skips:
   - Symlinks
   - Hidden directories (starting with `.`, except the root itself)
   - Files exceeding `MaxFileBytes`
   - Files beyond `MaxFiles` or `MaxChunks` caps

3. **Change detection** — A file is only re-indexed if its mtime, size, or content hash has changed since the last index. Two checks:
   - First: mtime + size match → skip (fast path)
   - Second: SHA256 hash match → skip (content hasn't changed)

4. **Reads and indexes** — For changed files:
   - Reads up to `MaxFileBytes`
   - Extracts file kind (`"markdown"` or `"text"`)
   - Extracts a one-line summary from the first non-heading, non-code-fence line
   - Generates an embedding if text fits within `EmbedMaxBytes`
   - Upserts into `memory_docs` using `INSERT ... ON CONFLICT(scope_key, path) DO UPDATE`

5. **Deactivates removed files** — Any `memory_docs` row with `active=1` that was not seen during the walk is set to `active=0`.

### Change Detection Details

The content hash is the hex-encoded first 8 bytes of SHA256 (`docs.go:337-340`). This is a fast, compact hash for detecting content changes without storing the full hash.

## FTS Index on Documents

The `memory_docs_fts` virtual table (`db.go:226`) provides BM25-ranked full-text search over document title, summary, and text:

```sql
CREATE VIRTUAL TABLE memory_docs_fts USING fts5(
    title, summary, text,
    content='memory_docs', content_rowid='id'
)
```

Triggers keep the FTS index in sync with `memory_docs` on insert, update, and delete.

## Document Retrieval

`DocRetriever` (`docs.go:271-273`) queries documents:

```go
type DocRetriever struct { DB *db.DB }
```

`RetrieveDocs()` (`docs.go:284-319`) does an FTS query matching against `memory_docs_fts` joined with `memory_docs`, filtered by `scope_key` and `active=1`. Results are ordered by BM25 rank. The score is converted to: `1.0 / (1.0 + rank)`.

Each result includes the file path, title, and an excerpt (first 500 characters of text).

Retrieved documents are included in memory retrieval results with `Source: "doc"` and `Kind: "file_summary"`.

## Helper Functions

- `fileHash()` — SHA256 of first 8 bytes, hex-encoded
- `extKind()` — Maps `.md` → `"markdown"`, `.txt` → `"text"`
- `extractSummary()` — First non-heading, non-code-fence line (max 200 chars)
- `truncateForEmbed()` — Truncates text for embedding generation
- `excerptText()` — Truncates text for display (max N chars + "…")
- `nullBytes()` — Returns `nil` for empty byte slices (for SQL NULL)
