package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"or3-intern/internal/approval"
)

type serviceFileRoot struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	Path     string `json:"path"`
	Writable bool   `json:"writable"`
}

type serviceFileEntry struct {
	Name       string `json:"name"`
	Path       string `json:"path"`
	Type       string `json:"type"`
	Size       int64  `json:"size,omitempty"`
	ModifiedAt string `json:"modified_at,omitempty"`
	MimeType   string `json:"mime_type,omitempty"`
}

type serviceFileSearchItem struct {
	serviceFileEntry
	RootID    string `json:"root_id"`
	RootLabel string `json:"root_label"`
}

type serviceFileReadResponse struct {
	RootID     string `json:"root_id"`
	Path       string `json:"path"`
	Name       string `json:"name"`
	MimeType   string `json:"mime_type,omitempty"`
	Size       int64  `json:"size"`
	ModifiedAt string `json:"modified_at"`
	Revision   string `json:"revision"`
	Writable   bool   `json:"writable"`
	Content    string `json:"content"`
}

const maxServiceFileSearchVisited = 5000

func (s *serviceServer) handleFiles(w http.ResponseWriter, r *http.Request) {
	if !requireServiceRole(w, r, approval.RoleOperator) {
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/internal/v1/files")
	path = strings.Trim(path, "/")
	switch path {
	case "roots":
		if r.Method != http.MethodGet {
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		writeServiceJSON(w, http.StatusOK, map[string]any{"items": s.serviceFileRoots()})
	case "list":
		if r.Method != http.MethodGet {
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		s.handleFileList(w, r)
	case "search":
		if r.Method != http.MethodGet {
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		s.handleFileSearch(w, r)
	case "stat":
		if r.Method != http.MethodGet {
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		s.handleFileStat(w, r)
	case "read":
		if r.Method != http.MethodGet {
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		s.handleFileRead(w, r)
	case "download":
		if r.Method != http.MethodGet {
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		s.handleFileDownload(w, r)
	case "write":
		if r.Method != http.MethodPut {
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		s.handleFileWrite(w, r)
	case "upload":
		if r.Method != http.MethodPost {
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		s.handleFileUpload(w, r)
	case "mkdir":
		if r.Method != http.MethodPost {
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		s.handleFileMkdir(w, r)
	case "delete":
		writeServiceJSON(w, http.StatusForbidden, map[string]any{"error": "file deletion is disabled in v1"})
	default:
		writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "file route not found"})
	}
}

func (s *serviceServer) serviceFileRoots() []serviceFileRoot {
	var roots []serviceFileRoot
	splitReadWrite := s.config.Tools.RestrictToWorkspace && s.config.Tools.AllowFullFileRead
	add := func(id, label, path string, writable bool) {
		path = strings.TrimSpace(path)
		if path == "" {
			return
		}
		if abs, err := filepath.Abs(path); err == nil {
			roots = append(roots, serviceFileRoot{ID: id, Label: label, Path: abs, Writable: writable})
		}
	}
	if splitReadWrite {
		add("computer", "Computer", string(filepath.Separator), false)
	}
	add("allowed", "Allowed Folder", s.config.AllowedDir, !splitReadWrite)
	add("workspace", "Workspace", s.config.WorkspaceDir, true)
	add("artifacts", "Artifacts", s.config.ArtifactsDir, false)
	if len(roots) == 0 {
		if cwd, err := os.Getwd(); err == nil {
			roots = append(roots, serviceFileRoot{ID: "cwd", Label: "Current Directory", Path: cwd, Writable: !splitReadWrite})
		}
	}
	return roots
}

func (s *serviceServer) serviceFileRootByID(id string) (serviceFileRoot, bool) {
	for _, root := range s.serviceFileRoots() {
		if root.ID == id {
			return root, true
		}
	}
	return serviceFileRoot{}, false
}

func (s *serviceServer) defaultSearchFileRoot() (serviceFileRoot, bool) {
	roots := s.serviceFileRoots()
	for _, id := range []string{"workspace", "allowed", "computer", "cwd"} {
		for _, root := range roots {
			if root.ID == id {
				return root, true
			}
		}
	}
	if len(roots) == 0 {
		return serviceFileRoot{}, false
	}
	return roots[0], true
}

func (s *serviceServer) resolveServiceFilePath(rootID, relPath string) (serviceFileRoot, string, string, error) {
	root, ok := s.serviceFileRootByID(strings.TrimSpace(rootID))
	if !ok {
		return serviceFileRoot{}, "", "", fmt.Errorf("unknown file root")
	}
	cleanRel := filepath.Clean(strings.TrimSpace(relPath))
	if cleanRel == "." || cleanRel == string(filepath.Separator) {
		cleanRel = "."
	}
	if filepath.IsAbs(cleanRel) || strings.HasPrefix(cleanRel, "..") || strings.Contains(cleanRel, string(filepath.Separator)+".."+string(filepath.Separator)) {
		return serviceFileRoot{}, "", "", fmt.Errorf("path escapes root")
	}
	absRoot, err := filepath.Abs(root.Path)
	if err != nil {
		return serviceFileRoot{}, "", "", err
	}
	realRoot, err := filepath.EvalSymlinks(absRoot)
	if err != nil {
		return serviceFileRoot{}, "", "", err
	}
	absPath, err := filepath.Abs(filepath.Join(absRoot, cleanRel))
	if err != nil {
		return serviceFileRoot{}, "", "", err
	}
	realPath, err := resolveExistingServicePath(realRoot, cleanRel)
	if err != nil {
		return serviceFileRoot{}, "", "", err
	}
	rel, err := filepath.Rel(realRoot, realPath)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return serviceFileRoot{}, "", "", fmt.Errorf("path escapes root")
	}
	displayRel, err := filepath.Rel(absRoot, absPath)
	if err != nil || displayRel == ".." || strings.HasPrefix(displayRel, ".."+string(filepath.Separator)) {
		return serviceFileRoot{}, "", "", fmt.Errorf("path escapes root")
	}
	return root, absPath, filepath.ToSlash(displayRel), nil
}

func resolveExistingServicePath(realRoot, cleanRel string) (string, error) {
	if cleanRel == "." {
		return realRoot, nil
	}
	current := realRoot
	for _, part := range strings.Split(cleanRel, string(filepath.Separator)) {
		if part == "" || part == "." {
			continue
		}
		next := filepath.Join(current, part)
		evaluated, err := filepath.EvalSymlinks(next)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return next, nil
			}
			return "", err
		}
		current = evaluated
	}
	return current, nil
}

func (s *serviceServer) handleFileList(w http.ResponseWriter, r *http.Request) {
	root, absPath, rel, err := s.resolveServiceFilePath(r.URL.Query().Get("root_id"), r.URL.Query().Get("path"))
	if err != nil {
		writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	items, err := os.ReadDir(absPath)
	if err != nil {
		writeServiceError(w, r, http.StatusNotFound, "directory unavailable", err)
		return
	}
	entries := make([]serviceFileEntry, 0, len(items))
	for _, item := range items {
		info, err := item.Info()
		if err != nil {
			continue
		}
		entryType := "file"
		if info.IsDir() {
			entryType = "directory"
		}
		entryRel := filepath.ToSlash(filepath.Join(rel, item.Name()))
		if rel == "." {
			entryRel = item.Name()
		}
		entries = append(entries, serviceFileEntry{Name: item.Name(), Path: entryRel, Type: entryType, Size: info.Size(), ModifiedAt: info.ModTime().Format(time.RFC3339), MimeType: mime.TypeByExtension(filepath.Ext(item.Name()))})
	}
	writeServiceJSON(w, http.StatusOK, map[string]any{"root_id": root.ID, "path": rel, "entries": entries})
}

func (s *serviceServer) handleFileSearch(w http.ResponseWriter, r *http.Request) {
	query := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	limit := parsePositiveInt(r.URL.Query().Get("limit"), 20)
	if limit > 50 {
		limit = 50
	}
	rootID := strings.TrimSpace(r.URL.Query().Get("root_id"))
	root := serviceFileRoot{}
	var ok bool
	if rootID == "" {
		root, ok = s.defaultSearchFileRoot()
	} else {
		root, ok = s.serviceFileRootByID(rootID)
	}
	if !ok {
		writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": "unknown file root"})
		return
	}

	_, absRoot, _, err := s.resolveServiceFilePath(root.ID, ".")
	if err != nil {
		writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}

	items := serviceFileSearchItems(absRoot, root, query, limit)

	writeServiceJSON(w, http.StatusOK, map[string]any{"root_id": root.ID, "query": query, "items": items})
}

func serviceFileSearchItems(absRoot string, root serviceFileRoot, query string, limit int) []serviceFileSearchItem {
	if limit <= 0 {
		return nil
	}

	items := make([]serviceFileSearchItem, 0, limit)
	queue := []string{absRoot}
	visited := 0

	for len(queue) > 0 && len(items) < limit && visited < maxServiceFileSearchVisited {
		dir := queue[0]
		queue = queue[1:]

		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}

		for _, entry := range entries {
			if len(items) >= limit || visited >= maxServiceFileSearchVisited {
				break
			}

			visited++
			name := entry.Name()
			childPath := filepath.Join(dir, name)
			rel, err := filepath.Rel(absRoot, childPath)
			if err != nil || rel == "." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				continue
			}
			slashRel := filepath.ToSlash(rel)

			if query == "" || fileSearchMatches(query, name, slashRel) {
				item, ok := newServiceFileSearchItem(entry, slashRel, root)
				if ok {
					items = append(items, item)
					if len(items) >= limit {
						break
					}
				}
			}

			if entry.IsDir() && !isIgnoredSearchDir(name) {
				queue = append(queue, childPath)
			}
		}
	}

	return items
}

func newServiceFileSearchItem(entry os.DirEntry, relPath string, root serviceFileRoot) (serviceFileSearchItem, bool) {
	info, err := entry.Info()
	if err != nil {
		return serviceFileSearchItem{}, false
	}

	entryType := "file"
	item := serviceFileSearchItem{
		serviceFileEntry: serviceFileEntry{
			Name:       entry.Name(),
			Path:       relPath,
			Type:       entryType,
			Size:       info.Size(),
			ModifiedAt: info.ModTime().Format(time.RFC3339),
			MimeType:   mime.TypeByExtension(filepath.Ext(entry.Name())),
		},
		RootID:    root.ID,
		RootLabel: root.Label,
	}
	if entry.IsDir() {
		item.Type = "directory"
		item.Size = 0
		item.MimeType = ""
	}

	return item, true
}

func parsePositiveInt(raw string, fallback int) int {
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}

func isIgnoredSearchDir(name string) bool {
	switch name {
	case ".git", ".hg", ".svn", "node_modules", ".nuxt", ".output", "dist", "build", "coverage", ".cache", "vendor":
		return true
	default:
		return false
	}
}

func fileSearchMatches(query, name, path string) bool {
	if query == "" {
		return true
	}
	lowerName := strings.ToLower(name)
	lowerPath := strings.ToLower(path)
	for _, token := range strings.Fields(query) {
		if !strings.Contains(lowerName, token) && !strings.Contains(lowerPath, token) {
			return false
		}
	}
	return true
}

func serviceFileRevision(info os.FileInfo) string {
	if info == nil {
		return ""
	}
	return fmt.Sprintf("%d:%d", info.ModTime().UTC().UnixNano(), info.Size())
}

func isTextLikeMime(mimeType string) bool {
	mimeType = strings.ToLower(strings.TrimSpace(mimeType))
	if strings.HasPrefix(mimeType, "text/") {
		return true
	}
	return strings.Contains(mimeType, "json") || strings.Contains(mimeType, "xml") || strings.Contains(mimeType, "javascript") || strings.Contains(mimeType, "yaml") || strings.Contains(mimeType, "toml")
}

func isTextLikeExtension(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".md", ".markdown", ".txt", ".json", ".yaml", ".yml", ".toml", ".ini", ".cfg", ".conf", ".env", ".csv", ".ts", ".tsx", ".js", ".jsx", ".vue", ".go", ".py", ".rb", ".php", ".java", ".kt", ".swift", ".sql", ".html", ".css", ".scss", ".sh":
		return true
	default:
		return false
	}
}

func serviceFileTextAllowed(name, mimeType string, content []byte) bool {
	if bytes.IndexByte(content, 0) >= 0 || !utf8.Valid(content) {
		return false
	}
	if isTextLikeMime(mimeType) || isTextLikeExtension(name) {
		return true
	}
	return false
}

func (s *serviceServer) handleFileStat(w http.ResponseWriter, r *http.Request) {
	root, absPath, rel, err := s.resolveServiceFilePath(r.URL.Query().Get("root_id"), r.URL.Query().Get("path"))
	if err != nil {
		writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	info, err := os.Stat(absPath)
	if err != nil {
		writeServiceError(w, r, http.StatusNotFound, "file unavailable", err)
		return
	}
	entryType := "file"
	if info.IsDir() {
		entryType = "directory"
	}
	writeServiceJSON(w, http.StatusOK, map[string]any{"item": serviceFileEntry{Name: info.Name(), Path: rel, Type: entryType, Size: info.Size(), ModifiedAt: info.ModTime().Format(time.RFC3339), MimeType: mime.TypeByExtension(filepath.Ext(info.Name()))}, "root_id": root.ID})
}

func (s *serviceServer) handleFileRead(w http.ResponseWriter, r *http.Request) {
	root, absPath, rel, err := s.resolveServiceFilePath(r.URL.Query().Get("root_id"), r.URL.Query().Get("path"))
	if err != nil {
		writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	info, err := os.Stat(absPath)
	if err != nil {
		writeServiceError(w, r, http.StatusNotFound, "file unavailable", err)
		return
	}
	if info.IsDir() {
		writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": "read target is not a file"})
		return
	}
	if info.Size() > serviceFileTextReadLimit {
		writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": "file is too large for inline editing"})
		return
	}
	content, err := os.ReadFile(absPath)
	if err != nil {
		writeServiceError(w, r, http.StatusBadGateway, "file read failed", err)
		return
	}
	mimeType := mime.TypeByExtension(filepath.Ext(info.Name()))
	if !serviceFileTextAllowed(info.Name(), mimeType, content) {
		writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": "file is not a supported text document"})
		return
	}
	writeServiceJSON(w, http.StatusOK, map[string]any{
		"root_id":     root.ID,
		"path":        rel,
		"name":        info.Name(),
		"mime_type":   mimeType,
		"size":        info.Size(),
		"modified_at": info.ModTime().UTC().Format(time.RFC3339),
		"revision":    serviceFileRevision(info),
		"writable":    root.Writable,
		"content":     string(content),
	})
}

func (s *serviceServer) handleFileDownload(w http.ResponseWriter, r *http.Request) {
	_, absPath, _, err := s.resolveServiceFilePath(r.URL.Query().Get("root_id"), r.URL.Query().Get("path"))
	if err != nil {
		writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	file, err := os.Open(absPath)
	if err != nil {
		writeServiceError(w, r, http.StatusNotFound, "file unavailable", err)
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || info.IsDir() {
		writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": "download target is not a file"})
		return
	}
	http.ServeContent(w, r, info.Name(), info.ModTime(), file)
}

func (s *serviceServer) handleFileWrite(w http.ResponseWriter, r *http.Request) {
	limitServiceRequestBody(w, r, serviceFileTextWriteLimit)
	var body struct {
		RootID           string `json:"root_id"`
		Path             string `json:"path"`
		Content          string `json:"content"`
		ExpectedRevision string `json:"expected_revision"`
		Create           bool   `json:"create"`
	}
	if err := decodeServiceRequestBody(r.Body, &body); err != nil {
		writeServiceRequestDecodeError(w, err)
		return
	}
	root, absPath, rel, err := s.resolveServiceFilePath(body.RootID, body.Path)
	if err != nil {
		writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	if !root.Writable {
		writeServiceJSON(w, http.StatusForbidden, map[string]any{"error": "file root is read-only"})
		return
	}
	contentBytes := []byte(body.Content)
	if int64(len(contentBytes)) > serviceFileTextWriteLimit {
		writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": "file is too large for inline editing"})
		return
	}
	name := filepath.Base(absPath)
	mimeType := mime.TypeByExtension(filepath.Ext(name))
	if !serviceFileTextAllowed(name, mimeType, contentBytes) {
		writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": "file is not a supported text document"})
		return
	}
	parent := filepath.Dir(absPath)
	parentInfo, err := os.Stat(parent)
	if err != nil || !parentInfo.IsDir() {
		writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": "parent directory does not exist"})
		return
	}
	existingInfo, statErr := os.Stat(absPath)
	if statErr == nil && existingInfo.IsDir() {
		writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": "write target is not a file"})
		return
	}
	status := http.StatusOK
	resultStatus := "written"
	if statErr != nil {
		if !errors.Is(statErr, os.ErrNotExist) {
			writeServiceError(w, r, http.StatusBadGateway, "file stat failed", statErr)
			return
		}
		if !body.Create {
			writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "file does not exist"})
			return
		}
		status = http.StatusCreated
		resultStatus = "created"
	} else {
		if body.Create {
			writeServiceJSON(w, http.StatusConflict, map[string]any{"error": "file already exists"})
			return
		}
		if body.ExpectedRevision != "" && body.ExpectedRevision != serviceFileRevision(existingInfo) {
			writeServiceJSON(w, http.StatusConflict, map[string]any{
				"error":            "file has changed on disk",
				"modified_at":      existingInfo.ModTime().UTC().Format(time.RFC3339),
				"current_revision": serviceFileRevision(existingInfo),
			})
			return
		}
	}
	tmp, err := os.CreateTemp(parent, ".or3-write-*")
	if err != nil {
		writeServiceError(w, r, http.StatusBadGateway, "could not prepare file write", err)
		return
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(contentBytes); err != nil {
		_ = tmp.Close()
		writeServiceError(w, r, http.StatusBadGateway, "file write failed", err)
		return
	}
	if err := tmp.Close(); err != nil {
		writeServiceError(w, r, http.StatusBadGateway, "file write failed", err)
		return
	}
	if err := os.Rename(tmpName, absPath); err != nil {
		writeServiceError(w, r, http.StatusBadGateway, "file write failed", err)
		return
	}
	updatedInfo, err := os.Stat(absPath)
	if err != nil {
		writeServiceError(w, r, http.StatusBadGateway, "file stat failed", err)
		return
	}
	writeServiceJSON(w, status, map[string]any{
		"root_id":     root.ID,
		"path":        rel,
		"status":      resultStatus,
		"modified_at": updatedInfo.ModTime().UTC().Format(time.RFC3339),
		"revision":    serviceFileRevision(updatedInfo),
	})
}

func (s *serviceServer) handleFileUpload(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, serviceFileUploadBodyLimit)
	if err := r.ParseMultipartForm(serviceFileUploadBodyLimit); err != nil {
		writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid multipart upload"})
		return
	}
	root, dirPath, rel, err := s.resolveServiceFilePath(r.FormValue("root_id"), r.FormValue("path"))
	if err != nil {
		writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	if !root.Writable {
		writeServiceJSON(w, http.StatusForbidden, map[string]any{"error": "file root is read-only"})
		return
	}
	source, header, err := r.FormFile("file")
	if err != nil {
		writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": "missing file"})
		return
	}
	defer source.Close()
	name := filepath.Base(header.Filename)
	if name == "." || name == ".." || name == string(filepath.Separator) || name == "" {
		writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid file name"})
		return
	}
	target := filepath.Join(dirPath, name)
	out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		writeServiceError(w, r, http.StatusConflict, "file already exists or cannot be created", err)
		return
	}
	defer out.Close()
	if _, err := io.Copy(out, source); err != nil {
		writeServiceError(w, r, http.StatusBadGateway, "file upload failed", err)
		return
	}
	writeServiceJSON(w, http.StatusCreated, map[string]any{"root_id": root.ID, "path": filepath.ToSlash(filepath.Join(rel, name)), "status": "uploaded"})
}

func (s *serviceServer) handleFileMkdir(w http.ResponseWriter, r *http.Request) {
	limitServiceRequestBody(w, r, serviceApprovalBodyLimit)
	var body struct {
		RootID string `json:"root_id"`
		Path   string `json:"path"`
		Name   string `json:"name"`
	}
	if err := decodeServiceRequestBody(r.Body, &body); err != nil {
		writeServiceRequestDecodeError(w, err)
		return
	}
	root, dirPath, rel, err := s.resolveServiceFilePath(body.RootID, body.Path)
	if err != nil {
		writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	if !root.Writable {
		writeServiceJSON(w, http.StatusForbidden, map[string]any{"error": "file root is read-only"})
		return
	}
	name := filepath.Base(strings.TrimSpace(body.Name))
	if name == "." || name == string(filepath.Separator) || name == "" {
		writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid directory name"})
		return
	}
	target := filepath.Join(dirPath, name)
	if err := os.Mkdir(target, 0o700); err != nil {
		writeServiceError(w, r, http.StatusConflict, "directory already exists or cannot be created", err)
		return
	}
	writeServiceJSON(w, http.StatusCreated, map[string]any{"root_id": root.ID, "path": filepath.ToSlash(filepath.Join(rel, name)), "status": "created"})
}
