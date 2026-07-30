package main

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"or3-intern/internal/config"
	"or3-intern/internal/db"
	"or3-intern/internal/memorysvc"
	"or3-intern/internal/skills"
)

func TestRunMemoryCommandSearchJSON(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "memory-cli.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := database.UpsertPinned(context.Background(), "sess:1", "color", "blue"); err != nil {
		t.Fatal(err)
	}
	svc := memorysvc.New(config.Default(), database, nil, "fp")
	var stdout bytes.Buffer
	deps := memoryCommandDeps{
		NewService: func(config.Config, *db.DB) *memorysvc.Service { return svc },
		Stdout:     &stdout,
		Stderr:     &stdout,
	}
	if err := runMemoryCommandWithDeps(context.Background(), config.Default(), database, []string{"search", "--session", "sess:1", "color"}, deps); err != nil {
		t.Fatalf("search: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("json: %v body=%q", err, stdout.String())
	}
	if _, ok := payload["hits"]; !ok {
		t.Fatalf("expected hits in %#v", payload)
	}
}

func TestRunMemoryCommandAddNoteAndPinnedRoundTrip(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "memory-cli.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	svc := memorysvc.New(config.Default(), database, nil, "fp")
	deps := memoryCommandDeps{
		NewService: func(config.Config, *db.DB) *memorysvc.Service { return svc },
	}
	var stdout bytes.Buffer
	deps.Stdout = &stdout
	deps.Stderr = &stdout
	if err := runMemoryCommandWithDeps(context.Background(), config.Default(), database, []string{"add-note", "--session", "sess:1", "durable lesson"}, deps); err != nil {
		t.Fatalf("add-note: %v", err)
	}
	stdout.Reset()
	if err := runMemoryCommandWithDeps(context.Background(), config.Default(), database, []string{"pinned", "set", "--session", "sess:1", "--key", "theme", "dark"}, deps); err != nil {
		t.Fatalf("pinned set: %v", err)
	}
	stdout.Reset()
	if err := runMemoryCommandWithDeps(context.Background(), config.Default(), database, []string{"pinned", "get", "--session", "sess:1", "--key", "theme"}, deps); err != nil {
		t.Fatalf("pinned get: %v", err)
	}
	var payload struct {
		Entries []memorysvc.PinnedEntry `json:"entries"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("json: %v", err)
	}
	if len(payload.Entries) != 1 || payload.Entries[0].Content != "dark" {
		t.Fatalf("unexpected pinned payload: %#v", payload)
	}
}

func TestRunMemoryCommandGlobalPinnedIsolation(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "memory-cli.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	svc := memorysvc.New(config.Default(), database, nil, "fp")
	deps := memoryCommandDeps{
		NewService: func(config.Config, *db.DB) *memorysvc.Service { return svc },
		Stdout:     &bytes.Buffer{},
		Stderr:     &bytes.Buffer{},
	}
	if err := runMemoryCommandWithDeps(context.Background(), config.Default(), database, []string{"pinned", "set", "--session", "sess:a", "--key", "local", "session-only"}, deps); err != nil {
		t.Fatalf("pinned set session: %v", err)
	}
	entries, err := svc.GetPinned(context.Background(), "sess:b", "local", false)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.Key == "local" {
			t.Fatal("session-scoped pin should not leak to another session")
		}
	}
	var stdout bytes.Buffer
	deps.Stdout = &stdout
	if err := runMemoryCommandWithDeps(context.Background(), config.Default(), database, []string{"pinned", "get", "--session", "sess:a", "--global"}, deps); err != nil {
		t.Fatalf("pinned get global-only: %v", err)
	}
}

func TestRunMemoryCommandValidationErrors(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "memory-cli.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	svc := memorysvc.New(config.Default(), database, nil, "fp")
	deps := memoryCommandDeps{
		NewService: func(config.Config, *db.DB) *memorysvc.Service { return svc },
		Stdout:     &bytes.Buffer{},
		Stderr:     &bytes.Buffer{},
	}
	cases := []struct {
		args []string
		want string
	}{
		{[]string{"search", "--session", "sess:1"}, "query"},
		{[]string{"add-note", "--session", "sess:1"}, "text"},
		{[]string{"pinned", "set", "--session", "sess:1", "--key", "k"}, "content"},
		{[]string{"pinned", "get"}, "session"},
	}
	for _, tc := range cases {
		err := runMemoryCommandWithDeps(context.Background(), config.Default(), database, tc.args, deps)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("args=%v err=%v want %q", tc.args, err, tc.want)
		}
	}
}

func TestMemorySkillBundledInventory(t *testing.T) {
	tmp := t.TempDir()
	bundled, err := skills.ResolveBundledSkillsDir(tmp)
	if err != nil {
		t.Fatalf("ResolveBundledSkillsDir: %v", err)
	}
	cfg := config.Default()
	inv := buildSkillsInventory(cfg, bundled, map[string]struct{}{"exec": {}})
	skill, ok := inv.Get("memory")
	if !ok {
		t.Fatal("memory skill not found in inventory")
	}
	if skill.Source != skills.SourceBundled {
		t.Fatalf("source=%q want bundled", skill.Source)
	}
	if !skill.Eligible {
		t.Fatalf("memory skill should be eligible: %#v", skill)
	}
	if skill.Dir == "" || !strings.Contains(skill.Dir, "memory") {
		t.Fatalf("unexpected skill dir: %q", skill.Dir)
	}
}

func TestRunMemoryCommandAutoRegistersMissingSkill(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "or3-intern.json")
	cfg := config.Default()
	cfg.DBPath = filepath.Join(tmp, "or3.db")
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatal(err)
	}
	database, err := db.Open(cfg.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	svc := memorysvc.New(cfg, database, nil, "fp")
	deps := memoryCommandDeps{
		NewService: func(config.Config, *db.DB) *memorysvc.Service { return svc },
		Stdout:     &bytes.Buffer{},
		Stderr:     &bytes.Buffer{},
	}
	if skills.IsBundledSkillInstalled(filepath.Join(tmp, "builtin_skills"), skills.BundledMemorySkillName) {
		t.Fatal("memory skill should start uninstalled")
	}
	changed, err := ensureMemorySkillRegistered(cfgPath, &cfg)
	if err != nil {
		t.Fatalf("ensureMemorySkillRegistered: %v", err)
	}
	if !changed {
		t.Fatal("expected auto-registration")
	}
	if err := runMemoryCommandWithDeps(context.Background(), cfg, database, []string{"pinned", "get", "--session", "sess:1"}, deps); err != nil {
		t.Fatalf("memory command after registration: %v", err)
	}
	if !skills.IsBundledSkillInstalled(filepath.Join(tmp, "builtin_skills"), skills.BundledMemorySkillName) {
		t.Fatal("memory skill should be installed after command bootstrap")
	}
}
