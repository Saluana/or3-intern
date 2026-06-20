package runners

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"or3-intern/internal/tools"
)

// resolveRunnerCwd resolves and canonicalizes the requested working directory.
// RestrictDir constrains the default (empty) and relative CWD; explicitly-set
// absolute paths are always trusted since the request is authenticated.
func resolveRunnerCwd(requested, restrictDir string) (string, error) {
	requested = strings.TrimSpace(requested)

	if requested == "" {
		if strings.TrimSpace(restrictDir) != "" {
			return restrictDir, nil
		}
		return os.Getwd()
	}

	if filepath.IsAbs(requested) {
		return requested, nil
	}

	base := strings.TrimSpace(restrictDir)
	if base == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return "", err
		}
		base = cwd
	}
	resolved := filepath.Join(base, requested)
	if strings.TrimSpace(restrictDir) == "" {
		return resolved, nil
	}
	return validateCwdWithinRoot(resolved, restrictDir)
}

func validateCwdWithinRoot(cwd, root string) (string, error) {
	abs, err := filepath.Abs(cwd)
	if err != nil {
		return "", err
	}
	abs, err = tools.CanonicalizePath(abs)
	if err != nil {
		return "", fmt.Errorf("cwd validation failed: %w", err)
	}
	root, err = tools.CanonicalizeRoot(root)
	if err != nil {
		return "", fmt.Errorf("cwd validation failed: invalid restrictDir: %w", err)
	}
	rel, err := filepath.Rel(root, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("cwd outside allowed directory: %s (allowed root: %s)", abs, root)
	}
	return abs, nil
}
