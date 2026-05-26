package tools

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

type FileTool struct {
	Base
	Root      string // allowed read root (optional)
	WriteRoot string // allowed write root (optional; falls back to Root)
}

const (
	defaultReadFileMaxBytes  = 64 * 1024
	defaultListDirMaxEntries = 80
)

func (t *FileTool) safePath(p string) (string, error) {
	return t.safePathForRoot(p, t.Root)
}

func (t *FileTool) safeWritePath(p string) (string, error) {
	return t.safePathForRoot(p, t.effectiveWriteRoot())
}

func (t *FileTool) safePathForRoot(p, rootPath string) (string, error) {
	if strings.TrimSpace(p) == "" {
		return "", errors.New("missing path")
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	abs, err = CanonicalizePath(abs)
	if err != nil {
		return "", err
	}
	if rootPath != "" {
		root, err := filepath.Abs(rootPath)
		if err != nil {
			return "", err
		}
		root, err = CanonicalizeRoot(root)
		if err != nil {
			return "", err
		}
		rel, err := filepath.Rel(root, abs)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return "", fmt.Errorf("path outside allowed root")
		}
	}
	return abs, nil
}

func (t *FileTool) validatePathInRoot(abs string) error {
	return validatePathInRoot(t.Root, abs)
}

func (t *FileTool) validatePathInWriteRoot(abs string) error {
	return validatePathInRoot(t.effectiveWriteRoot(), abs)
}

func (t *FileTool) effectiveWriteRoot() string {
	if strings.TrimSpace(t.WriteRoot) != "" {
		return t.WriteRoot
	}
	return t.Root
}

func validatePathInRoot(rootPath, abs string) error {
	if rootPath == "" {
		return nil
	}
	root, err := filepath.Abs(rootPath)
	if err != nil {
		return err
	}
	root, err = CanonicalizeRoot(root)
	if err != nil {
		return err
	}
	resolved, err := CanonicalizePath(abs)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(root, resolved)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("path outside allowed root")
	}
	return nil
}

func (t *FileTool) openSafeRead(path string) (*os.File, os.FileInfo, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, nil, err
	}
	if err := validateOpenedPathUnchanged(path, info); err != nil {
		f.Close()
		return nil, nil, err
	}
	if err := t.validatePathInRoot(path); err != nil {
		f.Close()
		return nil, nil, err
	}
	return f, info, nil
}

func (t *FileTool) openSafeWrite(path string, perm os.FileMode) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE, perm)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	if err := validateOpenedPathUnchanged(path, info); err != nil {
		f.Close()
		return nil, err
	}
	if err := t.validatePathInWriteRoot(path); err != nil {
		f.Close()
		return nil, err
	}
	if err := f.Truncate(0); err != nil {
		f.Close()
		return nil, err
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}

func (t *FileTool) validateOpenedPath(path string, openedInfo os.FileInfo) error {
	if err := t.validatePathInRoot(path); err != nil {
		return err
	}
	return validateOpenedPathUnchanged(path, openedInfo)
}

func (t *FileTool) validateOpenedWritePath(path string, openedInfo os.FileInfo) error {
	if err := t.validatePathInWriteRoot(path); err != nil {
		return err
	}
	return validateOpenedPathUnchanged(path, openedInfo)
}

func validateOpenedPathUnchanged(path string, openedInfo os.FileInfo) error {
	resolved, err := CanonicalizePath(path)
	if err != nil {
		return err
	}
	currentInfo, err := os.Stat(resolved)
	if err != nil {
		return err
	}
	if !os.SameFile(openedInfo, currentInfo) {
		return fmt.Errorf("path changed during file operation")
	}
	return nil
}

func CanonicalizeRoot(root string) (string, error) {
	if _, err := os.Stat(root); err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(root)
}

