package main

import (
	"bytes"
	"context"
	"database/sql"
	"flag"
	"fmt"
	"hash/fnv"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"or3-intern/internal/config"
	"or3-intern/internal/db"
	"or3-intern/internal/memory"
	"or3-intern/internal/providers"
	"or3-intern/internal/scope"
)

const (
	defaultOpenClawEmbedMaxBytes = 6000
	openClawBackupSuffix         = ".bak"
)

type openClawMigrationReport struct {
	CopiedFiles         int
	BackedUpFiles       int
	ImportedMemoryFiles int
	ImportedChunks      int
	EmbeddedChunks      int
	Warnings            []string
}

type openClawEmbedPlan struct {
	enabled     bool
	model       string
	fingerprint string
}

type openClawPreparedNote struct {
	text             string
	tags             string
	embedding        []byte
	embedFingerprint string
	kind             string
	importSource     string
}

type openClawFileWritePlan struct {
	path string
	data []byte
}

type openClawVectorRow struct {
	noteID    int64
	embedding []byte
	text      string
}

func runMigrateOpenClawCommand(ctx context.Context, cfg config.Config, d *db.DB, prov *providers.Client, args []string, stdout, stderr io.Writer) error {
	if d == nil {
		return fmt.Errorf("db is not configured")
	}
	if stdout == nil {
		stdout = io.Discard
	}
	if stderr == nil {
		stderr = io.Discard
	}
	fs := flag.NewFlagSet("migrate-openclaw", flag.ContinueOnError)
	fs.SetOutput(stderr)
	memoryScope := scope.GlobalMemoryScope
	embedMaxBytes := defaultOpenClawEmbedMaxBytes
	fs.StringVar(&memoryScope, "scope", scope.GlobalMemoryScope, "memory scope to receive imported daily notes")
	fs.IntVar(&embedMaxBytes, "embed-max-bytes", defaultOpenClawEmbedMaxBytes, "maximum bytes per imported memory chunk for embeddings")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: migrate-openclaw [--scope <scope-key>] [--embed-max-bytes <n>] <openclaw-agent-dir>")
	}
	report, err := migrateOpenClawAgent(ctx, cfg, d, prov, fs.Arg(0), memoryScope, embedMaxBytes)
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintf(stdout, "copied_files: %d\n", report.CopiedFiles)
	_, _ = fmt.Fprintf(stdout, "backed_up_files: %d\n", report.BackedUpFiles)
	_, _ = fmt.Fprintf(stdout, "memory_files_imported: %d\n", report.ImportedMemoryFiles)
	_, _ = fmt.Fprintf(stdout, "memory_chunks_imported: %d\n", report.ImportedChunks)
	_, _ = fmt.Fprintf(stdout, "memory_chunks_embedded: %d\n", report.EmbeddedChunks)
	for _, warning := range report.Warnings {
		_, _ = fmt.Fprintf(stdout, "warning: %s\n", warning)
	}
	_, _ = fmt.Fprintln(stdout, "ok")
	return nil
}

