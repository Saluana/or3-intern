package memory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBuildWorkspaceContext_CapsLargeFileReads(t *testing.T) {
	workspace := t.TempDir()
	large := filepath.Join(workspace, "README.md")
	content := strings.Repeat("a", 8192)
	if err := os.WriteFile(large, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	got := BuildWorkspaceContext(WorkspaceContextConfig{
		WorkspaceDir: workspace,
		MaxFileBytes: 128,
		MaxChars:     4096,
	}, "readme")
	if got == "" {
		t.Fatal("expected workspace context")
	}
	if strings.Contains(got, strings.Repeat("a", 512)) {
		t.Fatalf("expected output to respect MaxFileBytes cap, got %q", got)
	}
}

func TestBuildWorkspaceContext_PrefersRecentRelevantFiles(t *testing.T) {
	workspace := t.TempDir()
	older := filepath.Join(workspace, "deploy-old.md")
	newer := filepath.Join(workspace, "deploy-new.md")
	if err := os.WriteFile(older, []byte("deploy checklist and rollout notes"), 0o644); err != nil {
		t.Fatalf("WriteFile older: %v", err)
	}
	if err := os.WriteFile(newer, []byte("deploy checklist and rollout notes"), 0o644); err != nil {
		t.Fatalf("WriteFile newer: %v", err)
	}
	oldTime := time.Now().Add(-14 * 24 * time.Hour)
	newTime := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(older, oldTime, oldTime); err != nil {
		t.Fatalf("Chtimes older: %v", err)
	}
	if err := os.Chtimes(newer, newTime, newTime); err != nil {
		t.Fatalf("Chtimes newer: %v", err)
	}

	got := BuildWorkspaceContext(WorkspaceContextConfig{WorkspaceDir: workspace, MaxResults: 2}, "deploy rollout")
	firstOld := strings.Index(got, "deploy-old.md")
	firstNew := strings.Index(got, "deploy-new.md")
	if firstNew < 0 || firstOld < 0 {
		t.Fatalf("expected both files in output, got %q", got)
	}
	if firstNew > firstOld {
		t.Fatalf("expected newer relevant file first, got %q", got)
	}
}
