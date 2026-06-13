package main

import (
	"path/filepath"

	"or3-intern/internal/config"
	"or3-intern/internal/skills"
)

func resolveBundledSkillsDir(cfgPath string) (string, error) {
	return skills.ResolveBundledSkillsDir(filepath.Dir(cfgPathOrDefault(cfgPath)))
}

func ensureMemorySkillRegistered(cfgPath string, cfg *config.Config) (bool, error) {
	installed, policyChanged, err := skills.EnsureMemorySkillRegistered(filepath.Dir(cfgPathOrDefault(cfgPath)), cfg)
	if err != nil {
		return false, err
	}
	changed := installed || policyChanged
	if !policyChanged {
		return changed, nil
	}
	if err := config.Save(cfgPathOrDefault(cfgPath), *cfg); err != nil {
		return changed, err
	}
	return changed, nil
}
