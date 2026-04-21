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
	absSource, err := filepath.Abs(sourceDir)
	if err != nil {
		return report, fmt.Errorf("resolve source dir: %w", err)
	}
	info, err := os.Stat(absSource)
	if err != nil {
		return report, fmt.Errorf("stat source dir: %w", err)
	}
	if !info.IsDir() {
		return report, fmt.Errorf("openclaw source is not a directory: %s", absSource)
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
	if err := purgeImportedOpenClawMemory(ctx, d, memoryScope, importTag); err != nil {
		return report, err
	}

	soulText, ok, err := readOptionalTextFile(filepath.Join(absSource, "SOUL.md"))
	if err != nil {
		return report, err
	}
	if ok {
		backedUp, err := writeFileWithBackup(cfg.SoulFile, []byte(soulText))
		if err != nil {
			return report, err
		}
		report.CopiedFiles++
		if backedUp {
			report.BackedUpFiles++
		}
	}

	identityText, ok, err := readOptionalTextFile(filepath.Join(absSource, "IDENTITY.md"))
	if err != nil {
		return report, err
	}
	if ok {
		backedUp, err := writeFileWithBackup(cfg.IdentityFile, []byte(identityText))
		if err != nil {
			return report, err
		}
		report.CopiedFiles++
		if backedUp {
			report.BackedUpFiles++
		}
	}

	memoryText, _, err := readOptionalTextFile(filepath.Join(absSource, "MEMORY.md"))
	if err != nil {
		return report, err
	}
	userText, _, err := readOptionalTextFile(filepath.Join(absSource, "USER.md"))
	if err != nil {
		return report, err
	}
	staticMemory := buildOpenClawStaticMemory(memoryText, userText)
	if strings.TrimSpace(staticMemory) != "" {
		backedUp, err := writeFileWithBackup(cfg.MemoryFile, []byte(staticMemory))
		if err != nil {
			return report, err
		}
		report.CopiedFiles++
		if backedUp {
			report.BackedUpFiles++
		}
	}

	embedPlan, warning, err := buildOpenClawEmbedPlan(ctx, cfg, d, prov)
	if err != nil {
		return report, err
	}
	if warning != "" {
		report.Warnings = append(report.Warnings, warning)
	}

	memoryFiles, err := collectOpenClawMemoryFiles(filepath.Join(absSource, "memory"))
	if err != nil {
		return report, err
	}
	for _, path := range memoryFiles {
		counts, warnings, err := importOpenClawMemoryFile(ctx, d, prov, embedPlan, importTag, absSource, path, memoryScope, embedMaxBytes)
		if err != nil {
			return report, err
		}
		report.ImportedMemoryFiles++
		report.ImportedChunks += counts.imported
		report.EmbeddedChunks += counts.embedded
		report.Warnings = append(report.Warnings, warnings...)
	}
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

func readOptionalTextFile(path string) (string, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("read %s: %w", path, err)
	}
	return string(data), true, nil
}

