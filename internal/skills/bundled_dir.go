package skills

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ResolveBundledSkillsDir returns a stable on-disk bundled-skills directory next to
// the OR3 config, materializing embedded content when required bundled skills are missing.
func ResolveBundledSkillsDir(cfgDir string) (string, error) {
	cfgDir = strings.TrimSpace(cfgDir)
	if cfgDir == "" {
		cfgDir = "."
	}
	target := filepath.Join(cfgDir, "builtin_skills")
	if err := EnsureBundledSkillsMaterialized(target); err != nil {
		return "", err
	}
	return target, nil
}

// EnsureBundledSkillsMaterialized writes any missing required bundled skills to target.
func EnsureBundledSkillsMaterialized(target string) error {
	target = strings.TrimSpace(target)
	if target == "" {
		return fmt.Errorf("bundled skills target path is required")
	}
	if err := os.MkdirAll(target, 0o755); err != nil {
		return fmt.Errorf("mkdir bundled skills: %w", err)
	}
	for _, skillName := range RequiredBundledSkills {
		if _, err := EnsureBundledSkillInstalled(target, skillName); err != nil {
			return err
		}
	}
	return nil
}
