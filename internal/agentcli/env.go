package agentcli

import (
	"os"
	"path/filepath"
	"strings"

	"or3-intern/internal/tools"
)

// BuildAgentCLIEnv builds a sanitized child process environment for external CLI runs.
// It uses the existing tools.BuildChildEnv allowlist pattern while adding NO_COLOR=1
// and TERM=dumb. OR3 internal secrets are stripped automatically by not being in the
// allowlist.
func BuildAgentCLIEnv(base []string, allowlist []string, additionalEnv map[string]string) []string {
	overlay := map[string]string{
		"NO_COLOR": "1",
		"TERM":     "dumb",
	}
	for k, v := range additionalEnv {
		key := strings.TrimSpace(k)
		if key == "" {
			continue
		}
		// Never let callers override the forced values
		lower := strings.ToLower(key)
		if lower == "no_color" || lower == "term" {
			continue
		}
		overlay[key] = v
	}
	pathAppend := ""
	if envAllowlistIncludes(allowlist, "PATH") {
		pathAppend = defaultAgentCLIPathAppend()
	}
	env := tools.BuildChildEnv(base, allowlist, overlay, pathAppend)

	// Ensure OR3 internal secrets are not present even if the allowlist is broad.
	// These are explicitly blocked to prevent secret leakage.
	blocked := map[string]bool{
		"OR3_INTERNAL_TOKEN": true,
		"OR3_PAIRING_SECRET": true,
		"OR3_NODE_SECRET":    true,
		"OR3_SERVICE_SECRET": true,
		"OR3_API_KEY":        true,
		"OPENAI_API_KEY":     true,
	}
	filtered := make([]string, 0, len(env))
	for _, e := range env {
		key, _, ok := strings.Cut(e, "=")
		if !ok {
			continue
		}
		if blocked[key] || blocked[strings.ToUpper(key)] {
			continue
		}
		filtered = append(filtered, e)
	}
	return filtered
}

// SecretStrippedEnv returns the system environment with OR3 internals removed,
// suitable as base input for BuildAgentCLIEnv.
func SecretStrippedEnv() []string {
	return BuildAgentCLIEnv(os.Environ(), nil, nil)
}

func envAllowlistIncludes(allowlist []string, key string) bool {
	for _, name := range tools.EffectiveChildEnvAllowlist(allowlist) {
		if strings.EqualFold(strings.TrimSpace(name), key) {
			return true
		}
	}
	return false
}

func defaultAgentCLIPathAppend() string {
	var dirs []string
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		dirs = append(dirs,
			filepath.Join(home, ".opencode", "bin"),
			filepath.Join(home, ".bun", "bin"),
			filepath.Join(home, ".npm-global", "bin"),
			filepath.Join(home, ".local", "bin"),
			filepath.Join(home, "go", "bin"),
			filepath.Join(home, ".cargo", "bin"),
		)
	}
	dirs = append(dirs,
		"/opt/homebrew/bin",
		"/opt/homebrew/sbin",
		"/usr/local/bin",
	)

	out := make([]string, 0, len(dirs))
	seen := map[string]struct{}{}
	for _, dir := range dirs {
		dir = strings.TrimSpace(dir)
		if dir == "" {
			continue
		}
		if _, ok := seen[dir]; ok {
			continue
		}
		info, err := os.Stat(dir)
		if err != nil || !info.IsDir() {
			continue
		}
		seen[dir] = struct{}{}
		out = append(out, dir)
	}
	return strings.Join(out, string(os.PathListSeparator))
}
