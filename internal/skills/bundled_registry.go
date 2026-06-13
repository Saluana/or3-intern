package skills

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	builtinembed "or3-intern/builtin_skills"

	"or3-intern/internal/config"
)

// BundledMemorySkillName is the first-party memory bridge skill shipped with or3-intern.
const BundledMemorySkillName = "memory"

// RequiredBundledSkills must be present under the bundled skills root for a complete install.
var RequiredBundledSkills = []string{BundledMemorySkillName}

// IsBundledSkillInstalled reports whether a bundled skill directory contains SKILL.md.
func IsBundledSkillInstalled(bundledDir, skillName string) bool {
	bundledDir = strings.TrimSpace(bundledDir)
	skillName = strings.TrimSpace(skillName)
	if bundledDir == "" || skillName == "" {
		return false
	}
	_, err := os.Stat(filepath.Join(bundledDir, skillName, "SKILL.md"))
	return err == nil
}

// EnsureBundledSkillInstalled materializes one bundled skill from the embedded tree when missing.
// The returned bool is true when files were written.
func EnsureBundledSkillInstalled(bundledDir, skillName string) (bool, error) {
	if IsBundledSkillInstalled(bundledDir, skillName) {
		return false, nil
	}
	if err := materializeBundledSkill(bundledDir, skillName); err != nil {
		return false, err
	}
	if !IsBundledSkillInstalled(bundledDir, skillName) {
		return false, fmt.Errorf("bundled skill %q still missing after materialize", skillName)
	}
	return true, nil
}

// EnsureMemorySkillPolicy adds the memory skill to the approved policy list when absent.
// The returned bool is true when cfg was modified.
func EnsureMemorySkillPolicy(cfg *config.Config) bool {
	if cfg == nil {
		return false
	}
	for _, name := range cfg.Skills.Policy.Approved {
		if strings.EqualFold(strings.TrimSpace(name), BundledMemorySkillName) {
			return false
		}
	}
	cfg.Skills.Policy.Approved = append(cfg.Skills.Policy.Approved, BundledMemorySkillName)
	return true
}

// EnsureMemorySkillRegistered installs the bundled memory skill on disk when missing and
// ensures it is present in the approved-skills policy list.
func EnsureMemorySkillRegistered(cfgDir string, cfg *config.Config) (installed bool, policyChanged bool, err error) {
	cfgDir = strings.TrimSpace(cfgDir)
	if cfgDir == "" {
		cfgDir = "."
	}
	target := filepath.Join(cfgDir, "builtin_skills")
	wasInstalled := IsBundledSkillInstalled(target, BundledMemorySkillName)
	bundledDir, err := ResolveBundledSkillsDir(cfgDir)
	if err != nil {
		return false, false, err
	}
	installed = !wasInstalled && IsBundledSkillInstalled(bundledDir, BundledMemorySkillName)
	policyChanged = EnsureMemorySkillPolicy(cfg)
	return installed, policyChanged, nil
}

func materializeBundledSkill(targetDir, skillName string) error {
	targetDir = strings.TrimSpace(targetDir)
	skillName = strings.TrimSpace(skillName)
	if targetDir == "" || skillName == "" {
		return fmt.Errorf("bundled skill target path and name are required")
	}
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return fmt.Errorf("mkdir bundled skills: %w", err)
	}
	prefix := skillName + "/"
	found := false
	err := fs.WalkDir(builtinembed.FS, ".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == "." || entry.Name() == "embed.go" {
			return nil
		}
		slashPath := filepath.ToSlash(path)
		if !strings.HasPrefix(slashPath, prefix) {
			return nil
		}
		found = true
		rel := filepath.FromSlash(path)
		dest := filepath.Join(targetDir, rel)
		if entry.IsDir() {
			return os.MkdirAll(dest, 0o755)
		}
		data, err := fs.ReadFile(builtinembed.FS, path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return err
		}
		return os.WriteFile(dest, data, 0o644)
	})
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("bundled skill %q not found in embed", skillName)
	}
	return nil
}
