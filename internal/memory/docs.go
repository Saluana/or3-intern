package memory

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"or3-intern/internal/db"
	"or3-intern/internal/providers"
)

// DocIndexConfig controls what gets indexed.
type DocIndexConfig struct {
	Roots          []string
	MaxFiles       int
	MaxFileBytes   int
	MaxChunks      int
	EmbedMaxBytes  int
	RefreshSeconds int
	RetrieveLimit  int
}

// IndexedDoc is a row from memory_docs.
type IndexedDoc struct {
	ID        int64
	ScopeKey  string
	Path      string
	Kind      string
	Title     string
	Summary   string
	Text      string
	Embedding []byte
	MTimeMS   int64
	SizeBytes int64
	Active    bool
	UpdatedAt int64
}

// DocIndexer syncs configured roots into the memory_docs table.
type DocIndexer struct {
	DB         *db.DB
	Provider   *providers.Client
	EmbedModel string
	Config     DocIndexConfig
}

type indexedDocState struct {
	hash      string
	mtimeMS   int64
	sizeBytes int64
	active    bool
}

func (x *DocIndexer) defaults() DocIndexConfig {
	c := x.Config
	if c.MaxFiles <= 0 {
		c.MaxFiles = 100
	}
	if c.MaxFileBytes <= 0 {
		c.MaxFileBytes = 64 * 1024
	}
	if c.MaxChunks <= 0 {
		c.MaxChunks = 500
	}
	if c.EmbedMaxBytes <= 0 {
		c.EmbedMaxBytes = 8 * 1024
	}
	if c.RetrieveLimit <= 0 {
		c.RetrieveLimit = 5
	}
	return c
}

// SyncRoots scans all configured roots and updates memory_docs for scopeKey.
// It enforces caps on file count and file size, skips symlinks, and
// deactivates docs for files that have disappeared.
func (x *DocIndexer) SyncRoots(ctx context.Context, scopeKey string) error {
	if x == nil || x.DB == nil {
		return fmt.Errorf("doc indexer not configured")
	}
	cfg := x.defaults()
	if len(cfg.Roots) == 0 {
		return nil
	}

	seen := map[string]bool{}
	fileCount := 0
	chunkCount := 0
	existing, err := x.loadIndexedDocState(ctx, scopeKey)
	if err != nil {
		return err
	}

	for _, root := range cfg.Roots {
		if strings.TrimSpace(root) == "" {
			continue
		}
		absRoot, err := filepath.Abs(root)
		if err != nil {
			continue
		}
		absRoot, err = filepath.EvalSymlinks(absRoot)
		if err != nil {
			continue
		}

		err = filepath.WalkDir(absRoot, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.Type()&os.ModeSymlink != 0 {
				return nil
			}
			if d.IsDir() {
				if strings.HasPrefix(d.Name(), ".") && path != absRoot {
					return filepath.SkipDir
				}
				return nil
			}
			ext := strings.ToLower(filepath.Ext(path))
			switch ext {
			case ".md", ".txt":
			default:
				return nil
			}

			realPath, err := filepath.EvalSymlinks(path)
			if err != nil {
				return err
			}
			rel, err := filepath.Rel(absRoot, realPath)
			if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				return nil
			}

			if fileCount >= cfg.MaxFiles {
				return filepath.SkipAll
			}
			if chunkCount >= cfg.MaxChunks {
				return filepath.SkipAll
			}

			info, err := os.Lstat(realPath)
			if err != nil {
				return err
			}
			if info.Size() > int64(cfg.MaxFileBytes) {
				return nil
			}

			seen[realPath] = true
			fileCount++
			mtimeMS := info.ModTime().UnixMilli()
			sizeBytes := info.Size()
			if state, ok := existing[realPath]; ok && state.active && state.mtimeMS == mtimeMS && state.sizeBytes == sizeBytes {
				chunkCount++
				return nil
			}

			data, err := readDocFile(realPath, cfg.MaxFileBytes)
			if err != nil {
				return err
			}

			h := fileHash(data)
			if state, ok := existing[realPath]; ok && state.active && state.hash == h {
				chunkCount++
				return nil
			}

			kind := extKind(ext)
			title := filepath.Base(realPath)
			text := string(data)
			summary := extractSummary(text)

			var embedding []byte
			if x.Provider != nil && x.EmbedModel != "" && len(data) <= cfg.EmbedMaxBytes {
				vec, err := x.Provider.Embed(ctx, x.EmbedModel, truncateForEmbed(text, cfg.EmbedMaxBytes))
				if err == nil && len(vec) > 0 {
					embedding = PackFloat32(vec)
				}
			}

			now := db.NowMS()
			_, err = x.DB.SQL.ExecContext(ctx,
				`INSERT INTO memory_docs(scope_key, path, kind, title, summary, text, embedding, hash, mtime_ms, size_bytes, active, updated_at)
                 VALUES(?,?,?,?,?,?,?,?,?,?,1,?)
                 ON CONFLICT(scope_key, path) DO UPDATE SET
                   kind=excluded.kind, title=excluded.title, summary=excluded.summary,
                   text=excluded.text, embedding=excluded.embedding,
                   hash=excluded.hash, mtime_ms=excluded.mtime_ms,
                   size_bytes=excluded.size_bytes, active=1, updated_at=excluded.updated_at`,
				scopeKey, realPath, kind, title, summary, text, nullBytes(embedding), h, mtimeMS, sizeBytes, now)
			if err != nil {
				return fmt.Errorf("upsert indexed doc %s: %w", realPath, err)
			}
			chunkCount++
			return nil
		})
		if err != nil {
			return err
		}
	}

	// deactivate docs no longer on disk
	rows, err := x.DB.SQL.QueryContext(ctx,
		`SELECT path FROM memory_docs WHERE scope_key=? AND active=1`, scopeKey)
	if err != nil {
		return err
	}
	var toDeactivate []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			continue
		}
		if !seen[p] {
			toDeactivate = append(toDeactivate, p)
		}
	}
	rows.Close()
	for _, p := range toDeactivate {
		_, _ = x.DB.SQL.ExecContext(ctx,
			`UPDATE memory_docs SET active=0, updated_at=? WHERE scope_key=? AND path=?`,
			db.NowMS(), scopeKey, p)
	}
	return nil
}