func CanonicalizePath(abs string) (string, error) {
	if _, err := os.Lstat(abs); err == nil {
		return filepath.EvalSymlinks(abs)
	} else if !os.IsNotExist(err) {
		return "", err
	}
	existing := abs
	missingParts := make([]string, 0, 4)
	for {
		if _, err := os.Lstat(existing); err == nil {
			break
		} else if !os.IsNotExist(err) {
			return "", err
		}
		parent := filepath.Dir(existing)
		if parent == existing {
			return "", os.ErrNotExist
		}
		missingParts = append(missingParts, filepath.Base(existing))
		existing = parent
	}
	realExisting, err := filepath.EvalSymlinks(existing)
	if err != nil {
		return "", err
	}
	for i := len(missingParts) - 1; i >= 0; i-- {
		realExisting = filepath.Join(realExisting, missingParts[i])
	}
	return realExisting, nil
}

type ReadFile struct{ FileTool }

func (t *ReadFile) Name() string { return "read_file" }
func (t *ReadFile) Description() string {
	return "Read a UTF-8 text file from the allowed workspace. Default mode=preview is the safest general choice. Use mode=outline to understand a file cheaply, mode=grep when looking for a symbol/string, and mode=range after you know line numbers. Use mode=full when the whole bounded file is needed."
}
func (t *ReadFile) CapabilityForParams(params map[string]any) CapabilityLevel {
	return CapabilitySafe
}
func (t *ReadFile) Parameters() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{
		"path":      map[string]any{"type": "string", "description": "File path to read. Use an absolute path or a path relative to the current workspace; the path must stay inside the allowed read root."},
		"mode":      map[string]any{"type": "string", "enum": []string{"preview", "full", "range", "grep", "outline"}, "description": "Read mode. Omit for preview. All modes are safe read-only operations. Use full when the whole bounded file is needed."},
		"startLine": map[string]any{"type": "integer", "description": "For mode=range only: 1-based first line to return. Use with endLine after an outline/grep/preview identifies the area you need."},
		"endLine":   map[string]any{"type": "integer", "description": "For mode=range only: 1-based last line to return, inclusive. Keep ranges focused to avoid unnecessary output."},
		"pattern":   map[string]any{"type": "string", "description": "For mode=grep only: substring or regex pattern to search for, such as a function name, type name, config key, or exact error text."},
		"maxBytes":  map[string]any{"type": "integer", "description": "Maximum bytes returned directly for preview/grep/range/outline/full. Omit for default 65536."},
	}, "required": []string{"path"}}
}
func (t *ReadFile) Schema() map[string]any {
	return t.SchemaFor(t.Name(), t.Description(), t.Parameters())
}
func (t *ReadFile) Execute(ctx context.Context, params map[string]any) (string, error) {
	p, err := t.safePath(fmt.Sprint(params["path"]))
	if err != nil {
		return "", err
	}
	max := intParam(params, "maxBytes", defaultReadFileMaxBytes)
	mode := strings.ToLower(strings.TrimSpace(fmt.Sprint(params["mode"])))
	if mode == "" || mode == "<nil>" {
		mode = "preview"
	}
	f, info, err := t.openSafeRead(p)
	if err != nil {
		return "", err
	}
	defer f.Close()
	switch mode {
	case "preview", "full":
		return t.readPreview(f, p, info.Size(), max, mode)
	case "range":
		start := intParam(params, "startLine", 1)
		end := intParam(params, "endLine", start)
		return readLineRange(f, p, info.Size(), start, end, max)
	case "grep":
		pattern := strings.TrimSpace(fmt.Sprint(params["pattern"]))
		if pattern == "" || pattern == "<nil>" {
			return "", fmt.Errorf("missing pattern")
		}
		return grepFile(f, p, info.Size(), pattern, max)
	case "outline":
		return outlineFile(f, p, info.Size(), max)
	default:
		return "", fmt.Errorf("unsupported read_file mode: %s", mode)
	}
}

