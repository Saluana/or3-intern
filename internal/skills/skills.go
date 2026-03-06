package skills

import (
	"crypto/sha1"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type SkillMeta struct {
	Name string
	Path string
	ModTime time.Time
	Size int64
	ID string
}

type Inventory struct {
	Skills []SkillMeta
	byName map[string]SkillMeta
}

func Scan(dirs []string) Inventory {
	var skills []SkillMeta
	for _, dir := range dirs {
		if strings.TrimSpace(dir) == "" { continue }
		root, err := filepath.Abs(dir)
		if err != nil { continue }
		root, err = filepath.EvalSymlinks(root)
		if err != nil { continue }
		_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil { return nil }
			if d.Type()&os.ModeSymlink != 0 { return nil }
			if d.IsDir() { return nil }
			ext := strings.ToLower(filepath.Ext(path))
			if ext != ".md" && ext != ".txt" { return nil }
			realPath, err := filepath.EvalSymlinks(path)
			if err != nil { return nil }
			rel, err := filepath.Rel(root, realPath)
			if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) { return nil }
			info, _ := d.Info()
			mt := time.Time{}
			sz := int64(0)
			if info != nil { mt = info.ModTime(); sz = info.Size() }
			name := strings.TrimSuffix(filepath.Base(realPath), ext)
			skills = append(skills, SkillMeta{Name: name, Path: realPath, ModTime: mt, Size: sz, ID: hash(realPath)})
			return nil
		})
	}
	sort.Slice(skills, func(i, j int) bool { return skills[i].Name < skills[j].Name })
	by := map[string]SkillMeta{}
	for _, s := range skills { by[s.Name] = s }
	return Inventory{Skills: skills, byName: by}
}

func (inv Inventory) Get(name string) (SkillMeta, bool) {
	s, ok := inv.byName[name]
	return s, ok
}

func (inv Inventory) Summary(max int) string {
	if max <= 0 { max = 50 }
	lines := []string{}
	for i, s := range inv.Skills {
		if i >= max { lines = append(lines, "…"); break }
		lines = append(lines, "- "+s.Name)
	}
	if len(lines) == 0 { return "(no skills found)" }
	return strings.Join(lines, "\n")
}

func LoadBody(path string, maxBytes int) (string, error) {
	if maxBytes <= 0 { maxBytes = 200000 }
	info, err := os.Lstat(path)
	if err != nil { return "", err }
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() { return "", fs.ErrPermission }
	b, err := os.ReadFile(path)
	if err != nil { return "", err }
	if len(b) > maxBytes { b = b[:maxBytes] }
	return string(b), nil
}

func hash(s string) string {
	h := sha1.Sum([]byte(s))
	return hex.EncodeToString(h[:8])
}
