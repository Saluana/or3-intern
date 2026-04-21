package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"or3-intern/internal/config"
	"or3-intern/internal/db"
	"or3-intern/internal/scope"
)

func TestRunMigrateOpenClawCommand_ImportsFilesAndMemory(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	src := filepath.Join(root, "openclaw-agent")
	if err := os.MkdirAll(filepath.Join(src, "memory"), 0o755); err != nil {
		t.Fatalf("mkdir source memory dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(src, "SOUL.md"), []byte("# Soul\nImported soul\n"), 0o644); err != nil {
		t.Fatalf("write soul: %v", err)
	}
	if err := os.WriteFile(filepath.Join(src, "IDENTITY.md"), []byte("# Identity\nAgent name: Lobster\n"), 0o644); err != nil {
		t.Fatalf("write identity: %v", err)
	}
	if err := os.WriteFile(filepath.Join(src, "MEMORY.md"), []byte("# Memory\nLong term fact\n"), 0o644); err != nil {
		t.Fatalf("write memory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(src, "USER.md"), []byte("Preferred human: Alice\n"), 0o644); err != nil {
		t.Fatalf("write user: %v", err)
	}
	if err := os.WriteFile(filepath.Join(src, "memory", "2026-04-20.md"), []byte("Daily note one.\n\nDaily note two.\n"), 0o644); err != nil {
		t.Fatalf("write daily memory: %v", err)
	}

	cfg := config.Default()
	cfg.SoulFile = filepath.Join(root, "or3", "SOUL.md")
	cfg.IdentityFile = filepath.Join(root, "or3", "IDENTITY.md")
	cfg.MemoryFile = filepath.Join(root, "or3", "MEMORY.md")

	d, err := db.Open(filepath.Join(root, "or3.sqlite"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer d.Close()

	var stdout bytes.Buffer
	if err := runMigrateOpenClawCommand(ctx, cfg, d, nil, []string{src}, &stdout, &bytes.Buffer{}); err != nil {
		t.Fatalf("runMigrateOpenClawCommand: %v", err)
	}

	soul, err := os.ReadFile(cfg.SoulFile)
	if err != nil {
		t.Fatalf("read migrated soul: %v", err)
	}
	if !strings.Contains(string(soul), "Imported soul") {
		t.Fatalf("expected migrated soul, got %q", string(soul))
	}

	identity, err := os.ReadFile(cfg.IdentityFile)
	if err != nil {
		t.Fatalf("read migrated identity: %v", err)
	}
	if !strings.Contains(string(identity), "Lobster") {
		t.Fatalf("expected migrated identity, got %q", string(identity))
	}

	staticMemory, err := os.ReadFile(cfg.MemoryFile)
	if err != nil {
		t.Fatalf("read migrated memory: %v", err)
	}
	gotMemory := string(staticMemory)
	if !strings.Contains(gotMemory, "Long term fact") || !strings.Contains(gotMemory, "Imported OpenClaw USER.md") || !strings.Contains(gotMemory, "Alice") {
		t.Fatalf("expected MEMORY.md + USER.md content, got %q", gotMemory)
	}

	rows, err := d.StreamMemoryNotesScopeLimit(ctx, scope.GlobalMemoryScope, 10)
	if err != nil {
		t.Fatalf("StreamMemoryNotesScopeLimit: %v", err)
	}
	defer rows.Close()
	var noteCount int
	var joined strings.Builder
	for rows.Next() {
		var id int64
		var text string
		var embedding []byte
		var sourceID any
		var tags string
		var createdAt int64
		if err := rows.Scan(&id, &text, &embedding, &sourceID, &tags, &createdAt); err != nil {
			t.Fatalf("scan memory note: %v", err)
		}
		noteCount++
		joined.WriteString(text)
		joined.WriteString("\n")
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate memory notes: %v", err)
	}
	if noteCount == 0 {
		t.Fatal("expected imported memory notes")
	}
	if !strings.Contains(joined.String(), "OpenClaw memory import") || !strings.Contains(joined.String(), "Daily note one.") {
		t.Fatalf("expected imported daily notes, got %q", joined.String())
	}
	if !strings.Contains(stdout.String(), "memory_files_imported: 1") || !strings.Contains(stdout.String(), "ok") {
		t.Fatalf("expected migration summary, got %q", stdout.String())
	}
}

func TestRunMigrateOpenClawCommand_ReplacesPreviousImportAndBacksUpFiles(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	src := filepath.Join(root, "agent")
	if err := os.MkdirAll(filepath.Join(src, "memory"), 0o755); err != nil {
		t.Fatalf("mkdir source memory dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(src, "SOUL.md"), []byte("new soul\n"), 0o644); err != nil {
		t.Fatalf("write soul: %v", err)
	}
	if err := os.WriteFile(filepath.Join(src, "memory", "2026-04-20.md"), []byte("first import note\n"), 0o644); err != nil {
		t.Fatalf("write memory note: %v", err)
	}

	cfg := config.Default()
	cfg.SoulFile = filepath.Join(root, "or3", "SOUL.md")
	cfg.IdentityFile = filepath.Join(root, "or3", "IDENTITY.md")
	cfg.MemoryFile = filepath.Join(root, "or3", "MEMORY.md")
	if err := os.MkdirAll(filepath.Dir(cfg.SoulFile), 0o755); err != nil {
		t.Fatalf("mkdir dest dir: %v", err)
	}
	if err := os.WriteFile(cfg.SoulFile, []byte("old soul\n"), 0o644); err != nil {
		t.Fatalf("write existing soul: %v", err)
	}

	d, err := db.Open(filepath.Join(root, "or3.sqlite"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer d.Close()

	if err := runMigrateOpenClawCommand(ctx, cfg, d, nil, []string{src}, &bytes.Buffer{}, &bytes.Buffer{}); err != nil {
		t.Fatalf("first migrate: %v", err)
	}
	alias := filepath.Join(root, "agent-link")
	if err := os.Symlink(src, alias); err != nil {
		t.Fatalf("symlink source alias: %v", err)
	}
	if err := runMigrateOpenClawCommand(ctx, cfg, d, nil, []string{alias}, &bytes.Buffer{}, &bytes.Buffer{}); err != nil {
		t.Fatalf("second migrate: %v", err)
	}

	matches, err := filepath.Glob(cfg.SoulFile + openClawBackupSuffix + "*")
	if err != nil {
		t.Fatalf("glob backups: %v", err)
	}
	if len(matches) == 0 {
		t.Fatal("expected soul backup to be created")
	}

	rows, err := d.StreamMemoryNotesScopeLimit(ctx, scope.GlobalMemoryScope, 20)
	if err != nil {
		t.Fatalf("StreamMemoryNotesScopeLimit: %v", err)
	}
	defer rows.Close()
	var noteCount int
	for rows.Next() {
		noteCount++
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate memory notes: %v", err)
	}
	if noteCount != 1 {
		t.Fatalf("expected repeat import to replace previous imported notes, got %d notes", noteCount)
	}
}

func TestRunMigrateOpenClawCommand_PreservesPreviousImportOnInvalidRerun(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	src := filepath.Join(root, "agent")
	if err := os.MkdirAll(filepath.Join(src, "memory"), 0o755); err != nil {
		t.Fatalf("mkdir source memory dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(src, "SOUL.md"), []byte("initial soul\n"), 0o644); err != nil {
		t.Fatalf("write soul: %v", err)
	}
	if err := os.WriteFile(filepath.Join(src, "memory", "2026-04-20.md"), []byte("stable imported note\n"), 0o644); err != nil {
		t.Fatalf("write memory note: %v", err)
	}

	cfg := config.Default()
	cfg.SoulFile = filepath.Join(root, "or3", "SOUL.md")
	cfg.IdentityFile = filepath.Join(root, "or3", "IDENTITY.md")
	cfg.MemoryFile = filepath.Join(root, "or3", "MEMORY.md")

	d, err := db.Open(filepath.Join(root, "or3.sqlite"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer d.Close()

	if err := runMigrateOpenClawCommand(ctx, cfg, d, nil, []string{src}, &bytes.Buffer{}, &bytes.Buffer{}); err != nil {
		t.Fatalf("first migrate: %v", err)
	}

	if err := os.Remove(filepath.Join(src, "SOUL.md")); err != nil {
		t.Fatalf("remove soul: %v", err)
	}
	if err := os.Symlink(filepath.Join(root, "outside.md"), filepath.Join(src, "SOUL.md")); err != nil {
		t.Fatalf("replace soul with symlink: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "outside.md"), []byte("outside data\n"), 0o644); err != nil {
		t.Fatalf("write outside file: %v", err)
	}

	err = runMigrateOpenClawCommand(ctx, cfg, d, nil, []string{src}, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected symlinked source file to be rejected")
	}

	rows, err := d.StreamMemoryNotesScopeLimit(ctx, scope.GlobalMemoryScope, 20)
	if err != nil {
		t.Fatalf("StreamMemoryNotesScopeLimit: %v", err)
	}
	defer rows.Close()
	var noteCount int
	for rows.Next() {
		noteCount++
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate memory notes: %v", err)
	}
	if noteCount != 1 {
		t.Fatalf("expected previous imported notes to remain after failed rerun, got %d notes", noteCount)
	}
}

func TestBuildOpenClawMemoryChunks_RespectsByteLimit(t *testing.T) {
	text := strings.Repeat("paragraph words ", 40)
	chunks := buildOpenClawMemoryChunks("memory/2026-04-20.md", text, 120)
	if len(chunks) < 2 {
		t.Fatalf("expected multiple chunks, got %d", len(chunks))
	}
	for _, chunk := range chunks {
		if len(chunk) > 160 {
			t.Fatalf("expected bounded chunk, got %d bytes", len(chunk))
		}
		if !strings.Contains(chunk, "OpenClaw memory import") {
			t.Fatalf("expected import header, got %q", chunk)
		}
	}
}