func (t *ReadFile) readPreview(f *os.File, path string, size int64, max int, mode string) (string, error) {
	b, err := io.ReadAll(io.LimitReader(f, int64(max)+1))
	if err != nil {
		return "", err
	}
	truncated := len(b) > max
	if truncated {
		b = b[:max]
	}
	preview := string(b)
	summary := fmt.Sprintf("Read %s from %s", mode, path)
	advice := []string(nil)
	if truncated {
		summary = fmt.Sprintf("Read bounded %s from %s; output truncated", mode, path)
		if mode == "full" {
			advice = TruncationAdvice("read_file_full", path)
		}
	}
	return EncodeToolResult(ToolResult{
		Kind:    "file_read",
		OK:      true,
		Summary: summary,
		Preview: preview,
		Advice:  advice,
		Stats: map[string]any{
			"path":      path,
			"mode":      mode,
			"bytes":     size,
			"max_bytes": max,
			"truncated": truncated,
			"artifact":  "full content is available through runtime artifact spillover when output exceeds the tool byte budget",
		},
	}), nil
}

type SearchFile struct{ FileTool }

func (t *SearchFile) Name() string { return "search_file" }
func (t *SearchFile) Description() string {
	return "Search one UTF-8 text file and return bounded matching lines. Use this instead of read_file mode=full when you know a symbol, string, error message, or config key and only need matching context."
}
func (t *SearchFile) Parameters() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{
		"path":     map[string]any{"type": "string", "description": "File path to search. Use an absolute path or workspace-relative path inside the allowed read root."},
		"pattern":  map[string]any{"type": "string", "description": "Pattern to find. Valid regular expressions use regex matching; invalid regex falls back to literal substring matching."},
		"maxBytes": map[string]any{"type": "integer", "description": "Maximum bytes of matching-line output returned directly. Omit for default 65536."},
	}, "required": []string{"path", "pattern"}}
}
func (t *SearchFile) Schema() map[string]any {
	return t.SchemaFor(t.Name(), t.Description(), t.Parameters())
}
func (t *SearchFile) Execute(ctx context.Context, params map[string]any) (string, error) {
	_ = ctx
	p, err := t.safePath(fmt.Sprint(params["path"]))
	if err != nil {
		return "", err
	}
	pattern := strings.TrimSpace(fmt.Sprint(params["pattern"]))
	if pattern == "" || pattern == "<nil>" {
		return "", fmt.Errorf("missing pattern")
	}
	f, info, err := t.openSafeRead(p)
	if err != nil {
		return "", err
	}
	defer f.Close()
	return grepFile(f, p, info.Size(), pattern, intParam(params, "maxBytes", defaultReadFileMaxBytes))
}

type WriteFile struct{ FileTool }

func (t *WriteFile) Capability() CapabilityLevel { return CapabilityGuarded }
func (t *WriteFile) Name() string                { return "write_file" }
func (t *WriteFile) Description() string {
	return "Create or completely replace a text file in the allowed write root. This is guarded because it overwrites the target file. Prefer edit_file for small changes to an existing file; use write_file when creating a new file or intentionally replacing all content."
}
func (t *WriteFile) Parameters() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{
		"path":    map[string]any{"type": "string", "description": "Destination file path inside the allowed write root. Existing content at this path will be replaced."},
		"content": map[string]any{"type": "string", "description": "Complete new file contents, not a patch or partial snippet."},
		"mkdirs":  map[string]any{"type": "boolean", "description": "Set true only when parent directories should be created for a new path."},
	}, "required": []string{"path", "content"}}
}
func (t *WriteFile) Schema() map[string]any {
	return t.SchemaFor(t.Name(), t.Description(), t.Parameters())
}
func (t *WriteFile) Execute(ctx context.Context, params map[string]any) (string, error) {
	p, err := t.safeWritePath(fmt.Sprint(params["path"]))
	if err != nil {
		return "", err
	}
	content := fmt.Sprint(params["content"])
	mkdirs, _ := params["mkdirs"].(bool)
	if mkdirs {
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			return "", err
		}
	}
	out, err := t.openSafeWrite(p, 0o600)
	if err != nil {
		return "", err
	}
	defer out.Close()
	if _, err := out.Write([]byte(content)); err != nil {
		return "", err
	}
	return "ok", nil
}

type EditFile struct{ FileTool }

