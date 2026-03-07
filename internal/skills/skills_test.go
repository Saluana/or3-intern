package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func makeSkillsDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "skill_one.md"), []byte("# Skill One\nContent one"), 0o644)
	os.WriteFile(filepath.Join(dir, "skill_two.txt"), []byte("Skill two content"), 0o644)
	os.WriteFile(filepath.Join(dir, "not_a_skill.json"), []byte(`{}`), 0o644)
	return dir
}

func TestScan_Empty(t *testing.T) {
	inv := Scan(nil)
	if len(inv.Skills) != 0 {
		t.Errorf("expected 0 skills, got %d", len(inv.Skills))
	}
}

func TestScan_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	inv := Scan([]string{dir})
	if len(inv.Skills) != 0 {
		t.Errorf("expected 0 skills in empty dir, got %d", len(inv.Skills))
	}
}

func TestScan_BlankDirSkipped(t *testing.T) {
	inv := Scan([]string{"   ", ""})
	if len(inv.Skills) != 0 {
		t.Errorf("expected 0 skills with blank dirs, got %d", len(inv.Skills))
	}
}

func TestScan_FiltersByExtension(t *testing.T) {
	dir := makeSkillsDir(t)
	inv := Scan([]string{dir})
	// should include .md and .txt but not .json
	if len(inv.Skills) != 2 {
		t.Errorf("expected 2 skills, got %d", len(inv.Skills))
	}
	for _, s := range inv.Skills {
		if s.Name == "not_a_skill" {
			t.Error("expected .json file to be excluded")
		}
	}
}

func TestScan_SortedByName(t *testing.T) {
	dir := makeSkillsDir(t)
	inv := Scan([]string{dir})
	for i := 1; i < len(inv.Skills); i++ {
		if inv.Skills[i].Name < inv.Skills[i-1].Name {
			t.Errorf("expected sorted skills, got %q before %q", inv.Skills[i-1].Name, inv.Skills[i].Name)
		}
	}
}

func TestScan_SkillFields(t *testing.T) {
	dir := makeSkillsDir(t)
	inv := Scan([]string{dir})

	for _, s := range inv.Skills {
		if s.Name == "" {
			t.Error("expected non-empty skill name")
		}
		if s.Path == "" {
			t.Error("expected non-empty skill path")
		}
		if s.ID == "" {
			t.Error("expected non-empty skill ID")
		}
		if s.Size <= 0 {
			t.Errorf("expected positive size for %q, got %d", s.Name, s.Size)
		}
	}
}

func TestScan_MultipleDirs(t *testing.T) {
	dir1 := t.TempDir()
	dir2 := t.TempDir()
	os.WriteFile(filepath.Join(dir1, "alpha.md"), []byte("alpha"), 0o644)
	os.WriteFile(filepath.Join(dir2, "beta.md"), []byte("beta"), 0o644)

	inv := Scan([]string{dir1, dir2})
	if len(inv.Skills) != 2 {
		t.Errorf("expected 2 skills, got %d", len(inv.Skills))
	}
}

func TestScan_SkipsSymlinkedSkill(t *testing.T) {
	dir := t.TempDir()
	targetDir := t.TempDir()
	target := filepath.Join(targetDir, "outside.md")
	os.WriteFile(target, []byte("outside"), 0o644)
	link := filepath.Join(dir, "outside.md")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	inv := Scan([]string{dir})
	if len(inv.Skills) != 0 {
		t.Fatalf("expected symlinked skill to be skipped, got %#v", inv.Skills)
	}
}

func TestInventory_Get_Found(t *testing.T) {
	dir := makeSkillsDir(t)
	inv := Scan([]string{dir})

	s, ok := inv.Get("skill_one")
	if !ok {
		t.Fatal("expected to find 'skill_one'")
	}
	if s.Name != "skill_one" {
		t.Errorf("expected name 'skill_one', got %q", s.Name)
	}
}

func TestInventory_Get_NotFound(t *testing.T) {
	dir := makeSkillsDir(t)
	inv := Scan([]string{dir})

	_, ok := inv.Get("nonexistent")
	if ok {
		t.Error("expected 'nonexistent' to not be found")
	}
}

