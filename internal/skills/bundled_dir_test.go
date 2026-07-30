package skills

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveBundledSkillsDirMaterializesMemorySkill(t *testing.T) {
	tmp := t.TempDir()
	dir, err := ResolveBundledSkillsDir(tmp)
	if err != nil {
		t.Fatalf("ResolveBundledSkillsDir: %v", err)
	}
	if !IsBundledSkillInstalled(dir, BundledMemorySkillName) {
		t.Fatalf("memory skill missing under %s", dir)
	}
}

func TestResolveBundledSkillsDirRepairsPartialInstall(t *testing.T) {
	tmp := t.TempDir()
	bundled := filepath.Join(tmp, "builtin_skills")
	if err := os.MkdirAll(filepath.Join(bundled, "runner"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bundled, "runner", "SKILL.md"), []byte("---\nname: runner\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	dir, err := ResolveBundledSkillsDir(tmp)
	if err != nil {
		t.Fatalf("ResolveBundledSkillsDir: %v", err)
	}
	if !IsBundledSkillInstalled(dir, BundledMemorySkillName) {
		t.Fatal("expected partial install to be repaired with memory skill")
	}
}
