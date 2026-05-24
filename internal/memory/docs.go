package memory

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"

	"or3-intern/internal/db"
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
	ID               int64
	ScopeKey         string
	Path             string
	Kind             string
	Title            string
	Summary          string
	Text             string
	Embedding        []byte
	EmbedFingerprint string
	MTimeMS          int64
	SizeBytes        int64
	Active           bool
	UpdatedAt        int64
}

// DocIndexer syncs configured roots into the memory_docs table.
type DocIndexer struct {
	DB     *db.DB
	Config DocIndexConfig
}

// DocSyncResult summarizes a root sync pass.
type DocSyncResult struct {
	PartialScan bool
	Warning     string
}

type indexedDocState struct {
	hash        string
	mtimeMS     int64
	sizeBytes   int64
	active      bool
	fingerprint string
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
// It enforces caps on file count and file size, skips symlinks, and only
// deactivates docs when every configured root completed without hitting a cap.
func (x *DocIndexer) SyncRoots(ctx context.Context, scopeKey string) error {
	_, err := x.SyncRootsWithResult(ctx, scopeKey)
	return err
}

// SyncRootsWithResult is like SyncRoots but reports partial-scan warnings.
func (x *DocIndexer) SyncRootsWithResult(ctx context.Context, scopeKey string) (DocSyncResult, error) {
	result := DocSyncResult{}
	if x == nil || x.DB == nil {
		return result, fmt.Errorf("doc indexer not configured")
	}
	cfg := x.defaults()
	if len(cfg.Roots) == 0 {
		return result, nil
	}

	seen := map[string]bool{}
	fileCount := 0
	chunkCount := 0
	existing, err := x.loadIndexedDocState(ctx, scopeKey)
	if err != nil {
		return result, err
	}

	allRootsComplete := true
	hitCap := false
	rootIndex := 0
	for _, root := range cfg.Roots {
		if strings.TrimSpace(root) == "" {
			continue
		}
		currentRoot := rootIndex
		rootIndex++
		absRoot, err := filepath.Abs(root)
		if err != nil {
			allRootsComplete = false
			log.Printf("doc sync skipped root %q: abs path: %v", root, err)
			continue
		}
		absRoot, err = filepath.EvalSymlinks(absRoot)
		if err != nil {
			allRootsComplete = false
			log.Printf("doc sync skipped root %q: symlink eval: %v", root, err)
			continue
		}

		rootComplete := true
		walkErr := filepath.WalkDir(absRoot, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				rootComplete = false
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
				rootComplete = false
				return err
			}
			rel, err := filepath.Rel(absRoot, realPath)
			if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				return nil
			}
			storedPath := docStoredPath(currentRoot, rel)

			if fileCount >= cfg.MaxFiles {
				hitCap = true
				return filepath.SkipAll
			}
			if chunkCount >= cfg.MaxChunks {
				hitCap = true
				return filepath.SkipAll
			}

			info, err := os.Lstat(realPath)
			if err != nil {
				rootComplete = false
				return err
			}
			if info.Size() > int64(cfg.MaxFileBytes) {
				return nil
			}

			seen[storedPath] = true
			fileCount++
			mtimeMS := info.ModTime().UnixMilli()
			sizeBytes := info.Size()
			if state, ok := existing[storedPath]; ok && state.active && state.mtimeMS == mtimeMS && state.sizeBytes == sizeBytes {
				chunkCount++
				return nil
			}

			data, err := readDocFile(realPath, cfg.MaxFileBytes)
			if err != nil {
				rootComplete = false
				return err
			}

			h := fileHash(data)
			if state, ok := existing[storedPath]; ok && state.active && state.hash == h {
				chunkCount++
				return nil
			}

			kind := extKind(ext)
			title := filepath.Base(realPath)
			text := string(data)
			summary := extractSummary(text)

			now := db.NowMS()
			_, err = x.DB.SQL.ExecContext(ctx,
				`INSERT INTO memory_docs(scope_key, path, kind, title, summary, text, embedding, embed_fingerprint, hash, mtime_ms, size_bytes, active, updated_at)
	                VALUES(?,?,?,?,?,?,NULL,'',?,?,?,1,?)
                 ON CONFLICT(scope_key, path) DO UPDATE SET
                   kind=excluded.kind, title=excluded.title, summary=excluded.summary,
                   text=excluded.text, embedding=NULL, embed_fingerprint='',
	                  hash=excluded.hash, mtime_ms=excluded.mtime_ms,
                   size_bytes=excluded.size_bytes, active=1, updated_at=excluded.updated_at`,
				scopeKey, storedPath, kind, title, summary, text, h, mtimeMS, sizeBytes, now)
			if err != nil {
				return fmt.Errorf("upsert indexed doc %s: %w", storedPath, err)
			}
			chunkCount++
			return nil
		})
		if walkErr != nil {
			rootComplete = false
			log.Printf("doc sync root %q incomplete: %v", root, walkErr)
		}
		if !rootComplete {
			allRootsComplete = false
		}
	}

	if hitCap {
		result.PartialScan = true
		result.Warning = "doc sync stopped at configured file/chunk cap; existing docs kept active"
		log.Printf("doc sync partial for scope %q: hit configured cap", scopeKey)
	}
	if !allRootsComplete {
		result.PartialScan = true
		if result.Warning == "" {
			result.Warning = "doc sync did not complete every configured root; existing docs kept active"
		}
		log.Printf("doc sync partial for scope %q: not all roots completed", scopeKey)
	}

	if allRootsComplete && !hitCap {
		rows, err := x.DB.SQL.QueryContext(ctx,
			`SELECT path FROM memory_docs WHERE scope_key=? AND active=1`, scopeKey)
		if err != nil {
			return result, err
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
	}
	RecordDocSyncState(result)
	return result, nil
}

