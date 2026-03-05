package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type FileTool struct {
	Base
	Root string // allowed root (optional)
}

func (t *FileTool) safePath(p string) (string, error) {
	if strings.TrimSpace(p) == "" { return "", errors.New("missing path") }
	abs, err := filepath.Abs(p)
	if err != nil { return "", err }
	if t.Root != "" {
		root, _ := filepath.Abs(t.Root)
		rel, err := filepath.Rel(root, abs)
		if err != nil || strings.HasPrefix(rel, "..") {
			return "", fmt.Errorf("path outside allowed root")
		}
	}
	return abs, nil
}

type ReadFile struct{ FileTool }
func (t *ReadFile) Name() string { return "read_file" }
func (t *ReadFile) Description() string { return "Read a UTF-8 text file." }
func (t *ReadFile) Parameters() map[string]any {
	return map[string]any{"type":"object","properties":map[string]any{
		"path": map[string]any{"type":"string"},
		"maxBytes": map[string]any{"type":"integer","description":"Max bytes to read (default 200000)"},
	},"required":[]string{"path"}}
}
func (t *ReadFile) Schema() map[string]any { return t.SchemaFor(t.Name(), t.Description(), t.Parameters()) }
func (t *ReadFile) Execute(ctx context.Context, params map[string]any) (string, error) {
	p, err := t.safePath(fmt.Sprint(params["path"]))
	if err != nil { return "", err }
	max := 200000
	if v, ok := params["maxBytes"].(float64); ok && int(v) > 0 { max = int(v) }
	b, err := os.ReadFile(p)
	if err != nil { return "", err }
	if len(b) > max { b = b[:max] }
	return string(b), nil
}

type WriteFile struct{ FileTool }
func (t *WriteFile) Name() string { return "write_file" }
func (t *WriteFile) Description() string { return "Write text to a file (overwrites)." }
func (t *WriteFile) Parameters() map[string]any {
	return map[string]any{"type":"object","properties":map[string]any{
		"path": map[string]any{"type":"string"},
		"content": map[string]any{"type":"string"},
		"mkdirs": map[string]any{"type":"boolean"},
	},"required":[]string{"path","content"}}
}
func (t *WriteFile) Schema() map[string]any { return t.SchemaFor(t.Name(), t.Description(), t.Parameters()) }
func (t *WriteFile) Execute(ctx context.Context, params map[string]any) (string, error) {
	p, err := t.safePath(fmt.Sprint(params["path"]))
	if err != nil { return "", err }
	content := fmt.Sprint(params["content"])
	mkdirs, _ := params["mkdirs"].(bool)
	if mkdirs { _ = os.MkdirAll(filepath.Dir(p), 0o755) }
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil { return "", err }
	return "ok", nil
}

type EditFile struct{ FileTool }
func (t *EditFile) Name() string { return "edit_file" }
func (t *EditFile) Description() string {
	return "Edit a text file by applying a list of find/replace operations."
}
func (t *EditFile) Parameters() map[string]any {
	return map[string]any{"type":"object","properties":map[string]any{
		"path": map[string]any{"type":"string"},
		"edits": map[string]any{"type":"array","items":map[string]any{
			"type":"object",
			"properties":map[string]any{
				"find": map[string]any{"type":"string"},
				"replace": map[string]any{"type":"string"},
				"count": map[string]any{"type":"integer","description":"max replacements (0=all)"},
			},
			"required":[]string{"find","replace"},
		}},
	},"required":[]string{"path","edits"}}
}
func (t *EditFile) Schema() map[string]any { return t.SchemaFor(t.Name(), t.Description(), t.Parameters()) }
func (t *EditFile) Execute(ctx context.Context, params map[string]any) (string, error) {
	p, err := t.safePath(fmt.Sprint(params["path"]))
	if err != nil { return "", err }
	b, err := os.ReadFile(p)
	if err != nil { return "", err }
	s := string(b)
	rawEdits, _ := params["edits"].([]any)
	for _, e := range rawEdits {
		m, _ := e.(map[string]any)
		find := fmt.Sprint(m["find"])
		replace := fmt.Sprint(m["replace"])
		count := 0
		if v, ok := m["count"].(float64); ok { count = int(v) }
		if count <= 0 {
			s = strings.ReplaceAll(s, find, replace)
		} else {
			s = strings.Replace(s, find, replace, count)
		}
	}
	if err := os.WriteFile(p, []byte(s), 0o644); err != nil { return "", err }
	return "ok", nil
}

type ListDir struct{ FileTool }
func (t *ListDir) Name() string { return "list_dir" }
func (t *ListDir) Description() string { return "List directory entries." }
func (t *ListDir) Parameters() map[string]any {
	return map[string]any{"type":"object","properties":map[string]any{
		"path": map[string]any{"type":"string"},
		"max": map[string]any{"type":"integer"},
	},"required":[]string{"path"}}
}
func (t *ListDir) Schema() map[string]any { return t.SchemaFor(t.Name(), t.Description(), t.Parameters()) }
func (t *ListDir) Execute(ctx context.Context, params map[string]any) (string, error) {
	p, err := t.safePath(fmt.Sprint(params["path"]))
	if err != nil { return "", err }
	ents, err := os.ReadDir(p)
	if err != nil { return "", err }
	max := 200
	if v, ok := params["max"].(float64); ok && int(v) > 0 { max = int(v) }
	type entry struct{ Name string `json:"name"`; IsDir bool `json:"isDir"`; Size int64 `json:"size"` }
	out := []entry{}
	for _, e := range ents {
		if len(out) >= max { break }
		info, _ := e.Info()
		sz := int64(0)
		if info != nil { sz = info.Size() }
		out = append(out, entry{Name: e.Name(), IsDir: e.IsDir(), Size: sz})
	}
	b, _ := json.MarshalIndent(out, "", "  ")
	return string(b), nil
}
