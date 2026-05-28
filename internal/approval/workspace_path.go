package approval

import (
	"path/filepath"
	"strings"
)

func pathWithinRoot(root, target string) bool {
	root = strings.TrimSpace(root)
	target = strings.TrimSpace(target)
	if root == "" || target == "" {
		return false
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	targetAbs, err := filepath.Abs(target)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(rootAbs, targetAbs)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func workspaceRelation(workspace, workingDir string) string {
	workingDir = strings.TrimSpace(workingDir)
	if workingDir == "" {
		return "unknown"
	}
	workspace = strings.TrimSpace(workspace)
	if workspace == "" {
		return "unknown"
	}
	if pathWithinRoot(workspace, workingDir) {
		return "inside_workspace"
	}
	return "outside_workspace"
}
