package tools

import (
	"os"
	"strings"
)

var defaultChildEnvAllowlist = []string{"PATH", "HOME", "TMPDIR", "TMP", "TEMP"}

func EffectiveChildEnvAllowlist(allowlist []string) []string {
	cleaned := make([]string, 0, len(allowlist))
	seen := map[string]struct{}{}
	for _, name := range allowlist {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		cleaned = append(cleaned, name)
	}
	if len(cleaned) > 0 {
		return cleaned
	}
	return append([]string{}, defaultChildEnvAllowlist...)
}

func BuildChildEnv(base []string, allowlist []string, overlay map[string]string, pathAppend string) []string {
	values := map[string]string{}
	order := make([]string, 0, len(base)+len(overlay)+1)
	allowed := map[string]struct{}{}
	for _, name := range EffectiveChildEnvAllowlist(allowlist) {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		allowed[name] = struct{}{}
	}
	for _, raw := range base {
		key, value, ok := strings.Cut(raw, "=")
		if !ok {
			continue
		}
		if _, ok := allowed[key]; !ok {
			continue
		}
		if _, exists := values[key]; !exists {
			order = append(order, key)
		}
		values[key] = value
	}
	for key, value := range overlay {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		if _, exists := values[key]; !exists {
			order = append(order, key)
		}
		values[key] = value
	}
	if pathAppend != "" {
		pathValue := values["PATH"]
		if pathValue == "" {
			pathValue = os.Getenv("PATH")
		}
		values["PATH"] = pathValue + string(os.PathListSeparator) + pathAppend
		seen := false
		for _, key := range order {
			if key == "PATH" {
				seen = true
				break
			}
		}
		if !seen {
			order = append(order, "PATH")
		}
	}
	out := make([]string, 0, len(order))
	for _, key := range order {
		out = append(out, key+"="+values[key])
	}
	return out
}