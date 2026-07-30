// Package actionresult provides bounded JSON envelopes for admin and service actions.
package actionresult

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

// Result is the bounded result envelope shared by doctor and service actions.
type Result struct {
	Kind       string         `json:"kind"`
	OK         bool           `json:"ok"`
	Status     string         `json:"status,omitempty"`
	Summary    string         `json:"summary,omitempty"`
	Preview    string         `json:"preview,omitempty"`
	ArtifactID string         `json:"artifact_id,omitempty"`
	PlanID     string         `json:"plan_id,omitempty"`
	RequestID  int64          `json:"request_id,omitempty"`
	Advice     []string       `json:"advice,omitempty"`
	Stats      map[string]any `json:"stats,omitempty"`
}

// ApprovalRequiredError is returned when an action needs operator approval.
type ApprovalRequiredError struct {
	ToolName  string
	RequestID int64
}

func (e *ApprovalRequiredError) Error() string {
	toolName := strings.TrimSpace(e.ToolName)
	if toolName == "" {
		toolName = "action"
	}
	if e.RequestID > 0 {
		return fmt.Sprintf("approval required for %s (request %d)", toolName, e.RequestID)
	}
	return fmt.Sprintf("approval required for %s", toolName)
}

func Encode(result Result) string {
	if strings.TrimSpace(result.Kind) == "" {
		result.Kind = "tool_result"
	}
	b, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return `{"kind":"tool_result","ok":false,"summary":"failed to encode action result"}`
	}
	return string(b)
}

func Decode(out string) (Result, bool) {
	var result Result
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &result); err != nil {
		return Result{}, false
	}
	if strings.TrimSpace(result.Kind) == "" {
		return Result{}, false
	}
	return result, true
}

func EncodeFailure(actionName string, params map[string]any, out string, err error) string {
	if err == nil {
		return out
	}
	actionName = strings.TrimSpace(actionName)
	errText := strings.TrimSpace(err.Error())
	result, ok := Decode(out)
	if !ok {
		result = Result{
			Kind:    normalizedKind(actionName),
			Preview: strings.TrimSpace(out),
		}
	}
	if strings.TrimSpace(result.Kind) == "" {
		result.Kind = normalizedKind(actionName)
	}
	result.OK = false
	result.Summary = failureSummary(actionName, errText, result, out)
	var approvalErr *ApprovalRequiredError
	if errors.As(err, &approvalErr) {
		result.Status = "approval_required"
		result.RequestID = approvalErr.RequestID
	}
	if result.Preview == "" && strings.TrimSpace(out) != "" {
		result.Preview = strings.TrimSpace(out)
	}
	return Encode(result)
}

func normalizedKind(actionName string) string {
	actionName = strings.TrimSpace(actionName)
	if actionName == "" {
		return "action_result"
	}
	return actionName + "_result"
}

func failureSummary(actionName, errText string, result Result, out string) string {
	if strings.TrimSpace(result.Summary) != "" {
		return strings.TrimSpace(result.Summary)
	}
	if strings.TrimSpace(result.Preview) != "" {
		return strings.TrimSpace(result.Preview)
	}
	if strings.TrimSpace(out) != "" {
		return strings.TrimSpace(out)
	}
	if actionName != "" && errText != "" {
		return actionName + " failed: " + errText
	}
	return errText
}

func PreviewPath(path string, max int) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if max <= 0 {
		return path
	}
	base := filepath.Base(path)
	if len(base) <= max {
		return base
	}
	if max <= 3 {
		return base[:max]
	}
	return base[:max-3] + "..."
}
