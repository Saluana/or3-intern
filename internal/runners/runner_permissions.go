package runners

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	runnerPermissionKindFilesystem = "filesystem"
	runnerPermissionAccessRead     = "read"
	runnerPermissionAccessWrite    = "write"
)

type RunnerPermissionRequest struct {
	RunnerID   string `json:"runner_id,omitempty"`
	Kind       string `json:"kind,omitempty"`
	Access     string `json:"access,omitempty"`
	TargetPath string `json:"target_path,omitempty"`
}

var (
	openCodeExternalDirectoryPermissionPattern = regexp.MustCompile(`permission requested:\s*external_directory\s*\(([^)]+)\);\s*auto-rejecting`)
	codexSandboxWriteDeniedPattern             = regexp.MustCompile("(?i)can(?:'|’)t write to `([^`]+)`")
)

func NormalizeRunnerPermissionRequest(req RunnerPermissionRequest) (RunnerPermissionRequest, bool) {
	req.RunnerID = strings.TrimSpace(req.RunnerID)
	req.Kind = firstNonEmptyRunnerPermission(req.Kind, runnerPermissionKindFilesystem)
	req.Access = firstNonEmptyRunnerPermission(req.Access, runnerPermissionAccessRead)
	req.TargetPath = strings.TrimSpace(req.TargetPath)
	if req.TargetPath == "" {
		return RunnerPermissionRequest{}, false
	}
	req.TargetPath = filepath.Clean(req.TargetPath)
	if req.TargetPath == "." || req.TargetPath == string(filepath.Separator) {
		return RunnerPermissionRequest{}, false
	}
	return req, true
}

func runnerPermissionToMap(req RunnerPermissionRequest) map[string]any {
	normalized, ok := NormalizeRunnerPermissionRequest(req)
	if !ok {
		return nil
	}
	return map[string]any{
		"runner_id":   normalized.RunnerID,
		"kind":        normalized.Kind,
		"access":      normalized.Access,
		"target_path": normalized.TargetPath,
	}
}

func runnerPermissionFromMeta(meta map[string]any) (RunnerPermissionRequest, bool) {
	if meta == nil {
		return RunnerPermissionRequest{}, false
	}
	raw, ok := meta["runner_permission"]
	if !ok || raw == nil {
		return RunnerPermissionRequest{}, false
	}
	var item RunnerPermissionRequest
	switch value := raw.(type) {
	case RunnerPermissionRequest:
		item = value
	case map[string]any:
		item.RunnerID = stringMapValue(value, "runner_id", "runnerId")
		item.Kind = stringMapValue(value, "kind")
		item.Access = stringMapValue(value, "access")
		item.TargetPath = stringMapValue(value, "target_path", "targetPath")
	default:
		payload, err := json.Marshal(raw)
		if err != nil {
			return RunnerPermissionRequest{}, false
		}
		if err := json.Unmarshal(payload, &item); err != nil {
			return RunnerPermissionRequest{}, false
		}
	}
	return NormalizeRunnerPermissionRequest(item)
}

func detectOpenCodePermissionRequest(raw RunnerRunEvent) (RunnerPermissionRequest, bool) {
	if raw.Type == "structured" && len(raw.Payload) > 0 {
		if req, ok := detectStructuredRunnerPermission(raw.Payload, string(RunnerOpenCode)); ok {
			return req, true
		}
	}
	if raw.Type != "output" || raw.Stream != "stderr" {
		return RunnerPermissionRequest{}, false
	}
	matches := openCodeExternalDirectoryPermissionPattern.FindStringSubmatch(strings.TrimSpace(raw.Chunk))
	if len(matches) != 2 {
		return RunnerPermissionRequest{}, false
	}
	target := strings.TrimSpace(matches[1])
	target = strings.TrimSuffix(target, string(filepath.Separator)+"*")
	target = strings.TrimSuffix(target, "/*")
	return NormalizeRunnerPermissionRequest(RunnerPermissionRequest{
		RunnerID:   string(RunnerOpenCode),
		Kind:       runnerPermissionKindFilesystem,
		Access:     runnerPermissionAccessRead,
		TargetPath: target,
	})
}

func detectCodexStructuredPermissionRequest(raw RunnerRunEvent) (RunnerPermissionRequest, bool) {
	if raw.Type != "structured" || len(raw.Payload) == 0 {
		return RunnerPermissionRequest{}, false
	}
	return detectStructuredRunnerPermission(raw.Payload, string(RunnerCodex))
}

func detectCodexPendingFileChangeTarget(raw RunnerRunEvent) (string, bool) {
	if raw.Type != "structured" || len(raw.Payload) == 0 {
		return "", false
	}
	var obj map[string]any
	if err := json.Unmarshal(raw.Payload, &obj); err != nil {
		return "", false
	}
	params := mapAnyValue(obj, "params")
	item := mapAnyValue(params, "item")
	if item == nil {
		item = mapAnyValue(obj, "item")
	}
	if !strings.EqualFold(asString(item["type"]), "fileChange") {
		return "", false
	}
	changes, _ := item["changes"].([]any)
	for _, rawChange := range changes {
		change, _ := rawChange.(map[string]any)
		target := strings.TrimSpace(firstNonEmptyRunnerPermission(
			asString(change["path"]),
			asString(change["target_path"]),
			asString(change["targetPath"]),
		))
		if target == "" {
			continue
		}
		target = filepath.Clean(target)
		if target != "." && target != string(filepath.Separator) {
			return target, true
		}
	}
	return "", false
}

