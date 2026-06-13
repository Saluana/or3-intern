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

func containsString(values []string, want string) bool {
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), want) {
			return true
		}
	}
	return false
}