func writeFileWithBackup(path string, data []byte) (bool, error) {
	if strings.TrimSpace(path) == "" {
		return false, fmt.Errorf("destination path is empty")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, fmt.Errorf("mkdir %s: %w", filepath.Dir(path), err)
	}
	existing, err := os.ReadFile(path)
	switch {
	case err == nil:
		if bytes.Equal(existing, data) {
			return false, nil
		}
		backupPath, err := nextBackupPath(path)
		if err != nil {
			return false, err
		}
		if err := os.WriteFile(backupPath, existing, 0o644); err != nil {
			return false, fmt.Errorf("write backup %s: %w", backupPath, err)
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			return true, fmt.Errorf("write %s: %w", path, err)
		}
		return true, nil
	case os.IsNotExist(err):
		if err := os.WriteFile(path, data, 0o644); err != nil {
			return false, fmt.Errorf("write %s: %w", path, err)
		}
		return false, nil
	default:
		return false, fmt.Errorf("read %s: %w", path, err)
	}
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

func purgeImportedOpenClawMemory(ctx context.Context, d *db.DB, memoryScope, importTag string) error {
	rows, err := d.SQL.QueryContext(ctx,
		`SELECT id FROM memory_notes WHERE session_key=? AND (tags=? OR tags LIKE ?)`,
		memoryScope, importTag, importTag+",%")
	if err != nil {
		return fmt.Errorf("list prior imported memories: %w", err)
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return fmt.Errorf("scan prior imported memories: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate prior imported memories: %w", err)
	}
	for _, id := range ids {
		if d.VecSQL != nil {
			if _, err := d.VecSQL.ExecContext(ctx, `DELETE FROM memory_vec WHERE note_id=?`, id); err != nil && !strings.Contains(strings.ToLower(err.Error()), "no such table: memory_vec") {
				return fmt.Errorf("delete imported vector row %d: %w", id, err)
			}
		}
		if _, err := d.SQL.ExecContext(ctx, `DELETE FROM memory_notes WHERE id=?`, id); err != nil {
			return fmt.Errorf("delete imported memory row %d: %w", id, err)
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

func collectOpenClawMemoryFiles(root string) ([]string, error) {
	info, err := os.Stat(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("stat memory dir: %w", err)
	}
	if !info.IsDir() {
		return nil, nil
	}
	var files []string
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if path != root && strings.HasPrefix(entry.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(entry.Name(), ".") {
			return nil
		}
		if strings.EqualFold(filepath.Ext(entry.Name()), ".md") {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("scan memory dir: %w", err)
	}
	sort.Strings(files)
	return files, nil
}

type openClawImportCounts struct {
	imported int
	embedded int
}

func importOpenClawMemoryFile(ctx context.Context, d *db.DB, prov *providers.Client, embedPlan openClawEmbedPlan, importTag, sourceRoot, filePath, memoryScope string, embedMaxBytes int) (openClawImportCounts, []string, error) {
	var counts openClawImportCounts
	data, err := os.ReadFile(filePath)
	if err != nil {
		return counts, nil, fmt.Errorf("read memory file %s: %w", filePath, err)
	}
	relPath, err := filepath.Rel(sourceRoot, filePath)
	if err != nil {
		relPath = filepath.Base(filePath)
	}
	chunks := buildOpenClawMemoryChunks(relPath, string(data), embedMaxBytes)
	var warnings []string
	for _, chunk := range chunks {
		tags := importTag + ",source:" + strings.ReplaceAll(relPath, ",", "_")
		input := db.TypedNoteInput{
			Text:        chunk,
			SourceMsgID: sql.NullInt64{},
			Tags:        tags,
			Kind:        db.MemoryKindEpisode,
			Status:      db.MemoryStatusActive,
			Importance:  0.35,
		}
		if embedPlan.enabled {
			vec, err := prov.Embed(ctx, embedPlan.model, chunk)
			if err != nil {
				warnings = append(warnings, fmt.Sprintf("embedding failed for %s; imported without vectors", relPath))
			} else {
				input.Embedding = memory.PackFloat32(vec)
				input.EmbedFingerprint = embedPlan.fingerprint
			}
		}
		if _, err := d.InsertMemoryNoteTyped(ctx, memoryScope, input); err != nil {
			return counts, warnings, fmt.Errorf("insert memory note for %s: %w", relPath, err)
		}
		counts.imported++
		if len(input.Embedding) > 0 {
			counts.embedded++
		}
	}
	return counts, warnings, nil
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
	baseHeader := fmt.Sprintf("OpenClaw memory import\nSource: %s\n\n", relPath)
	maxHeader := fmt.Sprintf("OpenClaw memory import\nSource: %s\nPart: 999/999\n\n", relPath)
	bodyLimit := maxBytes - len(maxHeader)
	if bodyLimit <= 0 {
		bodyLimit = maxBytes
	}
	bodyChunks := splitTextByBytes(text, bodyLimit)
	out := make([]string, 0, len(bodyChunks))
	for i, body := range bodyChunks {
		header := baseHeader
		if len(bodyChunks) > 1 {
			header = fmt.Sprintf("OpenClaw memory import\nSource: %s\nPart: %d/%d\n\n", relPath, i+1, len(bodyChunks))
		}
		out = append(out, strings.TrimSpace(header+body))
	}
	return out
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
