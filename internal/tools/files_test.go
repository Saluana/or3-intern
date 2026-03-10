package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---- safePath ----

func TestSafePath_Valid(t *testing.T) {
	tool := &FileTool{}
	dir := t.TempDir()
	path, err := tool.safePath(filepath.Join(dir, "file.txt"))
	if err != nil {
		t.Fatalf("safePath: %v", err)
	}
	if path == "" {
		t.Error("expected non-empty path")
	}
}

func TestSafePath_Empty(t *testing.T) {
	tool := &FileTool{}
	_, err := tool.safePath("")
	if err == nil {
		t.Fatal("expected error for empty path")
	}
}

func TestSafePath_Whitespace(t *testing.T) {
	tool := &FileTool{}
	_, err := tool.safePath("   ")
	if err == nil {
		t.Fatal("expected error for whitespace-only path")
	}
}

func TestSafePath_OutsideRoot(t *testing.T) {
	dir := t.TempDir()
	tool := &FileTool{Root: dir}
	_, err := tool.safePath("/tmp")
	if err == nil {
		t.Fatal("expected error for path outside root")
	}
	if !strings.Contains(err.Error(), "outside allowed root") {
		t.Errorf("expected 'outside allowed root', got %q", err.Error())
	}
}

func TestSafePath_InsideRoot(t *testing.T) {
	dir := t.TempDir()
	tool := &FileTool{Root: dir}
	path, err := tool.safePath(filepath.Join(dir, "subdir", "file.txt"))
	if err != nil {
		t.Fatalf("safePath: %v", err)
	}
	if path == "" {
		t.Error("expected non-empty path")
	}
}

func TestSafePath_BlocksSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	link := filepath.Join(root, "escape")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	tool := &FileTool{Root: root}
	_, err := tool.safePath(filepath.Join(link, "secret.txt"))
	if err == nil {
		t.Fatal("expected symlink escape to be blocked")
	}
}

// ---- ReadFile ----

func TestReadFile_OK(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "test.txt")
	os.WriteFile(p, []byte("hello file"), 0o644)

	tool := &ReadFile{}
	out, err := tool.Execute(context.Background(), map[string]any{"path": p})
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if out != "hello file" {
		t.Errorf("expected 'hello file', got %q", out)
	}
}

func TestReadFile_NotFound(t *testing.T) {
	tool := &ReadFile{}
	_, err := tool.Execute(context.Background(), map[string]any{"path": "/nonexistent/file.txt"})
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestReadFile_Truncation(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "large.txt")
	os.WriteFile(p, []byte(strings.Repeat("x", 200)), 0o644)

	tool := &ReadFile{}
	out, err := tool.Execute(context.Background(), map[string]any{
		"path":     p,
		"maxBytes": float64(100),
	})
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if len(out) != 100 {
		t.Errorf("expected 100 bytes, got %d", len(out))
	}
}

func TestReadFile_EmptyPath(t *testing.T) {
	tool := &ReadFile{}
	_, err := tool.Execute(context.Background(), map[string]any{"path": ""})
	if err == nil {
		t.Fatal("expected error for empty path")
	}
}

func TestReadFile_Name(t *testing.T) {
	tool := &ReadFile{}
	if tool.Name() != "read_file" {
		t.Errorf("expected 'read_file', got %q", tool.Name())
	}
}

func TestReadFile_Schema(t *testing.T) {
	tool := &ReadFile{}
	schema := tool.Schema()
	if schema["type"] != "function" {
		t.Errorf("expected 'function', got %v", schema["type"])
	}
}

// ---- WriteFile ----

func TestWriteFile_OK(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "out.txt")

	tool := &WriteFile{}
	out, err := tool.Execute(context.Background(), map[string]any{
		"path":    p,
		"content": "written content",
	})
	if err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if out != "ok" {
		t.Errorf("expected 'ok', got %q", out)
	}
	got, _ := os.ReadFile(p)
	if string(got) != "written content" {
		t.Errorf("expected 'written content', got %q", string(got))
	}
}

