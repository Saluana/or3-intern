package memory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