func (x *DocIndexer) loadIndexedDocState(ctx context.Context, scopeKey string) (map[string]indexedDocState, error) {
	rows, err := x.DB.SQL.QueryContext(ctx,
		`SELECT path, hash, mtime_ms, size_bytes, active FROM memory_docs WHERE scope_key=?`, scopeKey)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]indexedDocState{}
	for rows.Next() {
		var path, hash string
		var mtimeMS, sizeBytes int64
		var active int
		if err := rows.Scan(&path, &hash, &mtimeMS, &sizeBytes, &active); err != nil {
			return nil, err
		}
		out[path] = indexedDocState{hash: hash, mtimeMS: mtimeMS, sizeBytes: sizeBytes, active: active == 1}
	}
	return out, rows.Err()
}

func readDocFile(path string, maxBytes int) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(io.LimitReader(f, int64(maxBytes)))
}

// DocRetriever retrieves indexed docs by FTS query.
type DocRetriever struct {
	DB *db.DB
}

// RetrievedDoc is a doc excerpt returned by retrieval.
type RetrievedDoc struct {
	Path    string
	Title   string
	Excerpt string
	Score   float64
}

// RetrieveDocs queries the FTS index for docs matching query.
func (r *DocRetriever) RetrieveDocs(ctx context.Context, scopeKey, query string, topK int) ([]RetrievedDoc, error) {
	if topK <= 0 {
		topK = 5
	}
	q := normalizeFTSQuery(query)
	if q == "" {
		return nil, nil
	}
	rows, err := r.DB.SQL.QueryContext(ctx,
		`SELECT memory_docs_fts.rowid, memory_docs.path, memory_docs.title, memory_docs.text, bm25(memory_docs_fts) as rank
         FROM memory_docs_fts
         JOIN memory_docs ON memory_docs.id = memory_docs_fts.rowid
         WHERE memory_docs_fts MATCH ? AND memory_docs.scope_key=? AND memory_docs.active=1
         ORDER BY rank LIMIT ?`,
		q, scopeKey, topK)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RetrievedDoc
	for rows.Next() {
		var rowid int64
		var path, title, text string
		var rank float64
		if err := rows.Scan(&rowid, &path, &title, &text, &rank); err != nil {
			return nil, err
		}
		out = append(out, RetrievedDoc{
			Path:    path,
			Title:   title,
			Excerpt: excerptText(text, 500),
			Score:   1.0 / (1.0 + rank),
		})
	}
	return out, rows.Err()
}

// UpsertDoc inserts or updates a doc in memory_docs (for direct use by tests).
func UpsertDoc(ctx context.Context, d *db.DB, scopeKey, path, kind, title, summary, text string, embedding []byte, hash string, mtimeMS, sizeBytes int64) error {
	now := db.NowMS()
	_, err := d.SQL.ExecContext(ctx,
		`INSERT INTO memory_docs(scope_key, path, kind, title, summary, text, embedding, hash, mtime_ms, size_bytes, active, updated_at)
         VALUES(?,?,?,?,?,?,?,?,?,?,1,?)
         ON CONFLICT(scope_key, path) DO UPDATE SET
           kind=excluded.kind, title=excluded.title, summary=excluded.summary,
           text=excluded.text, embedding=excluded.embedding,
           hash=excluded.hash, mtime_ms=excluded.mtime_ms,
           size_bytes=excluded.size_bytes, active=1, updated_at=excluded.updated_at`,
		scopeKey, path, kind, title, summary, text, nullBytes(embedding), hash, mtimeMS, sizeBytes, now)
	return err
}

func fileHash(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:8])
}

func extKind(ext string) string {
	switch ext {
	case ".md":
		return "markdown"
	case ".txt":
		return "text"
	default:
		return "text"
	}
}

func extractSummary(text string) string {
	for _, line := range strings.SplitN(text, "\n", 20) {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "```") || strings.HasPrefix(line, "---") {
			continue
		}
		if len(line) > 200 {
			line = line[:200]
		}
		return line
	}
	return ""
}

func truncateForEmbed(text string, max int) string {
	if max <= 0 || len(text) <= max {
		return text
	}
	return text[:max]
}

func excerptText(text string, maxChars int) string {
	text = strings.TrimSpace(text)
	if len(text) <= maxChars {
		return text
	}
	return text[:maxChars] + "…"
}

func nullBytes(b []byte) any {
	if len(b) == 0 {
		return nil
	}
	return b
}