func TestInventory_Summary_Empty(t *testing.T) {
	inv := Scan(nil)
	s := inv.Summary(50)
	if s != "(no skills found)" {
		t.Errorf("expected '(no skills found)', got %q", s)
	}
}

func TestInventory_Summary_WithItems(t *testing.T) {
	dir := makeSkillsDir(t)
	inv := Scan([]string{dir})

	s := inv.Summary(10)
	if !strings.Contains(s, "skill_one") && !strings.Contains(s, "skill_two") {
		t.Errorf("expected summary to contain skill names, got %q", s)
	}
}

func TestInventory_Summary_MaxItems(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 5; i++ {
		name := []string{"aaa", "bbb", "ccc", "ddd", "eee"}[i]
		os.WriteFile(filepath.Join(dir, name+".md"), []byte("content"), 0o644)
	}

	inv := Scan([]string{dir})
	// Limit to 2
	s := inv.Summary(2)
	lines := strings.Split(strings.TrimSpace(s), "\n")
	// 2 items + "…" = 3 lines
	if len(lines) != 3 {
		t.Errorf("expected 3 lines (2 items + ellipsis), got %d: %q", len(lines), s)
	}
	if lines[2] != "…" {
		t.Errorf("expected last line to be '…', got %q", lines[2])
	}
}

func TestInventory_Summary_DefaultMax(t *testing.T) {
	dir := makeSkillsDir(t)
	inv := Scan([]string{dir})
	// passing 0 should use default of 50
	s := inv.Summary(0)
	if s == "" || s == "(no skills found)" {
		t.Errorf("expected summary with content, got %q", s)
	}
}

func TestLoadBody_Normal(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "skill.md")
	content := "# Skill\nSome content here"
	os.WriteFile(path, []byte(content), 0o644)

	got, err := LoadBody(path, 0)
	if err != nil {
		t.Fatalf("LoadBody: %v", err)
	}
	if got != content {
		t.Errorf("expected %q, got %q", content, got)
	}
}

func TestLoadBody_Truncation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "large.md")
	content := strings.Repeat("a", 100)
	os.WriteFile(path, []byte(content), 0o644)

	got, err := LoadBody(path, 50)
	if err != nil {
		t.Fatalf("LoadBody: %v", err)
	}
	if len(got) != 50 {
		t.Errorf("expected 50 bytes, got %d", len(got))
	}
}

func TestLoadBody_FileNotFound(t *testing.T) {
	_, err := LoadBody("/nonexistent/path/skill.md", 0)
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestHash_Deterministic(t *testing.T) {
	h1 := hash("some/path/file.md")
	h2 := hash("some/path/file.md")
	if h1 != h2 {
		t.Errorf("expected deterministic hash, got %q and %q", h1, h2)
	}
}

func TestHash_Different(t *testing.T) {
	h1 := hash("path/a.md")
	h2 := hash("path/b.md")
	if h1 == h2 {
		t.Error("expected different hashes for different paths")
	}
}

// ---- Manifest and front matter ----

func TestSkillManifestParsing(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "myskill.md"), []byte("# My Skill\nDoes things."), 0o644)
	os.WriteFile(filepath.Join(dir, "skill.json"), []byte(`{
		"summary": "a really cool skill",
		"entrypoints": [
			{"name": "run", "command": ["python", "main.py"], "timeoutSeconds": 30, "acceptsStdin": false}
		]
	}`), 0o644)

	inv := Scan([]string{dir})
	if len(inv.Skills) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(inv.Skills))
	}
	s := inv.Skills[0]
	if s.Summary != "a really cool skill" {
		t.Errorf("expected summary 'a really cool skill', got %q", s.Summary)
	}
	if len(s.Entrypoints) != 1 {
		t.Fatalf("expected 1 entrypoint, got %d", len(s.Entrypoints))
	}
	ep := s.Entrypoints[0]
	if ep.Name != "run" {
		t.Errorf("expected entrypoint name 'run', got %q", ep.Name)
	}
	if len(ep.Command) != 2 || ep.Command[0] != "python" {
		t.Errorf("expected command [python main.py], got %v", ep.Command)
	}
	if ep.TimeoutSeconds != 30 {
		t.Errorf("expected timeout 30, got %d", ep.TimeoutSeconds)
	}
	if ep.AcceptsStdin {
		t.Error("expected acceptsStdin false")
	}
}

