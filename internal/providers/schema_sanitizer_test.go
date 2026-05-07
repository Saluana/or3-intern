package providers

import "testing"

func TestSchemaSanitizer_ClonesAndStripsUnsupportedKeywords(t *testing.T) {
	canonical := ToolDef{
		Type: "function",
		Function: ToolFunc{
			Name:        "read_file",
			Description: "short",
			Parameters: map[string]any{
				"$schema": "https://json-schema.org/draft/2020-12/schema",
				"type":    "object",
				"default": map[string]any{},
				"properties": map[string]any{
					"path": map[string]any{
						"type":        "string",
						"description": "path",
						"examples":    []any{"README.md"},
					},
				},
				"required": []any{"path"},
			},
		},
	}
	sanitized, report := SchemaSanitizer{Profile: OpenAICompatibleProfile()}.SanitizeToolDef(canonical)
	params := sanitized.Function.Parameters.(map[string]any)
	if _, ok := params["$schema"]; ok {
		t.Fatal("expected $schema to be stripped")
	}
	if _, ok := params["default"]; ok {
		t.Fatal("expected default to be stripped")
	}
	props := params["properties"].(map[string]any)
	path := props["path"].(map[string]any)
	if _, ok := path["examples"]; ok {
		t.Fatal("expected nested examples to be stripped")
	}
	originalParams := canonical.Function.Parameters.(map[string]any)
	if _, ok := originalParams["$schema"]; !ok {
		t.Fatal("canonical schema was mutated")
	}
	if len(report.RemovedKeywords) < 3 {
		t.Fatalf("expected removal diagnostics, got %#v", report)
	}
}

func TestSchemaSanitizer_RewritesMissingObjectRootAndBoundsDescription(t *testing.T) {
	profile := OpenAICompatibleProfile()
	profile.ToolSchema.MaxDescriptionRunes = 4
	def := ToolDef{Type: "function", Function: ToolFunc{Name: "exec", Description: "123456", Parameters: nil}}
	sanitized, report := SchemaSanitizer{Profile: profile}.SanitizeToolDef(def)
	if sanitized.Function.Description != "1234" {
		t.Fatalf("expected bounded description, got %q", sanitized.Function.Description)
	}
	params := sanitized.Function.Parameters.(map[string]any)
	if params["type"] != "object" {
		t.Fatalf("expected object root, got %#v", params)
	}
	if report.TruncatedDescriptions != 1 || len(report.Warnings) == 0 {
		t.Fatalf("expected diagnostics, got %#v", report)
	}
}
