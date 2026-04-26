package main

import (
	"os"
	"path/filepath"
	"testing"

	"or3-intern/internal/config"
)

func TestServiceFileRootsUseConfiguredDirs(t *testing.T) {
	tmp := t.TempDir()
	server := &serviceServer{config: config.Config{AllowedDir: tmp}}

	roots := server.serviceFileRoots()
	if len(roots) != 1 {
		t.Fatalf("expected one root, got %d", len(roots))
	}
	if roots[0].ID != "allowed" || !roots[0].Writable {
		t.Fatalf("unexpected root: %+v", roots[0])
	}
}

func TestResolveServiceFilePathRejectsTraversal(t *testing.T) {
	tmp := t.TempDir()
	server := &serviceServer{config: config.Config{AllowedDir: tmp}}

	_, _, _, err := server.resolveServiceFilePath("allowed", "../secret.txt")
	if err == nil {
		t.Fatal("expected traversal to be rejected")
	}
}

func TestResolveServiceFilePathKeepsPathInsideRoot(t *testing.T) {
	tmp := t.TempDir()
	server := &serviceServer{config: config.Config{AllowedDir: tmp}}

	_, absPath, relPath, err := server.resolveServiceFilePath("allowed", "notes/today.md")
	if err != nil {
		t.Fatalf("resolve path: %v", err)
	}
	expected := filepath.Join(tmp, "notes", "today.md")
	if absPath != expected {
		t.Fatalf("expected %q, got %q", expected, absPath)
	}
	if relPath != "notes/today.md" {
		t.Fatalf("expected slash rel path, got %q", relPath)
	}
}

func TestResolveServiceFilePathRejectsSymlinkEscape(t *testing.T) {
	tmp := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("secret"), 0o600); err != nil {
		t.Fatalf("write outside file: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(tmp, "outside")); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	server := &serviceServer{config: config.Config{AllowedDir: tmp}}

	_, _, _, err := server.resolveServiceFilePath("allowed", "outside/secret.txt")
	if err == nil {
		t.Fatal("expected symlink escape to be rejected")
	}
}
