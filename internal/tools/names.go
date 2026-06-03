package tools

import "strings"

const (
	ToolNameExec        = "exec"
	ToolNameReadFile    = "read_file"
	ToolNameSearchFile  = "search_file"
	ToolNameWriteFile   = "write_file"
	ToolNameEditFile    = "edit_file"
	ToolNameListDir     = "list_dir"
	ToolNameWebFetch    = "web_fetch"
	ToolNameWebSearch   = "web_search"
	ToolNameSpawnSubagent = "spawn_subagent"
)

// IsWriteToolName reports whether name is a legacy write/edit file tool.
func IsWriteToolName(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case ToolNameWriteFile, ToolNameEditFile:
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
