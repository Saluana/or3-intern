package agentcli

import (
	"encoding/json"
	"path/filepath"
	"runtime"
	"strings"

	"or3-intern/internal/config"
)

const openCodeConfigContentEnvVar = "OPENCODE_CONFIG_CONTENT"

// OpenCodeExternalDirectoriesFromConfig returns OR3-owned directories that are
// safe to pre-authorize for OpenCode external_directory access.
func OpenCodeExternalDirectoriesFromConfig(cfg config.Config) []string {
	dirs := make([]string, 0, 4+len(cfg.Skills.Load.ExtraDirs))
	seen := make(map[string]struct{})
	add := func(dir string) {
		dir = normalizeOpenCodePermissionDir(dir)
		if dir == "" {
			return
		}
		key := dir
		if runtime.GOOS == "windows" {
			key = strings.ToLower(key)
		}
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		dirs = append(dirs, dir)
	}

	add(cfg.WorkspaceDir)
	add(cfg.AllowedDir)
	add(cfg.Skills.ManagedDir)
	if !cfg.Skills.Load.DisableGlobalDir {
		add(cfg.Skills.Load.GlobalDir)
	}
	for _, dir := range cfg.Skills.Load.ExtraDirs {
		add(dir)
	}
	return dirs
}

func buildOpenCodeConfigEnv(externalDirs []string) map[string]string {
	content, ok := openCodeConfigContent(externalDirs)
	if !ok {
		return nil
	}
	return map[string]string{openCodeConfigContentEnvVar: content}
}

func openCodeConfigContent(externalDirs []string) (string, bool) {
	rules := make(map[string]string)
	for _, dir := range externalDirs {
		dir = normalizeOpenCodePermissionDir(dir)
		if dir == "" {
			continue
		}
		rules[filepath.Join(dir, "*")] = "allow"
	}
	if len(rules) == 0 {
		return "", false
	}
	payload := map[string]any{
		"permission": map[string]any{
			"external_directory": rules,
		},
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", false
	}
	return string(raw), true
}

func normalizeOpenCodePermissionDir(dir string) string {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return ""
	}
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	return filepath.Clean(dir)
}

func mergeEnvOverlay(base []string, overlay map[string]string) []string {
	if len(overlay) == 0 {
		return base
	}
	values := make(map[string]string, len(base)+len(overlay))
	order := make([]string, 0, len(base)+len(overlay))
	for _, raw := range base {
		key, value, ok := strings.Cut(raw, "=")
		if !ok {
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
	out := make([]string, 0, len(order))
	for _, key := range order {
		out = append(out, key+"="+values[key])
	}
	return out
}
