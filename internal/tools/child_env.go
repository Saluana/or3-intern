package tools

import (
	"os"
	"strings"
)

var defaultChildEnvAllowlist = []string{"PATH", "HOME", "TMPDIR", "TMP", "TEMP"}

// EffectiveChildEnvAllowlist returns the configured allowlist or the default child env keys.
func EffectiveChildEnvAllowlist(allowlist []string) []string {
	if len(allowlist) == 0 {
		return append([]string{}, defaultChildEnvAllowlist...)
	}
	out := make([]string, 0, len(allowlist))
	for _, name := range allowlist {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		out = append(out, name)
	}
	return out
}

// BuildChildEnv constructs a child process environment from base, filtered by allowlist,
// with overlay values applied and optional PATH append segments.
func BuildChildEnv(base []string, allowlist []string, overlay map[string]string, pathAppend string) []string {
	effective := EffectiveChildEnvAllowlist(allowlist)
	allowed := make(map[string]struct{}, len(effective))
	for _, name := range effective {
		allowed[strings.ToUpper(name)] = struct{}{}
	}

	parent := make(map[string]string)
	for _, entry := range base {
		key, value, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		if _, ok := allowed[strings.ToUpper(key)]; !ok {
			continue
		}
		parent[strings.ToUpper(key)] = value
	}

	for key, value := range overlay {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		parent[strings.ToUpper(key)] = value
	}

	if pathAppend = strings.TrimSpace(pathAppend); pathAppend != "" {
		if _, ok := allowed["PATH"]; ok {
			if current, ok := parent["PATH"]; ok && current != "" {
				parent["PATH"] = current + string(os.PathListSeparator) + pathAppend
			} else {
				parent["PATH"] = pathAppend
			}
		}
	}

	out := make([]string, 0, len(effective)+len(overlay))
	seen := make(map[string]struct{}, len(effective))
	for _, name := range effective {
		upper := strings.ToUpper(name)
		if _, ok := seen[upper]; ok {
			continue
		}
		value, ok := parent[upper]
		if !ok {
			continue
		}
		seen[upper] = struct{}{}
		out = append(out, name+"="+value)
	}
	for key, value := range overlay {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		upper := strings.ToUpper(key)
		if _, ok := seen[upper]; ok {
			continue
		}
		seen[upper] = struct{}{}
		out = append(out, key+"="+value)
	}
	return out
}
