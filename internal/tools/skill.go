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
	return "Read a bounded skill summary or preview by name (for ClawHub-compatible SKILL.md usage)."
}
func (t *ReadSkill) CapabilityForParams(params map[string]any) CapabilityLevel {
	if strings.EqualFold(strings.TrimSpace(fmt.Sprint(params["mode"])), "full") {
		return CapabilityGuarded
	}
	return CapabilitySafe
}
func (t *ReadSkill) Parameters() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{
		"name":     map[string]any{"type": "string", "description": "Skill name from inventory"},
		"mode":     map[string]any{"type": "string", "enum": []string{"preview", "full", "outline"}, "description": "Read mode (default preview). full is still bounded and may spill to an artifact at runtime."},
		"maxBytes": map[string]any{"type": "integer", "description": "Max preview bytes (default 6000)"},
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
		maxBytes = 6000
	}
	if v, ok := params["maxBytes"].(float64); ok && int(v) > 0 {
		maxBytes = int(v)
	}
	mode := strings.ToLower(strings.TrimSpace(fmt.Sprint(params["mode"])))
	if mode == "" || mode == "<nil>" {
		mode = "preview"
	}
	body, err := skills.LoadBody(s.Path, maxBytes)
	if err != nil {
		return "", err
	}
	content := fmt.Sprintf("# Skill: %s (%s, %s)\n\n%s", s.Name, s.Source, s.Dir, body)
	if mode == "outline" {
		content = skillOutline(s, body)
	}
	preview, truncated := PreviewString(content, maxBytes)
	if mode == "full" && s.Size > int64(maxBytes) {
		truncated = true
	}
	return EncodeToolResult(ToolResult{
		Kind:    "skill_read",
		OK:      true,
		Summary: skillSummary(s, mode, truncated),
		Preview: preview,
		Stats: map[string]any{
			"name":        s.Name,
			"source":      string(s.Source),
			"dir":         s.Dir,
			"path":        s.Path,
			"mode":        mode,
			"bytes":       s.Size,
			"max_bytes":   maxBytes,
			"truncated":   truncated,
			"eligible":    s.Eligible,
			"tools":       s.AllowedTools,
			"entrypoints": len(s.Entrypoints),
		},
	}), nil
}

func skillSummary(s skills.SkillMeta, mode string, truncated bool) string {
	parts := []string{fmt.Sprintf("Skill %s", s.Name)}
	if strings.TrimSpace(s.Description) != "" {
		parts = append(parts, s.Description)
	} else if strings.TrimSpace(s.Summary) != "" {
		parts = append(parts, s.Summary)
	}
	parts = append(parts, "mode="+mode)
	if truncated {
		parts = append(parts, "truncated")
	}
	return strings.Join(parts, " | ")
}

func skillOutline(s skills.SkillMeta, body string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "name: %s\n", s.Name)
	if s.Description != "" {
		fmt.Fprintf(&b, "purpose: %s\n", s.Description)
	} else if s.Summary != "" {
		fmt.Fprintf(&b, "purpose: %s\n", s.Summary)
	}
	if len(s.AllowedTools) > 0 {
		fmt.Fprintf(&b, "inputs/tools: %s\n", strings.Join(s.AllowedTools, ", "))
	}
	if len(s.Entrypoints) > 0 {
		names := make([]string, 0, len(s.Entrypoints))
		for _, entry := range s.Entrypoints {
			names = append(names, entry.Name)
		}
		fmt.Fprintf(&b, "entrypoints: %s\n", strings.Join(names, ", "))
	}
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		lower := strings.ToLower(line)
		if strings.HasPrefix(line, "#") ||
			strings.Contains(lower, "when to use") ||
			strings.Contains(lower, "constraint") ||
			strings.Contains(lower, "input") {
			b.WriteString(line)
			b.WriteByte('\n')
		}
	}
	return strings.TrimSpace(b.String())
}