func migrateOpenClawAgent(ctx context.Context, cfg config.Config, d *db.DB, prov *providers.Client, sourceDir, memoryScope string, embedMaxBytes int) (openClawMigrationReport, error) {
	var report openClawMigrationReport
	absSource, err := resolveOpenClawSourceDir(sourceDir)
	if err != nil {
		return report, err
	}
	if scope.IsGlobalScopeRequest(memoryScope) || strings.TrimSpace(memoryScope) == "" {
		memoryScope = scope.GlobalMemoryScope
	} else {
		memoryScope = strings.TrimSpace(memoryScope)
	}
	if embedMaxBytes <= 0 {
		embedMaxBytes = defaultOpenClawEmbedMaxBytes
	}

	importTag := buildOpenClawImportTag(absSource)
	filePlans := make([]openClawFileWritePlan, 0, 3)

	soulText, ok, err := readOpenClawTextFile(absSource, "SOUL.md")
	if err != nil {
		return report, err
	}
	if ok {
		filePlans = append(filePlans, openClawFileWritePlan{path: cfg.SoulFile, data: []byte(soulText)})
	}

	identityText, ok, err := readOpenClawTextFile(absSource, "IDENTITY.md")
	if err != nil {
		return report, err
	}
	if ok {
		filePlans = append(filePlans, openClawFileWritePlan{path: cfg.IdentityFile, data: []byte(identityText)})
	}

	memoryText, _, err := readOpenClawTextFile(absSource, "MEMORY.md")
	if err != nil {
		return report, err
	}
	userText, _, err := readOpenClawTextFile(absSource, "USER.md")
	if err != nil {
		return report, err
	}
	staticMemory := buildOpenClawStaticMemory(memoryText, userText)
	if strings.TrimSpace(staticMemory) != "" {
		filePlans = append(filePlans, openClawFileWritePlan{path: cfg.MemoryFile, data: []byte(staticMemory)})
	}

	embedPlan, warning, err := buildOpenClawEmbedPlan(ctx, cfg, d, prov)
	if err != nil {
		return report, err
	}
	if warning != "" {
		report.Warnings = append(report.Warnings, warning)
	}

	memoryFiles, err := collectOpenClawMemoryFiles(absSource, filepath.Join(absSource, "memory"))
	if err != nil {
		return report, err
	}
	var preparedNotes []openClawPreparedNote
	for _, path := range memoryFiles {
		notes, warnings, err := prepareOpenClawMemoryFile(ctx, prov, embedPlan, importTag, absSource, path, embedMaxBytes, db.MemoryKindEpisode, "daily")
		if err != nil {
			return report, err
		}
		if len(notes) > 0 {
			report.ImportedMemoryFiles++
		}
		for _, note := range notes {
			report.ImportedChunks++
			if len(note.embedding) > 0 {
				report.EmbeddedChunks++
			}
		}
		preparedNotes = append(preparedNotes, notes...)
		report.Warnings = append(report.Warnings, warnings...)
	}
	dreamsText, ok, err := readOpenClawTextFile(absSource, "DREAMS.md")
	if err != nil {
		return report, err
	}
	if ok {
		dreamNotes, warnings, err := prepareOpenClawMemoryText(ctx, prov, embedPlan, importTag, "DREAMS.md", dreamsText, embedMaxBytes, db.MemoryKindSummary, "dreams")
		if err != nil {
			return report, err
		}
		if len(dreamNotes) > 0 {
			report.ImportedMemoryFiles++
		}
		for _, note := range dreamNotes {
			report.ImportedChunks++
			if len(note.embedding) > 0 {
				report.EmbeddedChunks++
			}
		}
		preparedNotes = append(preparedNotes, dreamNotes...)
		report.Warnings = append(report.Warnings, warnings...)
	}
	dreamFiles, err := collectOpenClawMemoryFiles(absSource, filepath.Join(absSource, "memory", ".dreams"))
	if err != nil {
		return report, err
	}
	for _, path := range dreamFiles {
		notes, warnings, err := prepareOpenClawMemoryFile(ctx, prov, embedPlan, importTag, absSource, path, embedMaxBytes, db.MemoryKindSummary, "dreams")
		if err != nil {
			return report, err
		}
		if len(notes) > 0 {
			report.ImportedMemoryFiles++
		}
		for _, note := range notes {
			report.ImportedChunks++
			if len(note.embedding) > 0 {
				report.EmbeddedChunks++
			}
		}
		preparedNotes = append(preparedNotes, notes...)
		report.Warnings = append(report.Warnings, warnings...)
	}

	copiedFiles, backedUpFiles, restoreFiles, err := applyOpenClawFileWrites(filePlans)
	if err != nil {
		return report, err
	}
	vectorWarning, err := replaceOpenClawImportedMemory(ctx, d, memoryScope, importTag, preparedNotes)
	if err != nil {
		restoreFiles()
		return report, err
	}
	if vectorWarning != "" {
		report.Warnings = append(report.Warnings, vectorWarning)
	}
	report.CopiedFiles = copiedFiles
	report.BackedUpFiles = backedUpFiles
	return report, nil
}

func buildOpenClawStaticMemory(memoryText, userText string) string {
	parts := make([]string, 0, 2)
	if trimmed := strings.TrimSpace(memoryText); trimmed != "" {
		parts = append(parts, strings.TrimRight(memoryText, "\n"))
	}
	if trimmed := strings.TrimSpace(userText); trimmed != "" {
		parts = append(parts, "## Imported OpenClaw USER.md\n"+strings.TrimRight(userText, "\n"))
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, "\n\n") + "\n"
}