func TestWriteFile_Mkdirs(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a", "b", "c", "file.txt")

	tool := &WriteFile{}
	out, err := tool.Execute(context.Background(), map[string]any{
		"path":    p,
		"content": "content",
		"mkdirs":  true,
	})
	if err != nil {
		t.Fatalf("WriteFile with mkdirs: %v", err)
	}
	if out != "ok" {
		t.Errorf("expected 'ok', got %q", out)
	}
	info, err := os.Stat(filepath.Dir(p))
	if err != nil {
		t.Fatalf("Stat parent: %v", err)
	}
	if info.Mode().Perm() != 0o700 {
		t.Fatalf("expected parent dir mode 0700, got %#o", info.Mode().Perm())
	}
	fileInfo, err := os.Stat(p)
	if err != nil {
		t.Fatalf("Stat file: %v", err)
	}
	if fileInfo.Mode().Perm() != 0o600 {
		t.Fatalf("expected file mode 0600, got %#o", fileInfo.Mode().Perm())
	}
}

func TestWriteFile_NoMkdirs_FailsOnMissingDir(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "missing", "file.txt")

	tool := &WriteFile{}
	_, err := tool.Execute(context.Background(), map[string]any{
		"path":    p,
		"content": "content",
	})
	if err == nil {
		t.Fatal("expected error when parent dir is missing and mkdirs=false")
	}
}

func TestWriteFile_Name(t *testing.T) {
	tool := &WriteFile{}
	if tool.Name() != "write_file" {
		t.Errorf("expected 'write_file', got %q", tool.Name())
	}
}

func TestEditFile_PreservesExistingMode(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "script.sh")
	if err := os.WriteFile(p, []byte("echo one\n"), 0o755); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	tool := &EditFile{}
	if _, err := tool.Execute(context.Background(), map[string]any{
		"path": p,
		"edits": []any{
			map[string]any{"find": "one", "replace": "two"},
		},
	}); err != nil {
		t.Fatalf("EditFile: %v", err)
	}
	info, err := os.Stat(p)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Fatalf("expected executable mode to be preserved, got %#o", info.Mode().Perm())
	}
}

// ---- EditFile ----

func TestEditFile_ReplaceAll(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "edit.txt")
	os.WriteFile(p, []byte("foo foo foo"), 0o644)

	tool := &EditFile{}
	out, err := tool.Execute(context.Background(), map[string]any{
		"path": p,
		"edits": []any{
			map[string]any{"find": "foo", "replace": "bar"},
		},
	})
	if err != nil {
		t.Fatalf("EditFile: %v", err)
	}
	if out != "ok" {
		t.Errorf("expected 'ok', got %q", out)
	}
	got, _ := os.ReadFile(p)
	if string(got) != "bar bar bar" {
		t.Errorf("expected 'bar bar bar', got %q", string(got))
	}
}

func TestEditFile_ReplaceCount(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "edit.txt")
	os.WriteFile(p, []byte("foo foo foo"), 0o644)

	tool := &EditFile{}
	tool.Execute(context.Background(), map[string]any{
		"path": p,
		"edits": []any{
			map[string]any{"find": "foo", "replace": "bar", "count": float64(1)},
		},
	})
	got, _ := os.ReadFile(p)
	if string(got) != "bar foo foo" {
		t.Errorf("expected 'bar foo foo', got %q", string(got))
	}
}

func TestEditFile_NotFound(t *testing.T) {
	tool := &EditFile{}
	_, err := tool.Execute(context.Background(), map[string]any{
		"path":  "/nonexistent/file.txt",
		"edits": []any{},
	})
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestEditFile_Name(t *testing.T) {
	tool := &EditFile{}
	if tool.Name() != "edit_file" {
		t.Errorf("expected 'edit_file', got %q", tool.Name())
	}
}

func TestEditFile_NoEdits(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "file.txt")
	os.WriteFile(p, []byte("original"), 0o644)

	tool := &EditFile{}
	out, err := tool.Execute(context.Background(), map[string]any{
		"path":  p,
		"edits": []any{},
	})
	if err != nil {
		t.Fatalf("EditFile no edits: %v", err)
	}
	if out != "ok" {
		t.Errorf("expected 'ok', got %q", out)
	}
	got, _ := os.ReadFile(p)
	if string(got) != "original" {
		t.Errorf("expected unchanged 'original', got %q", string(got))
	}
}

