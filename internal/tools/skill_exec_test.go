package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"or3-intern/internal/skills"
)

func makeExecutableSkillInventory(t *testing.T) *skills.Inventory {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "runner")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\nname: runner\ndescription: run scripts\n---\n# Runner\n"), 0o644); err != nil {
		t.Fatalf("skill write: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "tool.sh"), []byte("#!/bin/sh\necho script:$*\n"), 0o755); err != nil {
		t.Fatalf("script write: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "skill.json"), []byte(`{"entrypoints":[{"name":"hello","command":["./tool.sh","entry"]}]}`), 0o644); err != nil {
		t.Fatalf("manifest write: %v", err)
	}
	inv := skills.ScanWithOptions(skills.LoadOptions{Roots: []skills.Root{{Path: root, Source: skills.SourceWorkspace}}})
	return &inv
}

func TestRunSkillScript_PathExecution(t *testing.T) {
	tool := &RunSkillScript{Inventory: makeExecutableSkillInventory(t)}
	out, err := tool.Execute(context.Background(), map[string]any{
		"skill": "runner",
		"path":  "tool.sh",
		"args":  []any{"arg1"},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "script:arg1") {
		t.Fatalf("unexpected output: %q", out)
	}
}

func TestRunSkillScript_EntrypointExecution(t *testing.T) {
	tool := &RunSkillScript{Inventory: makeExecutableSkillInventory(t)}
	out, err := tool.Execute(context.Background(), map[string]any{
		"skill":      "runner",
		"entrypoint": "hello",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "script:entry") {
		t.Fatalf("unexpected output: %q", out)
	}
}

func TestRunSkillScript_EntrypointExecution_AppendsArgs(t *testing.T) {
	tool := &RunSkillScript{Inventory: makeExecutableSkillInventory(t)}
	out, err := tool.Execute(context.Background(), map[string]any{
		"skill":      "runner",
		"entrypoint": "hello",
		"args":       []any{"tail"},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "script:entry tail") {
		t.Fatalf("unexpected output: %q", out)
	}
}

func TestRunSkillScript_RejectsPathEscape(t *testing.T) {
	tool := &RunSkillScript{Inventory: makeExecutableSkillInventory(t)}
	if _, err := tool.Execute(context.Background(), map[string]any{
		"skill": "runner",
		"path":  "../escape.sh",
	}); err == nil {
		t.Fatal("expected path escape to fail")
	}
}
