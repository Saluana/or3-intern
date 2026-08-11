package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"or3-intern/internal/config"
	"or3-intern/internal/skills"
)

func TestEnsureMemorySkillRegisteredPersistsPolicy(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "or3-intern.json")
	cfg := config.Default()
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatal(err)
	}
	changed, err := ensureMemorySkillRegistered(cfgPath, &cfg)
	if err != nil {
		t.Fatalf("ensureMemorySkillRegistered: %v", err)
	}
	if !changed {
		t.Fatal("expected registration changes")
	}
	if !skills.IsBundledSkillInstalled(filepath.Join(tmp, "builtin_skills"), skills.BundledMemorySkillName) {
		t.Fatal("memory skill not installed on disk")
	}
	loaded, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, name := range loaded.Skills.Policy.Approved {
		if strings.EqualFold(name, skills.BundledMemorySkillName) {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("memory not persisted in approved policy: %#v", loaded.Skills.Policy.Approved)
	}
}

func TestEnsureMemorySkillRegisteredDoesNotPersistRuntimeSecrets(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "or3-intern.json")
	persisted := config.Default()
	persisted.Provider.APIKey = "stored-key"
	if err := config.Save(cfgPath, persisted); err != nil {
		t.Fatal(err)
	}

	runtimeCfg := persisted
	runtimeCfg.Provider.APIKey = "environment-only-key"
	changed, err := ensureMemorySkillRegistered(cfgPath, &runtimeCfg)
	if err != nil {
		t.Fatalf("ensureMemorySkillRegistered: %v", err)
	}
	if !changed {
		t.Fatal("expected registration changes")
	}

	loaded, err := config.LoadPersisted(cfgPath)
	if err != nil {
		t.Fatalf("LoadPersisted: %v", err)
	}
	if loaded.Provider.APIKey != "stored-key" {
		t.Fatalf("persisted API key was overwritten: %q", loaded.Provider.APIKey)
	}
}

func TestPrepareRuntimeStorageAutoRegistersMemorySkill(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "or3-intern.json")
	cfg := config.Default()
	cfg.DBPath = filepath.Join(tmp, "state", "or3.db")
	cfg.ArtifactsDir = filepath.Join(tmp, "artifacts")
	cfg.SoulFile = filepath.Join(tmp, "SOUL.md")
	cfg.AgentsFile = filepath.Join(tmp, "AGENTS.md")
	cfg.ToolsFile = filepath.Join(tmp, "TOOLS.md")
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatal(err)
	}
	partial := filepath.Join(tmp, "builtin_skills", "runner")
	if err := os.MkdirAll(partial, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(partial, "SKILL.md"), []byte("---\nname: runner\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := prepareRuntimeStorage(&cfg, cfgPath); err != nil {
		t.Fatalf("prepareRuntimeStorage: %v", err)
	}
	if !skills.IsBundledSkillInstalled(filepath.Join(tmp, "builtin_skills"), skills.BundledMemorySkillName) {
		t.Fatal("prepareRuntimeStorage should install missing memory skill")
	}
}
