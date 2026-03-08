package tools

import (
	"context"
	"fmt"
	"strings"

	"or3-intern/internal/skills"
)

type ReadSkill struct {
	Base
	Inventory *skills.Inventory
	MaxBytes  int
}

func (t *ReadSkill) Name() string { return "read_skill" }
func (t *ReadSkill) Description() string {
	return "Read the full body of a skill by name (for ClawHub-compatible SKILL.md usage)."
}
func (t *ReadSkill) Parameters() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{
		"name":     map[string]any{"type": "string", "description": "Skill name from inventory"},
		"maxBytes": map[string]any{"type": "integer", "description": "Optional max bytes"},
	}, "required": []string{"name"}}
}
func (t *ReadSkill) Schema() map[string]any {
	return t.SchemaFor(t.Name(), t.Description(), t.Parameters())
}

func (t *ReadSkill) Execute(ctx context.Context, params map[string]any) (string, error) {
	_ = ctx
	if t.Inventory == nil {
		return "", fmt.Errorf("skills inventory not configured")
	}
	name := strings.TrimSpace(fmt.Sprint(params["name"]))
	if name == "" {
		return "", fmt.Errorf("missing name")
	}
	s, ok := t.Inventory.Get(name)
	if !ok {
		return "", fmt.Errorf("skill not found: %s", name)
	}
	maxBytes := t.MaxBytes
	if maxBytes <= 0 {
		maxBytes = 200000
	}
	if v, ok := params["maxBytes"].(float64); ok && int(v) > 0 {
		maxBytes = int(v)
	}
	body, err := skills.LoadBody(s.Path, maxBytes)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("# Skill: %s (%s, %s)\n\n%s", s.Name, s.Source, s.Dir, body), nil
}
