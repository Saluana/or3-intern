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
	toolName = strings.TrimSpace(toolName)
	lowerErr := strings.ToLower(strings.TrimSpace(errText))
	common := []string{"Check the tool arguments and use the smallest, most specific request that satisfies the task."}
	switch toolName {
	case "read_file":
		advice := []string{}
		if strings.Contains(lowerErr, "path outside allowed root") {
			advice = append(advice, "Choose a file path inside the current workspace or allowed read root. Use list_dir first if you need to discover the correct path.")
		}
		if strings.Contains(lowerErr, "missing pattern") {
			advice = append(advice, "For mode=grep, provide a specific pattern such as a symbol name, config key, or exact error string.")
		}
		if strings.Contains(lowerErr, "unsupported read_file mode") {
			advice = append(advice, "Use one of the supported modes: preview, full, range, grep, or outline.")
		}
		if strings.Contains(lowerErr, "no such file") || strings.Contains(lowerErr, "not exist") {
			advice = append(advice, "Use list_dir on the parent directory to confirm the file path, then retry read_file with the exact path.")
		}
		return appendUniqueAdvice(common, advice...)
	case "search_file":
		advice := []string{"Use read_file mode=outline or mode=preview first if you do not yet know what pattern to search for."}
		if strings.Contains(lowerErr, "path outside allowed root") {
			advice = append(advice, "Choose a file path inside the current workspace or allowed read root.")
		}
		if strings.Contains(lowerErr, "missing pattern") {
			advice = append(advice, "Provide a non-empty pattern such as a symbol, config key, or exact error message.")
		}
		return appendUniqueAdvice(common, advice...)
	case "write_file":
		advice := []string{"Use edit_file instead when you only need a localized change in an existing file."}
		if strings.Contains(lowerErr, "path outside allowed root") {
			advice = append(advice, "Write the file inside the configured writable workspace root.")
		}
		if strings.Contains(lowerErr, "no such file") || strings.Contains(lowerErr, "not exist") {
			advice = append(advice, "If you are creating a new file in a new directory, retry with mkdirs=true or choose an existing parent directory.")
		}
		return appendUniqueAdvice(common, advice...)
	case "edit_file":
		advice := []string{"Use write_file only when you intend to replace the full file content."}
		if strings.Contains(lowerErr, "path outside allowed root") {
			advice = append(advice, "Edit a file inside the configured writable workspace root.")
		}
		if strings.Contains(lowerErr, "no such file") || strings.Contains(lowerErr, "not exist") {
			advice = append(advice, "Confirm the file already exists with list_dir or create it first with write_file.")
		}
		return appendUniqueAdvice(common, advice...)
	case "list_dir":
		advice := []string{}
		if strings.Contains(lowerErr, "path is not a directory") {
			advice = append(advice, "Use read_file for files. list_dir only works on directories.")
		}
		if strings.Contains(lowerErr, "path outside allowed root") {
			advice = append(advice, "Choose a directory inside the current workspace or allowed read root.")
		}
		return appendUniqueAdvice(common, advice...)
	case "read_artifact":
		advice := []string{"Use the artifact_id returned by an earlier tool result exactly as given."}
		if strings.Contains(lowerErr, "missing artifact_id") {
			advice = append(advice, "Retry with the artifact_id from the earlier read_file, web_fetch, or web_fetch_markdown result.")
		}
		if strings.Contains(lowerErr, "not found") {
			advice = append(advice, "Retry with the exact artifact_id from the prior turn, or rerun the producing tool if the artifact no longer exists.")
		}
		return appendUniqueAdvice(common, advice...)
	case "read_skill":
		advice := []string{"Use the exact skill name from the inventory. If you only need a quick overview, prefer mode=outline or mode=preview."}
		if strings.Contains(lowerErr, "skill not found") {
			advice = append(advice, "Retry with an exact installed skill name.")
		}
		return appendUniqueAdvice(common, advice...)
	case "web_fetch", "web_fetch_markdown":
		advice := []string{"Use a full http:// or https:// URL. If you only know a topic, call web_search first to discover a likely URL."}
		if strings.Contains(lowerErr, "invalid url") {
			advice = append(advice, "Retry with a complete URL instead of a search query or hostname fragment.")
		}
		if strings.Contains(lowerErr, "unsupported content type") {
			advice = append(advice, "Use web_fetch with raw=true or plain web_fetch when the target is not HTML that can be converted to Markdown.")
		}
		if strings.Contains(lowerErr, "playwright") || strings.Contains(lowerErr, "browser") || strings.Contains(lowerErr, "render") {
			advice = append(advice, "Retry without render=true unless JavaScript rendering is truly required.")
		}
		return appendUniqueAdvice(common, advice...)
	case "web_search":
		advice := []string{"Keep the query specific: include the project, product, person, or exact error text you need."}
		if strings.Contains(lowerErr, "api key not configured") {
			advice = append(advice, "If you already know the target URL, use web_fetch directly instead of web_search.")
		}
		return appendUniqueAdvice(common, advice...)
	case "exec":
		advice := []string{"Prefer program + args over command strings, and keep the request to one direct program invocation."}
		if strings.Contains(lowerErr, "approval required") {
			advice = append(advice, "Wait for approval, then retry the exact same tool call so the approval token matches the same subject.")
			advice = append(advice, "For exec, keep the same program and args after approval; changing argv creates a different approval subject.")
		}
		if strings.Contains(lowerErr, "shell command execution disabled") || strings.Contains(lowerErr, "shell syntax is not allowed") {
			advice = append(advice, "Split shell pipelines into separate direct exec calls, or use a single program invocation with explicit args.")
		}
		if strings.Contains(lowerErr, "program not allowed") {
			advice = append(advice, "Use an allowed program, or switch to a read-only tool like read_file, search_file, or list_dir if that can answer the question.")
		}
		if strings.Contains(lowerErr, "executable file not found") || strings.Contains(lowerErr, "not found") {
			advice = append(advice, "Retry with an absolute executable path or with a different installed program name.")
		}
		if strings.Contains(lowerErr, "cwd outside allowed directory") {
			advice = append(advice, "Omit cwd to use the workspace default, or provide a cwd inside the allowed workspace root.")
		}
		return appendUniqueAdvice(common, advice...)
	case "run_skill", "run_skill_script":
		advice := []string{"Use read_skill first to inspect the skill instructions and available entrypoints before executing a script."}
		if strings.Contains(lowerErr, "requires approval") || strings.Contains(lowerErr, "approval required") {
			advice = append(advice, "Wait for approval, then retry the same skill name, entrypoint/path, and timeout or resume the returned plan_id so the approval token matches the frozen plan.")
		}
		if strings.Contains(lowerErr, "skill not found") {
			advice = append(advice, "Retry with the exact installed skill name.")
		}
		if strings.Contains(lowerErr, "missing path or entrypoint") {
			advice = append(advice, "Provide either an approved entrypoint name or a bundle-relative script path.")
		}
		return appendUniqueAdvice(common, advice...)
	case "spawn_subagent":
		advice := []string{"Use spawn_subagent only for work that can continue independently in the background."}
		if strings.Contains(lowerErr, "disabled") {
			advice = append(advice, "Handle the work in the current turn instead of delegating it to a background subagent.")
		}
		if strings.Contains(lowerErr, "empty task") {
			advice = append(advice, "Provide a complete, self-contained task description with the expected output.")
		}
		return appendUniqueAdvice(common, advice...)
	case "send_message":
		advice := []string{"Only use send_message when external delivery is part of the task; otherwise answer in the current conversation."}
		if strings.Contains(lowerErr, "message requires text or media") {
			advice = append(advice, "Provide message text, media paths, or both.")
		}
		if strings.Contains(lowerErr, "reply_in_thread") {
			advice = append(advice, "Keep the current channel/recipient when reply_in_thread=true, or disable reply_in_thread when changing targets.")
		}
		if strings.Contains(lowerErr, "deliver not configured") {
			advice = append(advice, "Respond in the current conversation or use a channel that is configured for delivery.")
		}
		return appendUniqueAdvice(common, advice...)
	case "cron":
		advice := []string{"Use cron for future or recurring work, not for one-turn actions that should happen now."}
		if strings.Contains(lowerErr, "unknown action") {
			advice = append(advice, "Use one of: add, list, remove, run, or status.")
		}
		if strings.Contains(lowerErr, "missing job") {
			advice = append(advice, "For action=add, include a complete job object with schedule and payload.")
		}
		return appendUniqueAdvice(common, advice...)
	case "memory_set_pinned", "memory_add_note", "memory_search", "memory_recent", "memory_get_pinned":
		advice := []string{"If memory is unavailable, continue the task without depending on memory persistence or retrieval."}
		if strings.Contains(lowerErr, "empty query") {
			advice = append(advice, "Provide a concrete memory query such as a project name, person, or decision.")
		}
		if strings.Contains(lowerErr, "empty text") || strings.Contains(lowerErr, "missing key/content") {
			advice = append(advice, "Provide the durable fact, note text, or pinned key/content explicitly.")
		}
		return appendUniqueAdvice(common, advice...)
	default:
		return common
	}
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
