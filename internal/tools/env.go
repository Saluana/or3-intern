package tools

import (
	"os"
	"strings"
)

func BuildChildEnv(base []string, allowlist []string, overlay map[string]string, pathAppend string) []string {
	values := map[string]string{}
	order := make([]string, 0, len(base)+len(overlay)+1)
	allowed := map[string]struct{}{}
	for _, name := range allowlist {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		allowed[name] = struct{}{}
	}
	includeAll := len(allowed) == 0
	for _, raw := range base {
		key, value, ok := strings.Cut(raw, "=")
		if !ok {
			continue
		}
		if !includeAll {
			if _, ok := allowed[key]; !ok {
				continue
			}
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