func detectCodexPermissionRequest(text string) (RunnerPermissionRequest, bool) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" || !strings.Contains(strings.ToLower(trimmed), "approvals are disabled") {
		return RunnerPermissionRequest{}, false
	}
	matches := codexSandboxWriteDeniedPattern.FindStringSubmatch(trimmed)
	if len(matches) != 2 {
		return RunnerPermissionRequest{}, false
	}
	target := normalizeWriteTarget(matches[1])
	return NormalizeRunnerPermissionRequest(RunnerPermissionRequest{
		RunnerID:   string(RunnerCodex),
		Kind:       runnerPermissionKindFilesystem,
		Access:     runnerPermissionAccessWrite,
		TargetPath: target,
	})
}

func detectStructuredRunnerPermission(payload json.RawMessage, runnerID string) (RunnerPermissionRequest, bool) {
	var obj map[string]any
	if err := json.Unmarshal(payload, &obj); err != nil {
		return RunnerPermissionRequest{}, false
	}
	typeValue := strings.ToLower(firstNonEmptyRunnerPermission(stringMapValue(obj, "type"), stringMapValue(obj, "method")))
	if !strings.Contains(typeValue, "permission") && !strings.Contains(typeValue, "approval") && !strings.Contains(typeValue, "requestapproval") {
		return RunnerPermissionRequest{}, false
	}
	params := mapAnyValue(obj, "params")
	permission := mapAnyValue(obj, "permission")
	request := mapAnyValue(obj, "request")
	var raw any
	if strings.Contains(typeValue, "command") {
		// Command approvals often report the runner cwd as well as the actual
		// out-of-workspace target in the human-readable reason. Prefer the
		// reason so policy is evaluated against what the command will touch.
		raw = firstNonNilRunnerPermission(
			params["reason"], params["message"], params["command"],
			request["reason"], request["message"], request["command"],
			permission["reason"], permission["message"], permission["command"],
		)
	}
	if raw == nil {
		raw = firstNonNilRunnerPermission(
			params["target_path"], params["targetPath"], params["path"], params["file"],
			permission["target_path"], permission["targetPath"], permission["path"], permission["file"],
			request["target_path"], request["targetPath"], request["path"], request["file"],
			obj["target_path"], obj["targetPath"], obj["path"], obj["file"],
			params["reason"], params["command"], params["cwd"],
			permission["command"], permission["reason"], permission["message"],
			request["command"], request["reason"], request["message"],
			obj["command"], obj["message"], obj["cwd"], obj["raw"],
		)
	}
	target := pathFromPermissionValue(raw)
	access := runnerPermissionAccessRead
	permissionKind := strings.ToLower(firstNonEmptyRunnerPermission(
		stringMapValue(params, "permission", "type", "tool", "name"),
		stringMapValue(permission, "permission", "type", "tool", "name"),
		stringMapValue(request, "permission", "type", "tool", "name"),
	))
	if strings.Contains(typeValue, "change") ||
		strings.Contains(typeValue, "command") ||
		strings.Contains(typeValue, "write") ||
		strings.Contains(permissionKind, "edit") ||
		strings.Contains(permissionKind, "write") ||
		strings.Contains(permissionKind, "patch") ||
		strings.Contains(permissionKind, "bash") ||
		strings.Contains(strings.ToLower(fmt.Sprint(raw)), "write") {
		access = runnerPermissionAccessWrite
	}
	return NormalizeRunnerPermissionRequest(RunnerPermissionRequest{RunnerID: runnerID, Kind: runnerPermissionKindFilesystem, Access: access, TargetPath: target})
}

func mapAnyValue(record map[string]any, key string) map[string]any {
	if record == nil {
		return nil
	}
	value, _ := record[key].(map[string]any)
	return value
}

func firstNonNilRunnerPermission(values ...any) any {
	for _, value := range values {
		if value != nil && strings.TrimSpace(fmt.Sprint(value)) != "" {
			return value
		}
	}
	return nil
}

func pathFromPermissionValue(value any) string {
	switch v := value.(type) {
	case string:
		fields := strings.Fields(v)
		var found string
		for _, field := range fields {
			trimmed := strings.Trim(field, "`'\".,:;?!()[]{}")
			if strings.HasPrefix(trimmed, string(filepath.Separator)) || strings.HasPrefix(trimmed, "~/") {
				found = filepath.Clean(trimmed)
			}
		}
		if found != "" {
			return found
		}
		return filepath.Clean(strings.Trim(v, "`'\""))
	case map[string]any:
		return stringMapValue(v, "target_path", "targetPath", "path", "file", "cwd")
	case []any:
		for _, item := range v {
			if path := pathFromPermissionValue(item); path != "" {
				return path
			}
		}
	}
	return ""
}

func normalizeWriteTarget(raw string) string {
	path := filepath.Clean(strings.TrimSpace(raw))
	if path == "" || path == "." || path == string(filepath.Separator) {
		return ""
	}
	if info, err := os.Stat(path); err == nil && info.IsDir() {
		return path
	}
	return filepath.Dir(path)
}

func stringMapValue(record map[string]any, keys ...string) string {
	for _, key := range keys {
		value, _ := record[key].(string)
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func firstNonEmptyRunnerPermission(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
