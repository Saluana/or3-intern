package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// CanonicalizePath resolves symlinks and returns a clean absolute path.
func CanonicalizePath(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", fmt.Errorf("empty path")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err == nil {
		return filepath.Clean(resolved), nil
	}
	if !os.IsNotExist(err) {
		return "", err
	}
	parent := filepath.Dir(abs)
	base := filepath.Base(abs)
	resolvedParent, err := CanonicalizePath(parent)
	if err != nil {
		return "", err
	}
	return filepath.Join(resolvedParent, base), nil
}

// CanonicalizeRoot canonicalizes a directory root used for path restriction checks.
func CanonicalizeRoot(root string) (string, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return "", fmt.Errorf("empty root")
	}
	return CanonicalizePath(root)
}
