package providers

import (
	"fmt"
	"strings"
)

type SchemaSanitizer struct {
	Profile ProviderProfile
}

type SchemaSanitizationReport struct {
	ToolName              string
	RemovedKeywords       []string
	TruncatedDescriptions int
	Warnings              []string
}

func (s SchemaSanitizer) SanitizeToolDef(def ToolDef) (ToolDef, SchemaSanitizationReport) {
	profile := s.Profile
	if profile.Name == "" {
		profile = OpenAICompatibleProfile()
	}
	report := SchemaSanitizationReport{ToolName: def.Function.Name}
	out := def
	out.Function.Description = trimDescription(out.Function.Description, profile.ToolSchema.MaxDescriptionRunes, &report)
	out.Function.Parameters = cloneJSONValue(def.Function.Parameters)
	root, ok := out.Function.Parameters.(map[string]any)
	if !ok || root == nil {
		if profile.ToolSchema.RequireObjectRoot {
			root = map[string]any{"type": "object", "properties": map[string]any{}}
			out.Function.Parameters = root
			report.Warnings = append(report.Warnings, "parameters_root_rewritten")
		}
		return out, report
	}
	if profile.ToolSchema.RequireObjectRoot {
		if typ, _ := root["type"].(string); strings.TrimSpace(typ) == "" {
			root["type"] = "object"
		}
		if _, ok := root["properties"]; !ok {
			root["properties"] = map[string]any{}
		}
	}
	drop := map[string]struct{}{}
	for _, key := range profile.ToolSchema.DropUnsupportedKeywords {
		drop[key] = struct{}{}
	}
	sanitizeSchemaNode(root, drop, profile.ToolSchema.MaxDescriptionRunes, &report)
	return out, report
}

func SanitizeToolDefs(defs []ToolDef, profile ProviderProfile) ([]ToolDef, []SchemaSanitizationReport) {
	out := make([]ToolDef, 0, len(defs))
	reports := make([]SchemaSanitizationReport, 0, len(defs))
	sanitizer := SchemaSanitizer{Profile: profile}
	for _, def := range defs {
		next, report := sanitizer.SanitizeToolDef(def)
		out = append(out, next)
		if len(report.RemovedKeywords) > 0 || report.TruncatedDescriptions > 0 || len(report.Warnings) > 0 {
			reports = append(reports, report)
		}
	}
	return out, reports
}

func sanitizeSchemaNode(node any, drop map[string]struct{}, maxDesc int, report *SchemaSanitizationReport) {
	switch typed := node.(type) {
	case map[string]any:
		for key, value := range typed {
			if _, shouldDrop := drop[key]; shouldDrop {
				delete(typed, key)
				report.RemovedKeywords = append(report.RemovedKeywords, key)
				continue
			}
			if key == "description" {
				if text, ok := value.(string); ok {
					typed[key] = trimDescription(text, maxDesc, report)
				}
				continue
			}
			sanitizeSchemaNode(value, drop, maxDesc, report)
		}
	case []any:
		for _, item := range typed {
			sanitizeSchemaNode(item, drop, maxDesc, report)
		}
	case []string:
		_ = typed
	}
}

func trimDescription(text string, limit int, report *SchemaSanitizationReport) string {
	if limit <= 0 {
		return text
	}
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	if report != nil {
		report.TruncatedDescriptions++
	}
	return string(runes[:limit])
}

func cloneJSONValue(v any) any {
	switch typed := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, value := range typed {
			out[key] = cloneJSONValue(value)
		}
		return out
	case []any:
		out := make([]any, len(typed))
		for i, value := range typed {
			out[i] = cloneJSONValue(value)
		}
		return out
	case []string:
		out := make([]string, len(typed))
		copy(out, typed)
		return out
	case []map[string]any:
		out := make([]any, 0, len(typed))
		for _, value := range typed {
			out = append(out, cloneJSONValue(value))
		}
		return out
	default:
		return typed
	}
}

func (r SchemaSanitizationReport) String() string {
	parts := []string{}
	if r.ToolName != "" {
		parts = append(parts, "tool="+r.ToolName)
	}
	if len(r.RemovedKeywords) > 0 {
		parts = append(parts, fmt.Sprintf("removed=%s", strings.Join(r.RemovedKeywords, ",")))
	}
	if r.TruncatedDescriptions > 0 {
		parts = append(parts, fmt.Sprintf("truncated=%d", r.TruncatedDescriptions))
	}
	if len(r.Warnings) > 0 {
		parts = append(parts, fmt.Sprintf("warnings=%s", strings.Join(r.Warnings, ",")))
	}
	return strings.Join(parts, " ")
}
