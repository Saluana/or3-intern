package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"or3-intern/internal/skills"
)

func makeRunSkillInventory(t *testing.T, manifest string) (*skills.Inventory, string) {
	t.Helper()
	dir := t.TempDir()
	// Write the skill markdown file
	if err := os.WriteFile(filepath.Join(dir, "testskill.md"), []byte("# Test Skill\nDoes things."), 0o644); err != nil {
		t.Fatal(err)
	}
	// Write skill.json manifest
	if manifest != "" {
		if err := os.WriteFile(filepath.Join(dir, "skill.json"), []byte(manifest), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	inv := skills.Scan([]string{dir})
	return &inv, dir
}

func TestRunSkillNoInventory(t *testing.T) {
	tool := &RunSkill{}
	_, err := tool.Execute(context.Background(), map[string]any{"skill": "any"})
	if err == nil {
		t.Fatal("expected error when inventory is nil")
	}
}

func TestRunSkillNotFound(t *testing.T) {
	inv, _ := makeRunSkillInventory(t, "")
	tool := &RunSkill{Inventory: inv}
	_, err := tool.Execute(context.Background(), map[string]any{"skill": "nonexistent"})
	if err == nil {
		t.Fatal("expected error for missing skill")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' in error, got %q", err.Error())
	}
}

func TestRunSkillNoEntrypoints(t *testing.T) {
	manifest := `{"summary":"no eps","entrypoints":[]}`
	inv, _ := makeRunSkillInventory(t, manifest)
	tool := &RunSkill{Inventory: inv}
	_, err := tool.Execute(context.Background(), map[string]any{"skill": "testskill"})
	if err == nil {
		t.Fatal("expected error for skill with no entrypoints")
	}
	if !strings.Contains(err.Error(), "no declared entrypoints") {
		t.Errorf("expected 'no declared entrypoints' in error, got %q", err.Error())
	}
}

func TestRunSkillEntrypointNotFound(t *testing.T) {
	manifest := buildManifest([]skills.SkillEntry{
		{Name: "run", Command: []string{"echo", "hello"}, TimeoutSeconds: 5},
	})
	inv, _ := makeRunSkillInventory(t, manifest)
	tool := &RunSkill{Inventory: inv}
	_, err := tool.Execute(context.Background(), map[string]any{
		"skill":      "testskill",
		"entrypoint": "nonexistent",
	})
	if err == nil {
		t.Fatal("expected error for missing entrypoint")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' in error, got %q", err.Error())
	}
}

func TestRunSkillSuccess(t *testing.T) {
	manifest := buildManifest([]skills.SkillEntry{
		{Name: "run", Command: []string{"echo", "hello world"}, TimeoutSeconds: 5},
	})
	inv, _ := makeRunSkillInventory(t, manifest)
	tool := &RunSkill{Inventory: inv}
	out, err := tool.Execute(context.Background(), map[string]any{"skill": "testskill"})
	if err != nil {
		t.Fatalf("RunSkill.Execute: %v", err)
	}
	if !strings.Contains(out, "hello world") {
		t.Errorf("expected 'hello world' in output, got %q", out)
	}
}

func TestRunSkillEntrypointNamedSelection(t *testing.T) {
	manifest := buildManifest([]skills.SkillEntry{
		{Name: "first", Command: []string{"echo", "first-output"}, TimeoutSeconds: 5},
		{Name: "second", Command: []string{"echo", "second-output"}, TimeoutSeconds: 5},
	})
	inv, _ := makeRunSkillInventory(t, manifest)
	tool := &RunSkill{Inventory: inv}

	// Select first
	out, err := tool.Execute(context.Background(), map[string]any{
		"skill":      "testskill",
		"entrypoint": "first",
	})
	if err != nil {
		t.Fatalf("RunSkill.Execute (first): %v", err)
	}
	if !strings.Contains(out, "first-output") {
		t.Errorf("expected 'first-output', got %q", out)
	}

	// Select second
	out, err = tool.Execute(context.Background(), map[string]any{
		"skill":      "testskill",
		"entrypoint": "second",
	})
	if err != nil {
		t.Fatalf("RunSkill.Execute (second): %v", err)
	}
	if !strings.Contains(out, "second-output") {
		t.Errorf("expected 'second-output', got %q", out)
	}
}

func TestRunSkillName(t *testing.T) {
	tool := &RunSkill{}
	if tool.Name() != "run_skill" {
		t.Errorf("expected 'run_skill', got %q", tool.Name())
	}
}

func TestRunSkillSchema(t *testing.T) {
	tool := &RunSkill{}
	schema := tool.Schema()
	if schema["type"] != "function" {
		t.Errorf("expected schema type 'function', got %v", schema["type"])
	}
}

// buildManifest marshals a skill manifest JSON for use in tests.
func buildManifest(entries []skills.SkillEntry) string {
	type manifest struct {
		Summary     string             `json:"summary"`
		Entrypoints []skills.SkillEntry `json:"entrypoints"`
	}
	b, _ := json.Marshal(manifest{Summary: "test skill", Entrypoints: entries})
	return string(b)
}