// ---- ListDir ----

func TestListDir_OK(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o644)
	os.WriteFile(filepath.Join(dir, "b.txt"), []byte("b"), 0o644)
	os.MkdirAll(filepath.Join(dir, "subdir"), 0o755)

	tool := &ListDir{}
	out, err := tool.Execute(context.Background(), map[string]any{"path": dir})
	if err != nil {
		t.Fatalf("ListDir: %v", err)
	}

	var entries []struct {
		Name  string `json:"name"`
		IsDir bool   `json:"isDir"`
		Size  int64  `json:"size"`
	}
	if err := json.Unmarshal([]byte(out), &entries); err != nil {
		t.Fatalf("parse output: %v", err)
	}
	if len(entries) != 3 {
		t.Errorf("expected 3 entries, got %d", len(entries))
	}
}

func TestListDir_MaxEntries(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 5; i++ {
		os.WriteFile(filepath.Join(dir, []string{"a", "b", "c", "d", "e"}[i]+".txt"), []byte("x"), 0o644)
	}

	tool := &ListDir{}
	out, err := tool.Execute(context.Background(), map[string]any{
		"path": dir,
		"max":  float64(2),
	})
	if err != nil {
		t.Fatalf("ListDir: %v", err)
	}
	var entries []any
	json.Unmarshal([]byte(out), &entries)
	if len(entries) != 2 {
		t.Errorf("expected 2 entries with max=2, got %d", len(entries))
	}
}

func TestListDir_NotFound(t *testing.T) {
	tool := &ListDir{}
	_, err := tool.Execute(context.Background(), map[string]any{"path": "/nonexistent/directory"})
	if err == nil {
		t.Fatal("expected error for missing directory")
	}
}

func TestListDir_Name(t *testing.T) {
	tool := &ListDir{}
	if tool.Name() != "list_dir" {
		t.Errorf("expected 'list_dir', got %q", tool.Name())
	}
}

func TestWriteFile_Description(t *testing.T) {
	tool := &WriteFile{}
	if tool.Description() == "" {
		t.Error("expected non-empty description")
	}
}

func TestWriteFile_Parameters(t *testing.T) {
	tool := &WriteFile{}
	params := tool.Parameters()
	if params["type"] != "object" {
		t.Errorf("expected 'object', got %v", params["type"])
	}
}

func TestEditFile_Description(t *testing.T) {
	tool := &EditFile{}
	if tool.Description() == "" {
		t.Error("expected non-empty description")
	}
}

func TestEditFile_Parameters(t *testing.T) {
	tool := &EditFile{}
	params := tool.Parameters()
	if params["type"] != "object" {
		t.Errorf("expected 'object', got %v", params["type"])
	}
}

func TestEditFile_Schema(t *testing.T) {
	tool := &EditFile{}
	schema := tool.Schema()
	if schema["type"] != "function" {
		t.Errorf("expected 'function', got %v", schema["type"])
	}
}

func TestListDir_Description(t *testing.T) {
	tool := &ListDir{}
	if tool.Description() == "" {
		t.Error("expected non-empty description")
	}
}

func TestListDir_Parameters(t *testing.T) {
	tool := &ListDir{}
	params := tool.Parameters()
	if params["type"] != "object" {
		t.Errorf("expected 'object', got %v", params["type"])
	}
}

func TestListDir_Schema(t *testing.T) {
	tool := &ListDir{}
	schema := tool.Schema()
	if schema["type"] != "function" {
		t.Errorf("expected 'function', got %v", schema["type"])
	}
}

func TestReadFile_Description(t *testing.T) {
	tool := &ReadFile{}
	if tool.Description() == "" {
		t.Error("expected non-empty description")
	}
}

func TestReadFile_Parameters(t *testing.T) {
	tool := &ReadFile{}
	params := tool.Parameters()
	if params["type"] != "object" {
		t.Errorf("expected 'object', got %v", params["type"])
	}
}

func TestWriteFile_Schema(t *testing.T) {
	tool := &WriteFile{}
	schema := tool.Schema()
	if schema["type"] != "function" {
		t.Errorf("expected 'function', got %v", schema["type"])
	}
}