func (t *EditFile) Capability() CapabilityLevel { return CapabilityGuarded }
func (t *EditFile) Name() string                { return "edit_file" }
func (t *EditFile) Description() string {
	return "Edit an existing text file by applying exact find/replace operations inside the allowed write root. This is guarded. Prefer it over write_file for localized changes, and make each find string specific enough to avoid accidental replacements."
}
func (t *EditFile) Parameters() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{
		"path": map[string]any{"type": "string", "description": "Existing file path to edit inside the allowed write root."},
		"edits": map[string]any{"type": "array", "items": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"find":    map[string]any{"type": "string", "description": "Exact text to replace. Include enough surrounding context when needed so the match is unique."},
				"replace": map[string]any{"type": "string", "description": "Replacement text for the matched find string."},
				"count":   map[string]any{"type": "integer", "description": "Maximum replacements for this edit. Omit or use 0 for all matches; use 1 for a unique targeted edit."},
			},
			"required": []string{"find", "replace"},
		}},
	}, "required": []string{"path", "edits"}}
}
func (t *EditFile) Schema() map[string]any {
	return t.SchemaFor(t.Name(), t.Description(), t.Parameters())
}
func (t *EditFile) Execute(ctx context.Context, params map[string]any) (string, error) {
	p, err := t.safeWritePath(fmt.Sprint(params["path"]))
	if err != nil {
		return "", err
	}
	in, _, err := t.openSafeRead(p)
	if err != nil {
		return "", err
	}
	const maxEditSize = 10 * 1024 * 1024
	b, err := io.ReadAll(io.LimitReader(in, maxEditSize+1))
	closeErr := in.Close()
	if err == nil && len(b) > maxEditSize {
		return "", fmt.Errorf("file too large to edit (max %d bytes)", maxEditSize)
	}
	if err != nil {
		return "", err
	}
	if closeErr != nil {
		return "", closeErr
	}
	s := string(b)
	rawEdits, _ := params["edits"].([]any)
	for _, e := range rawEdits {
		m, _ := e.(map[string]any)
		find := fmt.Sprint(m["find"])
		replace := fmt.Sprint(m["replace"])
		count := 0
		if v, ok := m["count"].(float64); ok {
			count = int(v)
		}
		if count <= 0 {
			s = strings.ReplaceAll(s, find, replace)
		} else {
			s = strings.Replace(s, find, replace, count)
		}
	}
	out, err := t.openSafeWrite(p, 0)
	if err != nil {
		return "", err
	}
	defer out.Close()
	if _, err := out.Write([]byte(s)); err != nil {
		return "", err
	}
	return "ok", nil
}

type DeleteFile struct{ FileTool }

func (t *DeleteFile) Capability() CapabilityLevel { return CapabilityGuarded }
func (t *DeleteFile) Name() string                { return "delete_file" }
func (t *DeleteFile) Metadata() ToolMetadata      { return metadataForTool(t, ToolGroupWrite) }
func (t *DeleteFile) Description() string {
	return "Move a file inside the allowed write root into the workspace trash folder. This is guarded. Use it when the user asks to delete a file; it does not permanently remove directories or files outside the write root."
}
func (t *DeleteFile) Parameters() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{
		"path": map[string]any{"type": "string", "description": "Existing file path inside the allowed write root to move into .or3-trash."},
	}, "required": []string{"path"}}
}
func (t *DeleteFile) Schema() map[string]any {
	return t.SchemaFor(t.Name(), t.Description(), t.Parameters())
}
func (t *DeleteFile) Execute(ctx context.Context, params map[string]any) (string, error) {
	_ = ctx
	p, err := t.safeWritePath(fmt.Sprint(params["path"]))
	if err != nil {
		return "", err
	}
	info, err := os.Stat(p)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		return "", fmt.Errorf("delete_file only deletes files, not directories: %s", p)
	}
	if err := t.validatePathInWriteRoot(p); err != nil {
		return "", err
	}
	writeRoot := strings.TrimSpace(t.effectiveWriteRoot())
	if writeRoot == "" {
		writeRoot = filepath.Dir(p)
	}
	root, err := filepath.Abs(writeRoot)
	if err != nil {
		return "", err
	}
	root, err = CanonicalizeRoot(root)
	if err != nil {
		return "", err
	}
	trashRoot := filepath.Join(root, ".or3-trash")
	rel, err := filepath.Rel(root, p)
	if err != nil {
		return "", err
	}
	if rel == ".or3-trash" || strings.HasPrefix(rel, ".or3-trash"+string(filepath.Separator)) {
		return "", fmt.Errorf("refusing to delete files already inside workspace trash")
	}
	dest := filepath.Join(trashRoot, rel+"."+fmt.Sprint(time.Now().UnixNano()))
	if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
		return "", err
	}
	if err := os.Rename(p, dest); err != nil {
		return "", err
	}
	return EncodeToolResult(ToolResult{
		Kind:    "delete_file",
		OK:      true,
		Summary: fmt.Sprintf("Moved %s to workspace trash", p),
		Stats: map[string]any{
			"path":       p,
			"trash_path": dest,
			"bytes":      info.Size(),
		},
	}), nil
}