func resolveOpenClawSourceDir(sourceDir string) (string, error) {
	absSource, err := filepath.Abs(sourceDir)
	if err != nil {
		return "", fmt.Errorf("resolve source dir: %w", err)
	}
	absSource, err = filepath.EvalSymlinks(absSource)
	if err != nil {
		return "", fmt.Errorf("canonicalize source dir: %w", err)
	}
	info, err := os.Stat(absSource)
	if err != nil {
		return "", fmt.Errorf("stat source dir: %w", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("openclaw source is not a directory: %s", absSource)
	}
	return absSource, nil
}

func readOpenClawTextFile(rootDir, relativePath string) (string, bool, error) {
	fullPath := filepath.Join(rootDir, relativePath)
	info, err := os.Lstat(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("stat %s: %w", fullPath, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return "", false, fmt.Errorf("symlinked source file is not allowed: %s", fullPath)
	}
	if info.IsDir() {
		return "", false, fmt.Errorf("source file is a directory: %s", fullPath)
	}
	realPath, err := filepath.EvalSymlinks(fullPath)
	if err != nil {
		return "", false, fmt.Errorf("canonicalize %s: %w", fullPath, err)
	}
	if !pathWithinRoot(rootDir, realPath) {
		return "", false, fmt.Errorf("source file escapes root: %s", fullPath)
	}
	data, err := os.ReadFile(realPath)
	if err != nil {
		return "", false, fmt.Errorf("read %s: %w", realPath, err)
	}
	return string(data), true, nil
}

func applyOpenClawFileWrites(plans []openClawFileWritePlan) (int, int, func(), error) {
	type previousFileState struct {
		path   string
		exists bool
		data   []byte
		mode   os.FileMode
		backup string
	}
	applied := make([]previousFileState, 0, len(plans))
	copiedFiles := 0
	backedUpFiles := 0
	restore := func() {
		for i := len(applied) - 1; i >= 0; i-- {
			state := applied[i]
			if state.exists {
				_ = os.WriteFile(state.path, state.data, state.mode)
			} else {
				_ = os.Remove(state.path)
			}
			if strings.TrimSpace(state.backup) != "" {
				_ = os.Remove(state.backup)
			}
		}
	}
	for _, plan := range plans {
		if strings.TrimSpace(plan.path) == "" {
			restore()
			return copiedFiles, backedUpFiles, func() {}, fmt.Errorf("destination path is empty")
		}
		if err := os.MkdirAll(filepath.Dir(plan.path), 0o755); err != nil {
			restore()
			return copiedFiles, backedUpFiles, func() {}, fmt.Errorf("mkdir %s: %w", filepath.Dir(plan.path), err)
		}
		existing, err := os.ReadFile(plan.path)
		switch {
		case err == nil:
			if bytes.Equal(existing, plan.data) {
				continue
			}
			info, statErr := os.Stat(plan.path)
			if statErr != nil {
				restore()
				return copiedFiles, backedUpFiles, restore, fmt.Errorf("stat %s: %w", plan.path, statErr)
			}
			mode := info.Mode().Perm()
			backupPath, err := nextBackupPath(plan.path)
			if err != nil {
				restore()
				return copiedFiles, backedUpFiles, func() {}, err
			}
			if err := os.WriteFile(backupPath, existing, mode); err != nil {
				restore()
				return copiedFiles, backedUpFiles, func() {}, fmt.Errorf("write backup %s: %w", backupPath, err)
			}
			backedUpFiles++
			applied = append(applied, previousFileState{path: plan.path, exists: true, data: existing, mode: mode, backup: backupPath})
		case os.IsNotExist(err):
			applied = append(applied, previousFileState{path: plan.path, exists: false, mode: 0o644})
		default:
			restore()
			return copiedFiles, backedUpFiles, func() {}, fmt.Errorf("read %s: %w", plan.path, err)
		}
		mode := os.FileMode(0o644)
		if state := applied[len(applied)-1]; state.path == plan.path && state.exists {
			mode = state.mode
		}
		if err := os.WriteFile(plan.path, plan.data, mode); err != nil {
			restore()
			return copiedFiles, backedUpFiles, func() {}, fmt.Errorf("write %s: %w", plan.path, err)
		}
		copiedFiles++
	}
	return copiedFiles, backedUpFiles, restore, nil
}

func nextBackupPath(path string) (string, error) {
	candidate := path + openClawBackupSuffix
	if _, err := os.Stat(candidate); os.IsNotExist(err) {
		return candidate, nil
	} else if err != nil {
		return "", fmt.Errorf("stat backup %s: %w", candidate, err)
	}
	for i := 2; i < 1000; i++ {
		candidate = fmt.Sprintf("%s%s.%d", path, openClawBackupSuffix, i)
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			return candidate, nil
		} else if err != nil {
			return "", fmt.Errorf("stat backup %s: %w", candidate, err)
		}
	}
	return "", fmt.Errorf("no free backup slot for %s", path)
}

func buildOpenClawImportTag(sourceDir string) string {
	base := strings.ToLower(filepath.Base(strings.TrimSpace(sourceDir)))
	base = strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z':
			return r
		case r >= '0' && r <= '9':
			return r
		case r == '-' || r == '_':
			return r
		default:
			return '-'
		}
	}, base)
	base = strings.Trim(base, "-")
	if base == "" {
		base = "agent"
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(sourceDir))
	return fmt.Sprintf("import:openclaw:%s-%08x", base, h.Sum32())
}

