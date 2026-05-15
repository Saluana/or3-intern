package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
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

func TestServiceFileRootsExposeComputerReadOnlyWhenFullReadEnabled(t *testing.T) {
	workspace := t.TempDir()
	allowed := t.TempDir()
	server := &serviceServer{config: config.Config{
		WorkspaceDir: workspace,
		AllowedDir:   allowed,
		Tools: config.ToolsConfig{
			RestrictToWorkspace: true,
			AllowFullFileRead:   true,
		},
	}}

	roots := server.serviceFileRoots()
	byID := map[string]serviceFileRoot{}
	for _, root := range roots {
		byID[root.ID] = root
	}
	if root, ok := byID["computer"]; !ok || root.Writable {
		t.Fatalf("expected read-only computer root, got %+v", root)
	}
	if root, ok := byID["allowed"]; !ok || root.Writable {
		t.Fatalf("expected allowed root to become read-only, got %+v", root)
	}
	if root, ok := byID["workspace"]; !ok || !root.Writable {
		t.Fatalf("expected writable workspace root, got %+v", root)
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

func TestResolveServiceFilePathAllowsDotPrefixedFilename(t *testing.T) {
	tmp := t.TempDir()
	server := &serviceServer{config: config.Config{AllowedDir: tmp}}

	_, absPath, relPath, err := server.resolveServiceFilePath("allowed", "..notes.md")
	if err != nil {
		t.Fatalf("resolve dot-prefixed filename: %v", err)
	}
	if absPath != filepath.Join(tmp, "..notes.md") {
		t.Fatalf("unexpected absolute path: %q", absPath)
	}
	if relPath != "..notes.md" {
		t.Fatalf("unexpected relative path: %q", relPath)
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

func TestHandleFileUploadRejectsParentDirectoryFilename(t *testing.T) {
	tmp := t.TempDir()
	uploadDir := filepath.Join(tmp, "uploads")
	if err := os.Mkdir(uploadDir, 0o755); err != nil {
		t.Fatalf("create upload dir: %v", err)
	}
	server := &serviceServer{config: config.Config{AllowedDir: uploadDir}}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("root_id", "allowed"); err != nil {
		t.Fatalf("write root field: %v", err)
	}
	if err := writer.WriteField("path", "."); err != nil {
		t.Fatalf("write path field: %v", err)
	}
	part, err := writer.CreateFormFile("file", "..")
	if err != nil {
		t.Fatalf("create file part: %v", err)
	}
	if _, err := part.Write([]byte("payload")); err != nil {
		t.Fatalf("write file part: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/internal/v1/files/upload", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rec := httptest.NewRecorder()

	server.handleFileUpload(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, rec.Code)
	}
	info, err := os.Stat(tmp)
	if err != nil {
		t.Fatalf("stat parent dir: %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("expected parent path to remain a directory")
	}
}

func TestFileSearchMatchesAllQueryTokens(t *testing.T) {
	if !fileSearchMatches("runtime go", "runtime.go", "internal/agent/runtime.go") {
		t.Fatal("expected filename/path tokens to match")
	}
	if fileSearchMatches("runtime missing", "runtime.go", "internal/agent/runtime.go") {
		t.Fatal("expected unmatched token to fail")
	}
}

func TestHandleFileSearchReturnsMatchingDirectories(t *testing.T) {
	tmp := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmp, ".openclaw", "state"), 0o755); err != nil {
		t.Fatalf("mkdir hidden dir: %v", err)
	}
	server := &serviceServer{config: config.Config{AllowedDir: tmp}}
	req := httptest.NewRequest(http.MethodGet, "/internal/v1/files/search?root_id=allowed&q=.openclaw", nil)
	rec := httptest.NewRecorder()

	server.handleFileSearch(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var payload struct {
		Items []serviceFileSearchItem `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(payload.Items) == 0 {
		t.Fatal("expected search to return the matching directory")
	}
	if payload.Items[0].Path != ".openclaw" || payload.Items[0].Type != "directory" {
		t.Fatalf("expected hidden directory match first, got %+v", payload.Items[0])
	}
}

func TestHandleFileSearchChecksSiblingDirectoriesBeforeDeepTree(t *testing.T) {
	tmp := t.TempDir()
	largeDir := filepath.Join(tmp, ".android")
	if err := os.MkdirAll(largeDir, 0o755); err != nil {
		t.Fatalf("mkdir large dir: %v", err)
	}
	for index := 0; index < maxServiceFileSearchVisited+100; index++ {
		path := filepath.Join(largeDir, fmt.Sprintf("entry-%04d.txt", index))
		if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
			t.Fatalf("write large tree entry %d: %v", index, err)
		}
	}
	if err := os.Mkdir(filepath.Join(tmp, ".openclaw"), 0o755); err != nil {
		t.Fatalf("mkdir target dir: %v", err)
	}

	server := &serviceServer{config: config.Config{AllowedDir: tmp}}
	req := httptest.NewRequest(http.MethodGet, "/internal/v1/files/search?root_id=allowed&q=.openclaw&limit=5", nil)
	rec := httptest.NewRecorder()

	server.handleFileSearch(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var payload struct {
		Items []serviceFileSearchItem `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(payload.Items) == 0 {
		t.Fatal("expected search to find the sibling hidden directory")
	}
	if payload.Items[0].Path != ".openclaw" || payload.Items[0].Type != "directory" {
		t.Fatalf("expected .openclaw directory despite earlier deep tree, got %+v", payload.Items[0])
	}
}

func TestHandleFileReadReturnsTextContentAndRevision(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "note.md")
	if err := os.WriteFile(path, []byte("# hello\n"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}
	server := &serviceServer{config: config.Config{AllowedDir: tmp}}
	req := httptest.NewRequest(http.MethodGet, "/internal/v1/files/read?root_id=allowed&path=note.md", nil)
	rec := httptest.NewRecorder()

	server.handleFileRead(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload["content"] != "# hello\n" {
		t.Fatalf("unexpected content: %#v", payload["content"])
	}
	if payload["revision"] == "" {
		t.Fatal("expected revision")
	}
}

func TestHandleFileReadRejectsBinaryContent(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "payload.bin")
	if err := os.WriteFile(path, []byte{0x00, 0x01, 0x02}, 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}
	server := &serviceServer{config: config.Config{AllowedDir: tmp}}
	req := httptest.NewRequest(http.MethodGet, "/internal/v1/files/read?root_id=allowed&path=payload.bin", nil)
	rec := httptest.NewRecorder()

	server.handleFileRead(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestHandleFileReadRejectsUnsupportedTextFile(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "payload.bin")
	if err := os.WriteFile(path, []byte("plain text but unsupported extension"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}
	server := &serviceServer{config: config.Config{AllowedDir: tmp}}
	req := httptest.NewRequest(http.MethodGet, "/internal/v1/files/read?root_id=allowed&path=payload.bin", nil)
	rec := httptest.NewRecorder()

	server.handleFileRead(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d (%s)", rec.Code, rec.Body.String())
	}
}

func TestHandleFileWriteCreatesAndOverwritesTextFiles(t *testing.T) {
	tmp := t.TempDir()
	server := &serviceServer{config: config.Config{AllowedDir: tmp}}
	body, _ := json.Marshal(map[string]any{
		"root_id": "allowed",
		"path":    "notes/today.md",
		"content": "hello world\n",
		"create":  true,
	})
	if err := os.MkdirAll(filepath.Join(tmp, "notes"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	req := httptest.NewRequest(http.MethodPut, "/internal/v1/files/write", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	server.handleFileWrite(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d (%s)", rec.Code, rec.Body.String())
	}
	written, err := os.ReadFile(filepath.Join(tmp, "notes", "today.md"))
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if string(written) != "hello world\n" {
		t.Fatalf("unexpected file contents: %q", string(written))
	}

	info, err := os.Stat(filepath.Join(tmp, "notes", "today.md"))
	if err != nil {
		t.Fatalf("stat file: %v", err)
	}
	secondBody, _ := json.Marshal(map[string]any{
		"root_id":           "allowed",
		"path":              "notes/today.md",
		"content":           "updated\n",
		"expected_revision": serviceFileRevision(info),
	})
	req = httptest.NewRequest(http.MethodPut, "/internal/v1/files/write", bytes.NewReader(secondBody))
	rec = httptest.NewRecorder()

	server.handleFileWrite(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	written, err = os.ReadFile(filepath.Join(tmp, "notes", "today.md"))
	if err != nil {
		t.Fatalf("read updated file: %v", err)
	}
	if string(written) != "updated\n" {
		t.Fatalf("unexpected updated contents: %q", string(written))
	}
}

func TestHandleFileWriteRejectsStaleRevision(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "note.md")
	if err := os.WriteFile(path, []byte("one\n"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}
	server := &serviceServer{config: config.Config{AllowedDir: tmp}}
	body, _ := json.Marshal(map[string]any{
		"root_id":           "allowed",
		"path":              "note.md",
		"content":           "two\n",
		"expected_revision": "stale",
	})
	req := httptest.NewRequest(http.MethodPut, "/internal/v1/files/write", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	server.handleFileWrite(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d (%s)", rec.Code, rec.Body.String())
	}
}

func TestHandleFileWriteRejectsReadonlyRoot(t *testing.T) {
	workspace := t.TempDir()
	allowed := t.TempDir()
	if err := os.MkdirAll(filepath.Join(allowed, "notes"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	server := &serviceServer{config: config.Config{
		WorkspaceDir: workspace,
		AllowedDir:   allowed,
		Tools: config.ToolsConfig{
			RestrictToWorkspace: true,
			AllowFullFileRead:   true,
		},
	}}
	body, _ := json.Marshal(map[string]any{
		"root_id": "allowed",
		"path":    "notes/today.md",
		"content": "hello\n",
		"create":  true,
	})
	req := httptest.NewRequest(http.MethodPut, "/internal/v1/files/write", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	server.handleFileWrite(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d (%s)", rec.Code, rec.Body.String())
	}
}

func TestHandleFileWriteRejectsUnsupportedTextFile(t *testing.T) {
	tmp := t.TempDir()
	server := &serviceServer{config: config.Config{AllowedDir: tmp}}
	body, _ := json.Marshal(map[string]any{
		"root_id": "allowed",
		"path":    "payload.bin",
		"content": "plain text but unsupported extension",
		"create":  true,
	})
	req := httptest.NewRequest(http.MethodPut, "/internal/v1/files/write", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	server.handleFileWrite(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d (%s)", rec.Code, rec.Body.String())
	}
	if _, err := os.Stat(filepath.Join(tmp, "payload.bin")); !os.IsNotExist(err) {
		t.Fatalf("expected unsupported file not to be written, stat err=%v", err)
	}
}

func TestDefaultSearchFileRootPrefersWorkspace(t *testing.T) {
	tmp := t.TempDir()
	workspace := filepath.Join(tmp, "workspace")
	allowed := filepath.Join(tmp, "allowed")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if err := os.MkdirAll(allowed, 0o755); err != nil {
		t.Fatalf("create allowed: %v", err)
	}
	server := &serviceServer{config: config.Config{AllowedDir: allowed, WorkspaceDir: workspace}}

	root, ok := server.defaultSearchFileRoot()
	if !ok {
		t.Fatal("expected default root")
	}
	if root.ID != "workspace" {
		t.Fatalf("expected workspace root, got %+v", root)
	}
}
