package tools

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

// ToolResult is the bounded result envelope shared by high-volume tools.
type ToolResult struct {
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

func EncodeToolResult(result ToolResult) string {
	if strings.TrimSpace(result.Kind) == "" {
		result.Kind = "tool_result"
	}
	b, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return `{"kind":"tool_result","ok":false,"summary":"failed to encode tool result"}`
	}
	return string(b)
}

func DecodeToolResult(out string) (ToolResult, bool) {
	var result ToolResult
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &result); err != nil {
		return ToolResult{}, false
	}
	if strings.TrimSpace(result.Kind) == "" {
		return ToolResult{}, false
	}
	return result, true
}

func EncodeToolFailure(toolName string, params map[string]any, out string, err error) string {
	if err == nil {
		return out
	}
	toolName = strings.TrimSpace(toolName)
	errText := strings.TrimSpace(err.Error())
	result, ok := DecodeToolResult(out)
	if !ok {
		result = ToolResult{
			Kind:    normalizedToolResultKind(toolName),
			Preview: strings.TrimSpace(out),
		}
	}
	if strings.TrimSpace(result.Kind) == "" {
		result.Kind = normalizedToolResultKind(toolName)
	}
	result.OK = false
	result.Summary = toolFailureSummary(toolName, errText, result, out)
	result.Advice = appendUniqueAdvice(result.Advice, toolFailureAdvice(toolName, params, errText)...)
	if result.Stats == nil {
		result.Stats = map[string]any{}
	}
	result.Stats["tool"] = toolName
	result.Stats["error"] = errText
	var approvalErr *ApprovalRequiredError
	if errors.As(err, &approvalErr) {
		result.Status = "approval_required"
		result.RequestID = approvalErr.RequestID
	}
	if result.Preview == "" && strings.TrimSpace(out) != "" {
		result.Preview = strings.TrimSpace(out)
	}
	return EncodeToolResult(result)
}

func PreviewString(s string, maxBytes int) (string, bool) {
	s = strings.TrimSpace(s)
	runes := []rune(s)
	if maxBytes <= 0 || len(runes) <= maxBytes {
		return s, false
	}
	return string(runes[:maxBytes]) + "\n...[preview truncated]", true
}

func normalizedToolResultKind(toolName string) string {
	name := strings.TrimSpace(toolName)
	if name == "" {
		return "tool_result"
	}
	return strings.ReplaceAll(name, " ", "_")
}

func firstNonEmptyToolName(toolName string) string {
	if strings.TrimSpace(toolName) == "" {
		return "tool"
	}
	return toolName
}

func toolFailureSummary(toolName string, errText string, result ToolResult, rawOut string) string {
	base := trimRedundantToolFailurePrefix(toolName, errText)
	if base == "" {
		base = strings.TrimSpace(errText)
	}
	if detail := toolFailureDetail(toolName, result, rawOut); detail != "" && !strings.Contains(strings.ToLower(base), strings.ToLower(detail)) {
		if base == "" {
			base = detail
		} else {
			base = base + ": " + detail
		}
	}
	if base == "" {
		base = "unknown error"
	}
	return fmt.Sprintf("%s failed: %s", firstNonEmptyToolName(toolName), base)
}

func trimRedundantToolFailurePrefix(toolName string, errText string) string {
	toolName = strings.ToLower(strings.TrimSpace(toolName))
	errText = strings.TrimSpace(errText)
	if toolName == "" || errText == "" {
		return errText
	}
	prefix := toolName + " failed:"
	if strings.HasPrefix(strings.ToLower(errText), prefix) {
		return strings.TrimSpace(errText[len(prefix):])
	}
	return errText
}

func toolFailureDetail(toolName string, result ToolResult, rawOut string) string {
	switch strings.TrimSpace(toolName) {
	case "exec":
		preview := strings.TrimSpace(result.Preview)
		if preview == "" {
			preview = strings.TrimSpace(rawOut)
		}
		return extractExecFailureDetail(preview)
	default:
		return ""
	}
}

