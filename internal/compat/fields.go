package compat

import "strings"

func FirstString(canonical string, aliases ...string) string {
	values := append([]string{canonical}, aliases...)
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func FirstStringSlice(canonical []string, aliases ...[]string) []string {
	values := append([][]string{canonical}, aliases...)
	for _, value := range values {
		if len(value) == 0 {
			continue
		}
		out := make([]string, len(value))
		copy(out, value)
		return out
	}
	return nil
}
