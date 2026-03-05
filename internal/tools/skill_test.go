package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"or3-intern/internal/skills"
)

func makeTestSkillsInventory(t *testing.T) *skills.Inventory {
	t.Helper()
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "my_skill.md"), []byte("# My Skill\nDo stuff"), 0o644)

	inv := skills.Scan([]string{dir})
	return &inv
}

func TestReadSkill_NoInventory(t *testing.T) {
	tool := &ReadSkill{}
	_, err := tool.Execute(context.Background(), map[string]any{"name": "any"})
	if err == nil {
		t.Fatal("expected error when inventory is nil")
	}
}

func TestReadSkill_MissingName(t *testing.T) {
	inv := makeTestSkillsInventory(t)
	tool := &ReadSkill{Inventory: inv}
	_, err := tool.Execute(context.Background(), map[string]any{"name": ""})
	if err == nil {
		t.Fatal("expected error for empty name")
	}
}

func TestReadSkill_WhitespaceName(t *testing.T) {
	inv := makeTestSkillsInventory(t)
	tool := &ReadSkill{Inventory: inv}
	_, err := tool.Execute(context.Background(), map[string]any{"name": "   "})
	if err == nil {
		t.Fatal("expected error for whitespace-only name")
	}
}

func TestReadSkill_NotFound(t *testing.T) {
	inv := makeTestSkillsInventory(t)
	tool := &ReadSkill{Inventory: inv}
	_, err := tool.Execute(context.Background(), map[string]any{"name": "nonexistent"})
	if err == nil {
		t.Fatal("expected error for nonexistent skill")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' in error, got %q", err.Error())
	}
}

func TestReadSkill_Success(t *testing.T) {
	inv := makeTestSkillsInventory(t)
	tool := &ReadSkill{Inventory: inv}
	out, err := tool.Execute(context.Background(), map[string]any{"name": "my_skill"})
	if err != nil {
		t.Fatalf("ReadSkill: %v", err)
	}
	if !strings.Contains(out, "my_skill") {
		t.Errorf("expected skill name in output, got %q", out)
	}
	if !strings.Contains(out, "Do stuff") {
		t.Errorf("expected skill body in output, got %q", out)
	}
}

func TestReadSkill_MaxBytes(t *testing.T) {
	dir := t.TempDir()
	content := strings.Repeat("x", 1000)
	os.WriteFile(filepath.Join(dir, "big_skill.md"), []byte(content), 0o644)
	inv := skills.Scan([]string{dir})
	tool := &ReadSkill{Inventory: &inv, MaxBytes: 100}

	out, err := tool.Execute(context.Background(), map[string]any{"name": "big_skill"})
	if err != nil {
		t.Fatalf("ReadSkill: %v", err)
	}
	// The body should be truncated to 100 bytes
	if len(out) > 200 { // allow for the "# Skill: ..." header
		// Check that content portion is limited
		idx := strings.Index(out, "\n\n")
		if idx >= 0 {
			body := out[idx+2:]
			if len(body) > 100 {
				t.Errorf("expected body truncated to 100 bytes, got %d", len(body))
			}
		}
	}
}

func TestReadSkill_MaxBytesParam(t *testing.T) {
	inv := makeTestSkillsInventory(t)
	tool := &ReadSkill{Inventory: inv}
	out, err := tool.Execute(context.Background(), map[string]any{
		"name":     "my_skill",
		"maxBytes": float64(10),
	})
	if err != nil {
		t.Fatalf("ReadSkill: %v", err)
	}
	_ = out
}

func TestReadSkill_Name(t *testing.T) {
	tool := &ReadSkill{}
	if tool.Name() != "read_skill" {
		t.Errorf("expected 'read_skill', got %q", tool.Name())
	}
}

func TestReadSkill_Schema(t *testing.T) {
	tool := &ReadSkill{}
	schema := tool.Schema()
	if schema["type"] != "function" {
		t.Errorf("expected 'function', got %v", schema["type"])
	}
}