func extractExecFailureDetail(preview string) string {
	preview = strings.TrimSpace(strings.ReplaceAll(preview, "\r\n", "\n"))
	if preview == "" {
		return ""
	}
	stdout, stderr := splitExecPreview(preview)
	if line := firstMeaningfulFailureLine(stderr); line != "" {
		return line
	}
	if line := firstMeaningfulFailureLine(stdout); line != "" {
		return line
	}
	return firstMeaningfulFailureLine(preview)
}

func splitExecPreview(preview string) (string, string) {
	const stdoutPrefix = "stdout:\n"
	const stderrMarker = "\n\nstderr:\n"
	if !strings.HasPrefix(preview, stdoutPrefix) {
		return preview, ""
	}
	body := strings.TrimPrefix(preview, stdoutPrefix)
	parts := strings.SplitN(body, stderrMarker, 2)
	stdout := strings.TrimSpace(parts[0])
	if len(parts) == 1 {
		return stdout, ""
	}
	return stdout, strings.TrimSpace(parts[1])
}

func firstMeaningfulFailureLine(block string) string {
	lines := strings.Split(strings.TrimSpace(block), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "error[") {
			return line
		}
	}
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || line == "{" || line == "}" || line == "[" || line == "]" {
			continue
		}
		if strings.HasPrefix(line, "stdout:") || strings.HasPrefix(line, "stderr:") {
			continue
		}
		lower := strings.ToLower(line)
		if strings.Contains(lower, "failed") || strings.Contains(lower, "error") || strings.Contains(lower, "invalid") || strings.Contains(lower, "denied") || strings.Contains(lower, "not found") || strings.Contains(lower, "permission") || strings.Contains(lower, "timeout") || strings.Contains(lower, "auth") {
			return strings.Trim(line, `",`)
		}
		if strings.Contains(line, `"message":`) {
			line = strings.TrimSpace(strings.TrimPrefix(line, `"message":`))
			line = strings.Trim(line, `",`)
			if line != "" {
				return line
			}
		}
	}
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || line == "{" || line == "}" || line == "[" || line == "]" {
			continue
		}
		if strings.HasPrefix(line, "stdout:") || strings.HasPrefix(line, "stderr:") {
			continue
		}
		return strings.Trim(line, `",`)
	}
	return ""
}

func appendUniqueAdvice(existing []string, additional ...string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(existing)+len(additional))
	for _, value := range append(append([]string{}, existing...), additional...) {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func toolFailureAdvice(toolName string, params map[string]any, errText string) []string {
	if IsToolNotAvailableThisTurn(errText) {
		return ToolNotAvailableThisTurnAdvice(toolName)
	}
	common := []string{"Check the tool arguments and use the smallest, most specific request that satisfies the task."}
	if provider := adviceProviderForToolName(toolName); provider != nil {
		return appendUniqueAdvice(common, provider.FailureAdvice(params, errText)...)
	}
	return common
}

func TruncationAdvice(kind string, target string) []string {
	target = strings.TrimSpace(target)
	switch strings.TrimSpace(kind) {
	case "read_file_full":
		return []string{
			fmt.Sprintf("%s is too large to read all at once in the current tool budget.", filepath.Base(target)),
			"Use read_file mode=outline or mode=grep to locate the relevant section, then mode=range for the exact lines you need.",
			"Only increase maxBytes when you truly need more direct content in one call.",
		}
	case "read_file_range":
		return []string{
			"Narrow the requested line range or increase maxBytes slightly if you need a larger slice.",
		}
	case "search_file":
		return []string{
			"Use a more specific pattern or increase maxBytes if you need more matching lines.",
		}
	case "read_file_outline":
		return []string{
			"Use the outline to choose a smaller line range, then call read_file mode=range for the exact section.",
		}
	case "list_dir":
		return []string{
			"Retry with a larger max only if you need more entries, or list a narrower subdirectory.",
		}
	case "read_skill_full":
		return []string{
			"Use read_skill mode=outline or mode=preview first, then raise maxBytes only if the full skill text is still needed.",
		}
	default:
		return nil
	}
}
