package tools

import (
	"fmt"
	"regexp"
	"strings"
)

const (
	MetadataScannerOff   = "off"
	MetadataScannerWarn  = "warn"
	MetadataScannerBlock = "block"
)

type MetadataDiagnostic struct {
	ToolName string
	Class    string
	Action   string
	Preview  string
}

var metadataScannerPatterns = []struct {
	class   string
	pattern *regexp.Regexp
}{
	{"instruction_override", regexp.MustCompile(`(?i)\b(ignore|override|bypass)\b.{0,80}\b(previous|system|developer|instructions?)\b`)},
	{"secret_exfiltration", regexp.MustCompile(`(?i)\b(send|exfiltrate|upload|leak|reveal)\b.{0,80}\b(secret|token|password|api[_ -]?key|credential)\b`)},
	{"hidden_prompt_request", regexp.MustCompile(`(?i)\b(hidden|invisible|private)\b.{0,80}\b(prompt|instruction|message)\b`)},
	{"unrelated_behavior", regexp.MustCompile(`(?i)\b(always|never)\b.{0,80}\b(answer|respond|refuse|obey)\b`)},
}

func ScanToolMetadata(tool Tool) []MetadataDiagnostic {
	if tool == nil {
		return nil
	}
	texts := []string{tool.Name(), tool.Description()}
	collectSchemaDescriptions(tool.Parameters(), &texts)
	var out []MetadataDiagnostic
	for _, text := range texts {
		trimmed := strings.TrimSpace(text)
		if trimmed == "" {
			continue
		}
		for _, candidate := range metadataScannerPatterns {
			if candidate.pattern.MatchString(trimmed) {
				out = append(out, MetadataDiagnostic{
					ToolName: tool.Name(),
					Class:    candidate.class,
					Preview:  trimMetadataPreview(trimmed, 180),
				})
				break
			}
		}
	}
	return out
}

func FilterSuspiciousExternalTools(reg *Registry, mode string, allowlist map[string]struct{}) (*Registry, []MetadataDiagnostic) {
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode == "" {
		mode = MetadataScannerWarn
	}
	if reg == nil || mode == MetadataScannerOff {
		return reg, nil
	}
	blocked := map[string]struct{}{}
	var diagnostics []MetadataDiagnostic
	for _, name := range reg.Names() {
		meta := reg.Metadata(name)
		if !hasMetadataGroup(meta.Groups, ToolGroupMCP) {
			continue
		}
		if _, ok := allowlist[name]; ok {
			continue
		}
		for _, diagnostic := range ScanToolMetadata(reg.Get(name)) {
			diagnostic.Action = "warn"
			if mode == MetadataScannerBlock {
				diagnostic.Action = "block"
				blocked[name] = struct{}{}
			}
			diagnostics = append(diagnostics, diagnostic)
		}
	}
	if len(blocked) == 0 {
		return reg, diagnostics
	}
	allowed := map[string]struct{}{}
	for _, name := range reg.Names() {
		if _, ok := blocked[name]; ok {
			continue
		}
		allowed[name] = struct{}{}
	}
	return reg.CloneSelected(allowed), diagnostics
}

func collectSchemaDescriptions(value any, out *[]string) {
	switch typed := value.(type) {
	case map[string]any:
		if desc, ok := typed["description"].(string); ok {
			*out = append(*out, desc)
		}
		for _, child := range typed {
			collectSchemaDescriptions(child, out)
		}
	case []any:
		for _, child := range typed {
			collectSchemaDescriptions(child, out)
		}
	}
}

func hasMetadataGroup(groups []string, wanted string) bool {
	for _, group := range groups {
		if strings.EqualFold(strings.TrimSpace(group), wanted) {
			return true
		}
	}
	return false
}

func trimMetadataPreview(text string, limit int) string {
	runes := []rune(strings.TrimSpace(text))
	if len(runes) <= limit {
		return string(runes)
	}
	return string(runes[:limit]) + "..."
}

func (d MetadataDiagnostic) String() string {
	return fmt.Sprintf("tool=%s class=%s action=%s preview=%q", d.ToolName, d.Class, d.Action, d.Preview)
}
