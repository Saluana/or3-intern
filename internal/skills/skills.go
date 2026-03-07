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

// extractFrontMatterSummary parses the first YAML front matter block (--- ... ---)
// and returns the value of the "summary:" field if present.
func extractFrontMatterSummary(content string) string {
	lines := strings.SplitN(content, "\n", 20)
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

func Scan(dirs []string) Inventory {
	var skills []SkillMeta
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
		_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.Type()&os.ModeSymlink != 0 {
				return nil
			}
			if d.IsDir() {
				return nil
			}
			ext := strings.ToLower(filepath.Ext(path))
			if ext != ".md" && ext != ".txt" {
				return nil
			}
			realPath, err := filepath.EvalSymlinks(path)
			if err != nil {
				return nil
			}
			rel, err := filepath.Rel(root, realPath)
			if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				return nil
			}
			info, _ := d.Info()
			mt := time.Time{}
			sz := int64(0)
			if info != nil {
				mt = info.ModTime()
				sz = info.Size()
			}
			name := strings.TrimSuffix(filepath.Base(realPath), ext)
			meta := SkillMeta{Name: name, Path: realPath, ModTime: mt, Size: sz, ID: hash(realPath)}

			// Try skill.json manifest in the same directory.
			if man, ok := loadManifest(filepath.Dir(realPath)); ok {
				meta.Summary = man.Summary
				meta.Entrypoints = man.Entrypoints
			}

			// Try YAML front matter summary if not already set from manifest.
			if meta.Summary == "" && ext == ".md" {
				if data, readErr := os.ReadFile(realPath); readErr == nil {
					meta.Summary = extractFrontMatterSummary(string(data))
				}
			}

			skills = append(skills, meta)
			return nil
		})
	}
	sort.Slice(skills, func(i, j int) bool { return skills[i].Name < skills[j].Name })
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