type ListDir struct{ FileTool }

func (t *ListDir) Name() string { return "list_dir" }
func (t *ListDir) Description() string {
	return "List files and folders in one directory without recursion. Use this before read_file when navigating an unfamiliar tree or choosing which file to inspect."
}
func (t *ListDir) Parameters() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{
		"path": map[string]any{"type": "string", "description": "Directory path to list. Use an absolute path or workspace-relative path inside the allowed read root."},
		"max":  map[string]any{"type": "integer", "description": "Maximum entries to return. Omit for default 80; increase only when a directory listing is truncated."},
	}, "required": []string{"path"}}
}
func (t *ListDir) Schema() map[string]any {
	return t.SchemaFor(t.Name(), t.Description(), t.Parameters())
}
func (t *ListDir) Execute(ctx context.Context, params map[string]any) (string, error) {
	p, err := t.safePath(fmt.Sprint(params["path"]))
	if err != nil {
		return "", err
	}
	f, info, err := t.openSafeRead(p)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if !info.IsDir() {
		return "", fmt.Errorf("path is not a directory: %s", p)
	}
	max := intParam(params, "max", defaultListDirMaxEntries)
	ents, err := f.ReadDir(max + 1)
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	truncated := len(ents) > max
	if truncated {
		ents = ents[:max]
	}
	sort.Slice(ents, func(i, j int) bool {
		if ents[i].IsDir() != ents[j].IsDir() {
			return ents[i].IsDir()
		}
		return ents[i].Name() < ents[j].Name()
	})
	type entry struct {
		Name  string `json:"name"`
		IsDir bool   `json:"isDir"`
		Size  int64  `json:"size"`
	}
	out := []entry{}
	for _, e := range ents {
		if len(out) >= max {
			break
		}
		info, _ := e.Info()
		sz := int64(0)
		if info != nil {
			sz = info.Size()
		}
		out = append(out, entry{Name: e.Name(), IsDir: e.IsDir(), Size: sz})
	}
	b, _ := json.MarshalIndent(out, "", "  ")
	return EncodeToolResult(ToolResult{
		Kind:    "list_dir",
		OK:      true,
		Summary: fmt.Sprintf("Listed %d entries in %s", len(out), p),
		Preview: string(b),
		Advice: func() []string {
			if truncated {
				return TruncationAdvice("list_dir", p)
			}
			return nil
		}(),
		Stats: map[string]any{
			"path":      p,
			"returned":  len(out),
			"max":       max,
			"truncated": truncated,
		},
	}), nil
}

