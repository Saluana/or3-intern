package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"or3-intern/internal/config"
)

func TestIsBundledSkillInstalled(t *testing.T) {
	tmp := t.TempDir()
	if IsBundledSkillInstalled(tmp, BundledMemorySkillName) {
		t.Fatal("expected missing memory skill")
	}
	skillDir := filepath.Join(tmp, BundledMemorySkillName)
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: memory\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !IsBundledSkillInstalled(tmp, BundledMemorySkillName) {
		t.Fatal("expected installed memory skill")
	}
}

func TestEnsureBundledSkillInstalledRepairsPartialTree(t *testing.T) {
	tmp := t.TempDir()
	bundled := filepath.Join(tmp, "builtin_skills")
	if err := os.MkdirAll(filepath.Join(bundled, "runner"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bundled, "runner", "SKILL.md"), []byte("---\nname: runner\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if IsBundledSkillInstalled(bundled, BundledMemorySkillName) {
		t.Fatal("memory skill should still be missing in partial tree")
	}
	installed, err := EnsureBundledSkillInstalled(bundled, BundledMemorySkillName)
	if err != nil {
		t.Fatalf("EnsureBundledSkillInstalled: %v", err)
	}
	if !installed {
		t.Fatal("expected install to report changes")
	}
	if !IsBundledSkillInstalled(bundled, BundledMemorySkillName) {
		t.Fatal("memory skill should be installed after repair")
	}
	body, err := os.ReadFile(filepath.Join(bundled, BundledMemorySkillName, "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "name: memory") {
		t.Fatalf("unexpected skill body: %q", body)
	}
	again, err := EnsureBundledSkillInstalled(bundled, BundledMemorySkillName)
	if err != nil {
		t.Fatal(err)
	}
	if again {
		t.Fatal("second ensure should be a no-op")
	}
}

func TestEnsureMemorySkillPolicyAddsApproval(t *testing.T) {
	cfg := config.Default()
	if EnsureMemorySkillPolicy(&cfg) != true {
		t.Fatal("expected first registration to modify config")
	}
	if !containsString(cfg.Skills.Policy.Approved, BundledMemorySkillName) {
		t.Fatalf("approved=%#v", cfg.Skills.Policy.Approved)
	}
	if EnsureMemorySkillPolicy(&cfg) {
		t.Fatal("expected second registration to be a no-op")
	}
}

func TestEnsureMemorySkillRegisteredInstallsAndApproves(t *testing.T) {
	tmp := t.TempDir()
	cfg := config.Default()
	installed, policyChanged, err := EnsureMemorySkillRegistered(tmp, &cfg)
	if err != nil {
		t.Fatalf("EnsureMemorySkillRegistered: %v", err)
	}
	if !installed {
		t.Fatal("expected memory skill install")
	}
	if !policyChanged {
		t.Fatal("expected policy registration")
	}
	bundled := filepath.Join(tmp, "builtin_skills")
	if !IsBundledSkillInstalled(bundled, BundledMemorySkillName) {
		t.Fatal("memory skill missing on disk")
	}
	inv := ScanWithOptions(LoadOptions{
		Roots: []Root{{Path: bundled, Source: SourceBundled}},
	})
	skill, ok := inv.Get(BundledMemorySkillName)
	if !ok || !skill.Eligible || skill.Source != SourceBundled {
		t.Fatalf("unexpected inventory skill: ok=%v %#v", ok, skill)
	}
}

func TestListBundledSkills_ReturnsExpectedDirectories(t *testing.T) {
	names, err := ListBundledSkills()
	if err != nil {
		t.Fatalf("ListBundledSkills: %v", err)
	}
	if len(names) == 0 {
		t.Fatal("expected at least one bundled skill")
	}
	// Should include all RequiredBundledSkills.
	for _, required := range RequiredBundledSkills {
		found := false
		for _, name := range names {
			if name == required {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("required bundled skill %q not in list: %v", required, names)
		}
	}
	// embed.go should not appear as a skill.
	for _, name := range names {
		if name == "embed.go" {
			t.Fatalf("embed.go should not appear in bundled skill list")
		}
	}
}

func TestInstallAllBundledSkills_MaterializesAllToTarget(t *testing.T) {
	tmp := t.TempDir()
	installed, err := InstallAllBundledSkills(tmp)
	if err != nil {
		t.Fatalf("InstallAllBundledSkills: %v", err)
	}
	if len(installed) == 0 {
		t.Fatal("expected at least one installed skill")
	}
	for _, name := range installed {
		if !IsBundledSkillInstalled(tmp, name) {
			t.Fatalf("skill %q should be installed at %s", name, tmp)
		}
		// Verify SKILL.md is readable.
		path := filepath.Join(tmp, name, "SKILL.md")
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s/SKILL.md: %v", name, err)
		}
		if len(body) == 0 {
			t.Fatalf("SKILL.md for %q is empty", name)
		}
	}
}

func TestInstallAllBundledSkills_Idempotent(t *testing.T) {
	tmp := t.TempDir()
	first, err := InstallAllBundledSkills(tmp)
	if err != nil {
		t.Fatalf("first InstallAllBundledSkills: %v", err)
	}
	second, err := InstallAllBundledSkills(tmp)
	if err != nil {
		t.Fatalf("second InstallAllBundledSkills: %v", err)
	}
	// Second install should still report them all (the materialize function
	// writes files regardless, which is idempotent at the filesystem level).
	if len(second) != len(first) {
		t.Fatalf("expected same number of skills (first=%d, second=%d)", len(first), len(second))
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), want) {
			return true
		}
	}
	return false
}
