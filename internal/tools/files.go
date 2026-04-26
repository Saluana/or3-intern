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
	"strings"
)

type FileTool struct {
	Base
	Root string // allowed root (optional)
}

const (
	defaultReadFileMaxBytes  = 12000
	defaultListDirMaxEntries = 80
)

func (t *FileTool) safePath(p string) (string, error) {
	if strings.TrimSpace(p) == "" {
		return "", errors.New("missing path")
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	abs, err = canonicalizePath(abs)
	if err != nil {
		return "", err
	}
	if t.Root != "" {
		root, err := filepath.Abs(t.Root)
		if err != nil {
			return "", err
		}
		root, err = canonicalizeRoot(root)
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

func canonicalizeRoot(root string) (string, error) {
	if _, err := os.Stat(root); err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(root)
}

func canonicalizePath(abs string) (string, error) {
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

func (t *ReadFile) Name() string        { return "read_file" }
func (t *ReadFile) Description() string { return "Read a UTF-8 text file." }
func (t *ReadFile) CapabilityForParams(params map[string]any) CapabilityLevel {
	if strings.EqualFold(strings.TrimSpace(fmt.Sprint(params["mode"])), "full") {
		return CapabilityGuarded
	}
	return CapabilitySafe
}
func (t *ReadFile) Parameters() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{
		"path":      map[string]any{"type": "string"},
		"mode":      map[string]any{"type": "string", "enum": []string{"preview", "full", "range", "grep", "outline"}, "description": "Read mode (default preview). full is still bounded and may spill to an artifact at runtime."},
		"startLine": map[string]any{"type": "integer", "description": "1-based start line for range mode"},
		"endLine":   map[string]any{"type": "integer", "description": "1-based end line for range mode"},
		"pattern":   map[string]any{"type": "string", "description": "Substring or regex pattern for grep mode"},
		"maxBytes":  map[string]any{"type": "integer", "description": "Max preview bytes (default 12000)"},
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
	info, err := os.Stat(p)
	if err != nil {
		return "", err
	}
	switch mode {
	case "preview", "full":
		return t.readPreview(p, info.Size(), max, mode)
	case "range":
		start := intParam(params, "startLine", 1)
		end := intParam(params, "endLine", start)
		return readLineRange(p, info.Size(), start, end, max)
	case "grep":
		pattern := strings.TrimSpace(fmt.Sprint(params["pattern"]))
		if pattern == "" || pattern == "<nil>" {
			return "", fmt.Errorf("missing pattern")
		}
		return grepFile(p, info.Size(), pattern, max)
	case "outline":
		return outlineFile(p, info.Size(), max)
	default:
		return "", fmt.Errorf("unsupported read_file mode: %s", mode)
	}
}

func (t *ReadFile) readPreview(path string, size int64, max int, mode string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
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
	if truncated {
		summary = fmt.Sprintf("Read bounded %s from %s; output truncated", mode, path)
	}
	return EncodeToolResult(ToolResult{
		Kind:    "file_read",
		OK:      true,
		Summary: summary,
		Preview: preview,
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
	return "Search a UTF-8 text file and return bounded matching lines."
}
func (t *SearchFile) Parameters() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{
		"path":     map[string]any{"type": "string"},
		"pattern":  map[string]any{"type": "string"},
		"maxBytes": map[string]any{"type": "integer", "description": "Max preview bytes (default 12000)"},
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
	info, err := os.Stat(p)
	if err != nil {
		return "", err
	}
	pattern := strings.TrimSpace(fmt.Sprint(params["pattern"]))
	if pattern == "" || pattern == "<nil>" {
		return "", fmt.Errorf("missing pattern")
	}
	return grepFile(p, info.Size(), pattern, intParam(params, "maxBytes", defaultReadFileMaxBytes))
}

type WriteFile struct{ FileTool }

func (t *WriteFile) Capability() CapabilityLevel { return CapabilityGuarded }
func (t *WriteFile) Name() string                { return "write_file" }
func (t *WriteFile) Description() string         { return "Write text to a file (overwrites)." }
func (t *WriteFile) Parameters() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{
		"path":    map[string]any{"type": "string"},
		"content": map[string]any{"type": "string"},
		"mkdirs":  map[string]any{"type": "boolean"},
	}, "required": []string{"path", "content"}}
}
func (t *WriteFile) Schema() map[string]any {
	return t.SchemaFor(t.Name(), t.Description(), t.Parameters())
}
func (t *WriteFile) Execute(ctx context.Context, params map[string]any) (string, error) {
	p, err := t.safePath(fmt.Sprint(params["path"]))
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
	if err := os.WriteFile(p, []byte(content), existingFileMode(p, 0o600)); err != nil {
		return "", err
	}
	return "ok", nil
}

type EditFile struct{ FileTool }

func (t *EditFile) Capability() CapabilityLevel { return CapabilityGuarded }
func (t *EditFile) Name() string                { return "edit_file" }
func (t *EditFile) Description() string {
	return "Edit a text file by applying a list of find/replace operations."
}
func (t *EditFile) Parameters() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{
		"path": map[string]any{"type": "string"},
		"edits": map[string]any{"type": "array", "items": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"find":    map[string]any{"type": "string"},
				"replace": map[string]any{"type": "string"},
				"count":   map[string]any{"type": "integer", "description": "max replacements (0=all)"},
			},
			"required": []string{"find", "replace"},
		}},
	}, "required": []string{"path", "edits"}}
}
func (t *EditFile) Schema() map[string]any {
	return t.SchemaFor(t.Name(), t.Description(), t.Parameters())
}
func (t *EditFile) Execute(ctx context.Context, params map[string]any) (string, error) {
	p, err := t.safePath(fmt.Sprint(params["path"]))
	if err != nil {
		return "", err
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return "", err
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
	if err := os.WriteFile(p, []byte(s), existingFileMode(p, 0)); err != nil {
		return "", err
	}
	return "ok", nil
}

type ListDir struct{ FileTool }

func (t *ListDir) Name() string        { return "list_dir" }
func (t *ListDir) Description() string { return "List directory entries." }
func (t *ListDir) Parameters() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{
		"path": map[string]any{"type": "string"},
		"max":  map[string]any{"type": "integer"},
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
	ents, err := os.ReadDir(p)
	if err != nil {
		return "", err
	}
	max := defaultListDirMaxEntries
	if v, ok := params["max"].(float64); ok && int(v) > 0 {
		max = int(v)
	}
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
		Summary: fmt.Sprintf("Listed %d of %d entries in %s", len(out), len(ents), p),
		Preview: string(b),
		Stats: map[string]any{
			"path":      p,
			"returned":  len(out),
			"total":     len(ents),
			"max":       max,
			"truncated": len(ents) > len(out),
		},
	}), nil
}

func readLineRange(path string, size int64, start, end, max int) (string, error) {
	if start <= 0 {
		start = 1
	}
	if end < start {
		end = start
	}
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
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

func grepFile(path string, size int64, pattern string, max int) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	var b strings.Builder
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	line, matches := 0, 0
	truncated := false
	for sc.Scan() {
		line++
		text := sc.Text()
		if !strings.Contains(text, pattern) {
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

func outlineFile(path string, size int64, max int) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
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
