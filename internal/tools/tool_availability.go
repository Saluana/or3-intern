package tools

import (
	"fmt"
	"strings"
)

const ErrToolNotAvailableThisTurn = "tool not available in this turn"

func IsToolNotAvailableThisTurn(errText string) bool {
	return strings.Contains(strings.ToLower(strings.TrimSpace(errText)), ErrToolNotAvailableThisTurn)
}

func AvailableIncludesWriteTool(names []string) bool {
	for _, name := range names {
		if IsWriteToolName(name) {
			return true
		}
	}
	return false
}

func AttemptedIncludesWriteTool(names []string) bool {
	return AvailableIncludesWriteTool(names)
}

// ToolNotAvailableThisTurnAdvice returns guidance when a tool was blocked by the
// current turn policy (for example Ask/read-only mode). It must not suggest
// argument fixes or alternate write tools that are also unavailable.
func ToolNotAvailableThisTurnAdvice(toolName string) []string {
	name := strings.TrimSpace(toolName)
	if name == "" {
		name = "that tool"
	}
	if IsWriteToolName(name) {
		return []string{
			fmt.Sprintf("This turn is in read-only (Ask) mode: %s is not available, and retrying the same call will not succeed.", name),
			"Do not call write_file, edit_file, or delete_file again this turn.",
			"Tell the user you cannot create or modify files in Ask mode. Suggest switching to Work mode if they want file changes, or paste the content in chat for them to save manually.",
		}
	}
	if strings.EqualFold(name, ToolNameExec) {
		return []string{
			"This turn does not expose exec. Retrying exec with different arguments will not succeed.",
			"Use only the tools currently advertised in the system message, or explain to the user that shell commands require a mode that allows exec.",
		}
	}
	return []string{
		fmt.Sprintf("%s is not available in this turn's tool policy. Do not retry the same call.", name),
		"Use only the tools currently advertised in the system message, or answer without tools.",
	}
}