func TestSkillFrontMatterSummary(t *testing.T) {
	dir := t.TempDir()
	content := "---\nsummary: parses front matter correctly\n---\n# Skill\nBody text."
	os.WriteFile(filepath.Join(dir, "frontmatter.md"), []byte(content), 0o644)

	inv := Scan([]string{dir})
	if len(inv.Skills) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(inv.Skills))
	}
	s := inv.Skills[0]
	if s.Summary != "parses front matter correctly" {
		t.Errorf("expected summary 'parses front matter correctly', got %q", s.Summary)
	}
}

func TestSkillManifestOverridesFrontMatter(t *testing.T) {
	dir := t.TempDir()
	content := "---\nsummary: from front matter\n---\n# Skill"
	os.WriteFile(filepath.Join(dir, "skill.md"), []byte(content), 0o644)
	os.WriteFile(filepath.Join(dir, "skill.json"), []byte(`{"summary":"from manifest"}`), 0o644)

	inv := Scan([]string{dir})
	if len(inv.Skills) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(inv.Skills))
	}
	// manifest takes precedence
	if inv.Skills[0].Summary != "from manifest" {
		t.Errorf("expected manifest summary to take precedence, got %q", inv.Skills[0].Summary)
	}
}

func TestSkillSummaryInInventory(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "alpha.md"), []byte("---\nsummary: does alpha things\n---\n# Alpha"), 0o644)
	os.WriteFile(filepath.Join(dir, "beta.md"), []byte("# Beta\nNo front matter."), 0o644)

	inv := Scan([]string{dir})
	s := inv.Summary(10)

	if !strings.Contains(s, "alpha: does alpha things") {
		t.Errorf("expected summary line 'alpha: does alpha things' in %q", s)
	}
	if !strings.Contains(s, "- beta\n") && !strings.HasSuffix(s, "- beta") {
		t.Errorf("expected plain '- beta' line (no summary) in %q", s)
	}
}

func TestExtractFrontMatterSummary_NoFrontMatter(t *testing.T) {
	got := extractFrontMatterSummary("# Title\nsome body")
	if got != "" {
		t.Errorf("expected empty string for no front matter, got %q", got)
	}
}

func TestExtractFrontMatterSummary_WithSummary(t *testing.T) {
	content := "---\nsummary: hello world\nauthor: test\n---\n# Body"
	got := extractFrontMatterSummary(content)
	if got != "hello world" {
		t.Errorf("expected 'hello world', got %q", got)
	}
}

func TestExtractFrontMatterSummary_WithQuotedSummary(t *testing.T) {
	content := "---\nsummary: \"quoted value\"\n---\n# Body"
	got := extractFrontMatterSummary(content)
	if got != "quoted value" {
		t.Errorf("expected 'quoted value', got %q", got)
	}
}

func TestExtractFrontMatterSummary_MissingSummaryKey(t *testing.T) {
	content := "---\nauthor: someone\n---\n# Body"
	got := extractFrontMatterSummary(content)
	if got != "" {
		t.Errorf("expected empty string when no summary key, got %q", got)
	}
}

func TestSkillManifest_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "myskill.md"), []byte("# Skill"), 0o644)
	os.WriteFile(filepath.Join(dir, "skill.json"), []byte(`not valid json`), 0o644)

	// Should not panic; skill loads without summary/entrypoints
	inv := Scan([]string{dir})
	if len(inv.Skills) != 1 {
		t.Fatalf("expected 1 skill even with invalid manifest, got %d", len(inv.Skills))
	}
	if inv.Skills[0].Summary != "" {
		t.Errorf("expected empty summary for invalid manifest, got %q", inv.Skills[0].Summary)
	}
}