func readLineRange(f *os.File, path string, size int64, start, end, max int) (string, error) {
	if start <= 0 {
		start = 1
	}
	if end < start {
		end = start
	}
	var b strings.Builder
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	line := 0
	truncated := false
	for sc.Scan() {
		line++
		if line < start {
			continue
		}
		if line > end {
			break
		}
		next := fmt.Sprintf("%d: %s\n", line, sc.Text())
		if max > 0 && b.Len()+len(next) > max {
			truncated = true
			break
		}
		b.WriteString(next)
	}
	if err := sc.Err(); err != nil {
		return "", err
	}
	return EncodeToolResult(ToolResult{
		Kind:    "file_read",
		OK:      true,
		Summary: fmt.Sprintf("Read lines %d-%d from %s", start, end, path),
		Preview: strings.TrimRight(b.String(), "\n"),
		Advice: func() []string {
			if truncated {
				return TruncationAdvice("read_file_range", path)
			}
			return nil
		}(),
		Stats: map[string]any{
			"path":       path,
			"mode":       "range",
			"bytes":      size,
			"start_line": start,
			"end_line":   end,
			"max_bytes":  max,
			"truncated":  truncated,
		},
	}), nil
}

func grepFile(f *os.File, path string, size int64, pattern string, max int) (string, error) {
	var match func(string) bool
	if re, err := regexp.Compile(pattern); err == nil {
		match = re.MatchString
	} else {
		match = func(s string) bool { return strings.Contains(s, pattern) }
	}
	var b strings.Builder
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	line, matches := 0, 0
	truncated := false
	for sc.Scan() {
		line++
		text := sc.Text()
		if !match(text) {
			continue
		}
		matches++
		next := fmt.Sprintf("%d: %s\n", line, text)
		if max > 0 && b.Len()+len(next) > max {
			truncated = true
			break
		}
		b.WriteString(next)
	}
	if err := sc.Err(); err != nil {
		return "", err
	}
	return EncodeToolResult(ToolResult{
		Kind:    "file_search",
		OK:      true,
		Summary: fmt.Sprintf("Found %d matching lines in %s", matches, path),
		Preview: strings.TrimRight(b.String(), "\n"),
		Advice: func() []string {
			if truncated {
				return TruncationAdvice("search_file", path)
			}
			return nil
		}(),
		Stats: map[string]any{
			"path":      path,
			"pattern":   pattern,
			"bytes":     size,
			"matches":   matches,
			"max_bytes": max,
			"truncated": truncated,
		},
	}), nil
}

func outlineFile(f *os.File, path string, size int64, max int) (string, error) {
	var b strings.Builder
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	line, entries := 0, 0
	truncated := false
	for sc.Scan() {
		line++
		text := strings.TrimSpace(sc.Text())
		if !isOutlineLine(text) {
			continue
		}
		entries++
		next := fmt.Sprintf("%d: %s\n", line, text)
		if max > 0 && b.Len()+len(next) > max {
			truncated = true
			break
		}
		b.WriteString(next)
	}
	if err := sc.Err(); err != nil {
		return "", err
	}
	return EncodeToolResult(ToolResult{
		Kind:    "file_outline",
		OK:      true,
		Summary: fmt.Sprintf("Outlined %d structural lines in %s", entries, path),
		Preview: strings.TrimRight(b.String(), "\n"),
		Advice: func() []string {
			if truncated {
				return TruncationAdvice("read_file_outline", path)
			}
			return nil
		}(),
		Stats: map[string]any{
			"path":      path,
			"bytes":     size,
			"entries":   entries,
			"max_bytes": max,
			"truncated": truncated,
		},
	}), nil
}

func isOutlineLine(s string) bool {
	return strings.HasPrefix(s, "#") ||
		strings.HasPrefix(s, "func ") ||
		strings.HasPrefix(s, "type ") ||
		strings.HasPrefix(s, "class ") ||
		strings.HasPrefix(s, "def ")
}

func intParam(params map[string]any, key string, fallback int) int {
	switch v := params[key].(type) {
	case float64:
		if int(v) > 0 {
			return int(v)
		}
	case int:
		if v > 0 {
			return v
		}
	}
	return fallback
}

func existingFileMode(path string, defaultMode os.FileMode) os.FileMode {
	if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() {
		return info.Mode().Perm()
	}
	if defaultMode == 0 {
		return 0o600
	}
	return defaultMode
}
