package approval

import (
	"encoding/json"
	"strings"
)

func quoteCommandPart(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if isSimpleCommandToken(value) {
		return value
	}
	blob, err := json.Marshal(value)
	if err != nil {
		return `""`
	}
	return string(blob)
}

func isSimpleCommandToken(value string) bool {
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '.', r == '/', r == ':', r == '@', r == '%', r == '+', r == '=', r == ',', r == '-', r == '_':
		default:
			return false
		}
	}
	return true
}

// FormatExecCommandDisplay renders argv for human review (matches app quoteCommandPart behavior).
func FormatExecCommandDisplay(executablePath string, argv []string) string {
	parts := append([]string{}, argv...)
	executablePath = strings.TrimSpace(executablePath)
	if len(parts) == 0 && executablePath != "" {
		parts = []string{executablePath}
	}
	quoted := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		quoted = append(quoted, quoteCommandPart(part))
	}
	return strings.TrimSpace(strings.Join(quoted, " "))
}
