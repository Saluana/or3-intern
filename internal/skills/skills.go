package skills

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// SkillEntry describes a declared executable entrypoint from a skill manifest.
type SkillEntry struct {
	Name           string   `json:"name"`
	Command        []string `json:"command"`
	TimeoutSeconds int      `json:"timeoutSeconds"`
	AcceptsStdin   bool     `json:"acceptsStdin"`
}

type SkillMeta struct {
	Name        string
	Path        string
	ModTime     time.Time
	Size        int64
	ID          string
	Summary     string       // short capability description
	Entrypoints []SkillEntry // declared executable entrypoints from manifest
}

type Inventory struct {
	Skills []SkillMeta
	byName map[string]SkillMeta
}

// skillManifest is the JSON structure of skill.json.
type skillManifest struct {
	Summary     string       `json:"summary"`
	Entrypoints []SkillEntry `json:"entrypoints"`
}

// loadManifest tries to load a skill.json from the same directory as path.
func loadManifest(dir string) (skillManifest, bool) {
	data, err := os.ReadFile(filepath.Join(dir, "skill.json"))
	if err != nil {
		return skillManifest{}, false
	}
	var m skillManifest
	if err := json.Unmarshal(data, &m); err != nil {
		return skillManifest{}, false
	}
	return m, true
}

// maxFrontMatterLines is the maximum number of lines scanned for YAML front matter.
const maxFrontMatterLines = 20

// extractFrontMatterSummary parses the first YAML front matter block (--- ... ---)
// and returns the value of the "summary:" field if present.
func extractFrontMatterSummary(content string) string {
	lines := strings.SplitN(content, "\n", maxFrontMatterLines)
	if len(lines) == 0 {
		return ""
	}
	if strings.TrimSpace(lines[0]) != "---" {
		return ""
	}
	for _, line := range lines[1:] {
		trimmed := strings.TrimSpace(line)
		if trimmed == "---" {
			break
		}
		if strings.HasPrefix(trimmed, "summary:") {
			val := strings.TrimSpace(strings.TrimPrefix(trimmed, "summary:"))
			// Strip optional quotes
			val = strings.Trim(val, `"'`)
			return val
		}
	}
	return ""
}

func skillFileInDir(dir string) (string, bool) {
	for _, name := range []string{"SKILL.md", "skill.md"} {
		path := filepath.Join(dir, name)
		info, err := os.Lstat(path)
		if err != nil {
			continue
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			continue
		}
		return path, true
	}
	return "", false
}

func appendSkill(metaByName map[string]SkillMeta, path, name string, info fs.FileInfo) {
	if strings.TrimSpace(name) == "" {
		return
	}
	meta := SkillMeta{
		Name: name,
		Path: path,
		ID:   hash(path),
	}
	if info != nil {
		meta.ModTime = info.ModTime()
		meta.Size = info.Size()
	}
	if man, ok := loadManifest(filepath.Dir(path)); ok {
		meta.Summary = man.Summary
		meta.Entrypoints = man.Entrypoints
	}
	if meta.Summary == "" {
		if data, readErr := os.ReadFile(path); readErr == nil {
			meta.Summary = extractFrontMatterSummary(string(data))
		}
	}
	metaByName[name] = meta
}

func Scan(dirs []string) Inventory {
	metaByName := map[string]SkillMeta{}
	for _, dir := range dirs {
		if strings.TrimSpace(dir) == "" {
			continue
		}
		root, err := filepath.Abs(dir)
		if err != nil {
			continue
		}
		root, err = filepath.EvalSymlinks(root)
		if err != nil {
			continue
		}
		_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.Type()&os.ModeSymlink != 0 {
				return nil
			}
			if !d.IsDir() {
				return nil
			}
			if path == root {
				return nil
			}
			realPath, err := filepath.EvalSymlinks(path)
			if err != nil {
				return filepath.SkipDir
			}
			rel, err := filepath.Rel(root, realPath)
			if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				return filepath.SkipDir
			}
			skillPath, ok := skillFileInDir(realPath)
			if !ok {
				return nil
			}
			info, _ := os.Stat(skillPath)
			appendSkill(metaByName, skillPath, filepath.Base(realPath), info)
			return filepath.SkipDir
		})
	}

	skills := make([]SkillMeta, 0, len(metaByName))
	for _, s := range metaByName {
		skills = append(skills, s)
	}
	sort.Slice(skills, func(i, j int) bool {
		if skills[i].Name == skills[j].Name {
			return skills[i].Path < skills[j].Path
		}
		return skills[i].Name < skills[j].Name
	})
	by := map[string]SkillMeta{}
	for _, s := range skills {
		by[s.Name] = s
	}
	return Inventory{Skills: skills, byName: by}
}

func (inv Inventory) Get(name string) (SkillMeta, bool) {
	s, ok := inv.byName[name]
	return s, ok
}

func (inv Inventory) Summary(max int) string {
	if max <= 0 {
		max = 50
	}
	lines := []string{}
	for i, s := range inv.Skills {
		if i >= max {
			lines = append(lines, "…")
			break
		}
		if s.Summary != "" {
			lines = append(lines, "- "+s.Name+": "+s.Summary)
		} else {
			lines = append(lines, "- "+s.Name)
		}
	}
	if len(lines) == 0 {
		return "(no skills found)"
	}
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
