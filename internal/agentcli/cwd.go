package agentcli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"or3-intern/internal/tools"
)

// resolveAgentCLICwd validates and canonicalizes the requested working directory
// against the manager's RestrictDir. It follows these rules:
//
//   - Empty requested → defaults to RestrictDir (or os.Getwd if unrestricted)
//   - Relative requested → resolved below RestrictDir
//   - Absolute requested → must be inside RestrictDir, rejected otherwise
//   - Empty RestrictDir → allow any cwd (no restriction)
func resolveAgentCLICwd(requested, restrictDir string) (string, error) {
	requested = strings.TrimSpace(requested)

	if requested == "" {
		if strings.TrimSpace(restrictDir) != "" {
			return restrictDir, nil
		}
		return os.Getwd()
	}

	if filepath.IsAbs(requested) {
		if strings.TrimSpace(restrictDir) == "" {
			return requested, nil
		}
		return validateCwdWithinRoot(requested, restrictDir)
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
