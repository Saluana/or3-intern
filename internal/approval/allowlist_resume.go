package approval

import (
	"encoding/json"
	"strings"
)

// ApprovalOperationTool returns the guarded tool name encoded in an approval subject.
func ApprovalOperationTool(subjectJSON string) string {
	subjectJSON = strings.TrimSpace(subjectJSON)
	if subjectJSON == "" {
		return ""
	}
	var payload struct {
		ToolName string `json:"tool_name"`
	}
	if err := json.Unmarshal([]byte(subjectJSON), &payload); err != nil {
		return ""
	}
	return strings.TrimSpace(payload.ToolName)
}