func docStoredPath(rootIndex int, rel string) string {
	return fmt.Sprintf("r%d:%s", rootIndex, filepath.ToSlash(rel))
}

// DocDisplayPath renders a stored doc path for prompts and UI.
func DocDisplayPath(stored string) string {
	stored = strings.TrimSpace(stored)
	if stored == "" {
		return ""
	}
	if idx := strings.Index(stored, ":"); idx > 1 && strings.HasPrefix(stored, "r") {
		return stored[idx+1:]
	}
	return filepath.Base(stored)
}

func (x *DocIndexer) loadIndexedDocState(ctx context.Context, scopeKey string) (map[string]indexedDocState, error) {
	rows, err := x.DB.SQL.QueryContext(ctx,
		`SELECT path, hash, mtime_ms, size_bytes, active, embed_fingerprint FROM memory_docs WHERE scope_key=?`, scopeKey)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]indexedDocState{}
	for rows.Next() {
		var path, hash string
		var mtimeMS, sizeBytes int64
		var active int
		var fingerprint string
		if err := rows.Scan(&path, &hash, &mtimeMS, &sizeBytes, &active, &fingerprint); err != nil {
			return nil, err
		}
		out[path] = indexedDocState{hash: hash, mtimeMS: mtimeMS, sizeBytes: sizeBytes, active: active == 1, fingerprint: fingerprint}
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
			Path:    DocDisplayPath(path),
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
		`INSERT INTO memory_docs(scope_key, path, kind, title, summary, text, embedding, embed_fingerprint, hash, mtime_ms, size_bytes, active, updated_at)
	        VALUES(?,?,?,?,?,?,?,?,?,?,?,1,?)
         ON CONFLICT(scope_key, path) DO UPDATE SET
           kind=excluded.kind, title=excluded.title, summary=excluded.summary,
           text=excluded.text, embedding=excluded.embedding,
	          embed_fingerprint=excluded.embed_fingerprint,
	          hash=excluded.hash, mtime_ms=excluded.mtime_ms,
           size_bytes=excluded.size_bytes, active=1, updated_at=excluded.updated_at`,
		scopeKey, path, kind, title, summary, text, nullBytes(embedding), "", hash, mtimeMS, sizeBytes, now)
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
