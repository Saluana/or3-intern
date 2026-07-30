package tools

import "strings"

const (
	ToolNameExec      = "exec"
	toolNameWriteFile = "write_file"
	toolNameEditFile  = "edit_file"
)

// IsWriteToolName reports whether name is a legacy write/edit file tool.
func IsWriteToolName(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case toolNameWriteFile, toolNameEditFile:
		return true
	default:
		return false
	}
}

// IsToolNotAvailableThisTurn reports common legacy tool-unavailable error text.
func IsToolNotAvailableThisTurn(detail string) bool {
	detail = strings.ToLower(strings.TrimSpace(detail))
	if detail == "" {
		return false
	}
	return strings.Contains(detail, "not available this turn") ||
		strings.Contains(detail, "not available in this turn") ||
		strings.Contains(detail, "tool is not available") ||
		strings.Contains(detail, "not registered")
}