func listImportedOpenClawNoteIDs(ctx context.Context, d *db.DB, memoryScope, importTag string) ([]int64, error) {
	escapedTag := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(importTag)
	rows, err := d.SQL.QueryContext(ctx,
		`SELECT id FROM memory_notes WHERE session_key=? AND (tags=? OR tags LIKE ? ESCAPE '\')`,
		memoryScope, importTag, escapedTag+",%")
	if err != nil {
		return nil, fmt.Errorf("list prior imported memories: %w", err)
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan prior imported memories: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate prior imported memories: %w", err)
	}
	return ids, nil
}

func replaceOpenClawImportedMemory(ctx context.Context, d *db.DB, memoryScope, importTag string, notes []openClawPreparedNote) (string, error) {
	if err := ensureOpenClawEmbeddingProfile(ctx, d, notes); err != nil {
		return "", err
	}
	existingIDs, err := listImportedOpenClawNoteIDs(ctx, d, memoryScope, importTag)
	if err != nil {
		return "", err
	}
	tx, err := d.SQL.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer func() {
		_ = tx.Rollback()
	}()
	var vectorRows []openClawVectorRow
	for _, note := range notes {
		emb := note.embedding
		if emb == nil {
			emb = make([]byte, 0)
		}
		res, err := tx.ExecContext(ctx,
			`INSERT INTO memory_notes(session_key, text, embedding, embed_fingerprint, source_message_id, tags, created_at, kind, status, importance)
			 VALUES(?,?,?,?,?,?,?,?,?,?)`,
			memoryScope, note.text, emb, note.embedFingerprint, sql.NullInt64{}, note.tags, db.NowMS(),
			openClawNoteKind(note.kind), db.MemoryStatusActive, 0.35)
		if err != nil {
			return "", fmt.Errorf("insert imported memory note: %w", err)
		}
		id, _ := res.LastInsertId()
		if len(note.embedding) >= 4 && len(note.embedding)%4 == 0 {
			vectorRows = append(vectorRows, openClawVectorRow{noteID: id, embedding: note.embedding, text: note.text})
		}
	}
	for _, id := range existingIDs {
		if _, err := tx.ExecContext(ctx, `DELETE FROM memory_notes WHERE id=?`, id); err != nil {
			return "", fmt.Errorf("delete prior imported memory note %d: %w", id, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	if err := syncOpenClawMemoryVectors(ctx, d, memoryScope, existingIDs, vectorRows); err != nil {
		return fmt.Sprintf("memory vectors were skipped after import: %v", err), nil
	}
	return "", nil
}

func ensureOpenClawEmbeddingProfile(ctx context.Context, d *db.DB, notes []openClawPreparedNote) error {
	for _, note := range notes {
		if len(note.embedding) < 4 || len(note.embedding)%4 != 0 {
			continue
		}
		return d.EnsureMemoryVecIndexWithProfile(ctx, len(note.embedding)/4, note.embedFingerprint)
	}
	return nil
}

func syncOpenClawMemoryVectors(ctx context.Context, d *db.DB, memoryScope string, oldIDs []int64, rows []openClawVectorRow) error {
	if d.VecSQL == nil {
		return nil
	}
	for _, id := range oldIDs {
		if _, err := d.VecSQL.ExecContext(ctx, `DELETE FROM memory_vec WHERE note_id=?`, id); err != nil && !strings.Contains(strings.ToLower(err.Error()), "no such table: memory_vec") {
			return fmt.Errorf("delete imported vector row %d: %w", id, err)
		}
	}
	for _, row := range rows {
		if _, err := d.VecSQL.ExecContext(ctx,
			`INSERT OR REPLACE INTO memory_vec(note_id, session_key, embedding, text) VALUES(?,?,?,?)`,
			row.noteID, memoryScope, row.embedding, row.text); err != nil && !strings.Contains(strings.ToLower(err.Error()), "no such table: memory_vec") {
			return fmt.Errorf("upsert imported vector row %d: %w", row.noteID, err)
		}
	}
	return nil
}

func buildOpenClawEmbedPlan(ctx context.Context, cfg config.Config, d *db.DB, prov *providers.Client) (openClawEmbedPlan, string, error) {
	plan := openClawEmbedPlan{}
	if prov == nil || strings.TrimSpace(cfg.Provider.EmbedModel) == "" {
		return plan, "", nil
	}
	current := currentEmbedFingerprint(cfg)
	dims, err := d.MemoryVectorDims(ctx)
	if err != nil {
		return plan, "", err
	}
	if dims == 0 {
		plan.enabled = true
		plan.model = strings.TrimSpace(cfg.Provider.EmbedModel)
		plan.fingerprint = current
		return plan, "", nil
	}
	stored, err := d.MemoryVectorFingerprint(ctx)
	if err != nil {
		return plan, "", err
	}
	if strings.TrimSpace(stored) == "" || strings.TrimSpace(stored) != strings.TrimSpace(current) {
		return plan, fmt.Sprintf("skipping imported memory embeddings because stored fingerprint %q does not match current fingerprint %q; run `or3-intern embeddings rebuild memory` first if you want vector embeddings", emptyAsNone(stored), emptyAsNone(current)), nil
	}
	plan.enabled = true
	plan.model = strings.TrimSpace(cfg.Provider.EmbedModel)
	plan.fingerprint = current
	return plan, "", nil
}

func collectOpenClawMemoryFiles(sourceRoot, root string) ([]string, error) {
	info, err := os.Lstat(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("stat memory dir: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("symlinked memory dir is not allowed: %s", root)
	}
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("canonicalize memory dir: %w", err)
	}
	if !info.IsDir() {
		return nil, nil
	}
	if !pathWithinRoot(sourceRoot, realRoot) {
		return nil, fmt.Errorf("memory dir escapes source root: %s", root)
	}
	var files []string
	err = filepath.WalkDir(realRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlinked memory path is not allowed: %s", path)
		}
		if entry.IsDir() {
			if path != realRoot && strings.HasPrefix(entry.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(entry.Name(), ".") {
			return nil
		}
		if strings.EqualFold(filepath.Ext(entry.Name()), ".md") {
			realPath, err := filepath.EvalSymlinks(path)
			if err != nil {
				return err
			}
			if !pathWithinRoot(realRoot, realPath) {
				return fmt.Errorf("memory path escapes source root: %s", path)
			}
			files = append(files, realPath)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("scan memory dir: %w", err)
	}
	sort.Strings(files)
	return files, nil
}

func prepareOpenClawMemoryFile(ctx context.Context, prov *providers.Client, embedPlan openClawEmbedPlan, importTag, sourceRoot, filePath string, embedMaxBytes int, kind, importSource string) ([]openClawPreparedNote, []string, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, nil, fmt.Errorf("read memory file %s: %w", filePath, err)
	}
	relPath, err := filepath.Rel(sourceRoot, filePath)
	if err != nil {
		relPath = filepath.Base(filePath)
	}
	return prepareOpenClawMemoryText(ctx, prov, embedPlan, importTag, relPath, string(data), embedMaxBytes, kind, importSource)
}

func prepareOpenClawMemoryText(ctx context.Context, prov *providers.Client, embedPlan openClawEmbedPlan, importTag, relPath, text string, embedMaxBytes int, kind, importSource string) ([]openClawPreparedNote, []string, error) {
	chunks := buildOpenClawMemoryChunks(relPath, text, embedMaxBytes)
	notes := make([]openClawPreparedNote, 0, len(chunks))
	var warnings []string
	for _, chunk := range chunks {
		note := openClawPreparedNote{
			text:         chunk,
			tags:         importTag + ",source:" + strings.ReplaceAll(relPath, ",", "_") + ",type:" + strings.ReplaceAll(strings.TrimSpace(importSource), ",", "_"),
			kind:         kind,
			importSource: importSource,
		}
		if embedPlan.enabled {
			vec, err := prov.Embed(ctx, embedPlan.model, chunk)
			if err != nil {
				warnings = append(warnings, fmt.Sprintf("embedding failed for %s; imported without vectors", relPath))
			} else {
				note.embedding = memory.PackFloat32(vec)
				note.embedFingerprint = embedPlan.fingerprint
			}
		}
		notes = append(notes, note)
	}
	return notes, warnings, nil
}

func openClawNoteKind(kind string) string {
	kind = strings.TrimSpace(kind)
	if kind == "" {
		return db.MemoryKindEpisode
	}
	return kind
}

func buildOpenClawMemoryChunks(relPath, text string, maxBytes int) []string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	if maxBytes <= 0 {
		maxBytes = defaultOpenClawEmbedMaxBytes
	}
	maxHeader := openClawChunkHeader(relPath, 999, 999, maxBytes)
	bodyLimit := maxBytes - len(maxHeader)
	if bodyLimit <= 0 {
		bodyLimit = maxBytes / 2
		if bodyLimit <= 0 {
			bodyLimit = 1
		}
	}
	bodyChunks := splitTextByBytes(text, bodyLimit)
	out := make([]string, 0, len(bodyChunks))
	for i, body := range bodyChunks {
		header := openClawChunkHeader(relPath, i+1, len(bodyChunks), maxBytes)
		out = append(out, strings.TrimSpace(header+body))
	}
	return out
}

func openClawChunkHeader(relPath string, part, total, maxBytes int) string {
	base := "OpenClaw memory import"
	if total > 1 {
		base += fmt.Sprintf("\nPart: %d/%d", part, total)
	}
	headerWithSource := func(source string) string {
		if strings.TrimSpace(source) == "" {
			return base + "\n\n"
		}
		return base + "\nSource: " + source + "\n\n"
	}
	for _, source := range []string{relPath, filepath.Base(relPath), ""} {
		header := headerWithSource(source)
		if len(header) < maxBytes {
			return header
		}
	}
	return base + "\n\n"
}

func splitTextByBytes(text string, maxBytes int) []string {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	if maxBytes <= 0 || len(text) <= maxBytes {
		return []string{strings.TrimSpace(text)}
	}
	paragraphs := splitParagraphs(text)
	out := make([]string, 0, len(paragraphs))
	var current strings.Builder
	flush := func() {
		if strings.TrimSpace(current.String()) == "" {
			current.Reset()
			return
		}
		out = append(out, strings.TrimSpace(current.String()))
		current.Reset()
	}
	for _, paragraph := range paragraphs {
		paragraph = strings.TrimSpace(paragraph)
		if paragraph == "" {
			continue
		}
		if len(paragraph) > maxBytes {
			flush()
			out = append(out, splitLongTextByBytes(paragraph, maxBytes)...)
			continue
		}
		candidate := paragraph
		if current.Len() > 0 {
			candidate = current.String() + "\n\n" + paragraph
		}
		if len(candidate) <= maxBytes {
			current.Reset()
			current.WriteString(candidate)
			continue
		}
		flush()
		current.WriteString(paragraph)
	}
	flush()
	return out
}

func splitParagraphs(text string) []string {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	var out []string
	var current []string
	flush := func() {
		if len(current) == 0 {
			return
		}
		out = append(out, strings.Join(current, "\n"))
		current = nil
	}
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			flush()
			continue
		}
		current = append(current, line)
	}
	flush()
	return out
}

func splitLongTextByBytes(text string, maxBytes int) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	if maxBytes <= 0 || len(text) <= maxBytes {
		return []string{text}
	}
	var out []string
	for len(text) > 0 {
		if len(text) <= maxBytes {
			out = append(out, strings.TrimSpace(text))
			break
		}
		cut := bestChunkCut(text, maxBytes)
		out = append(out, strings.TrimSpace(text[:cut]))
		text = strings.TrimSpace(text[cut:])
	}
	return out
}

func bestChunkCut(text string, maxBytes int) int {
	cut := maxBytes
	for cut > 0 && !utf8.ValidString(text[:cut]) {
		cut--
	}
	if cut <= 0 {
		return maxBytes
	}
	best := cut
	for i := cut; i > cut/2; {
		r, size := utf8.DecodeLastRuneInString(text[:i])
		if r == utf8.RuneError && size == 0 {
			break
		}
		if unicode.IsSpace(r) {
			best = i
			break
		}
		i -= size
	}
	return best
}

func pathWithinRoot(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
