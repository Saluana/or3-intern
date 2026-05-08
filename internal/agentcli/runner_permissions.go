package agentcli

import (
	"encoding/json"
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

func detectOpenCodePermissionRequest(raw AgentRunEvent) (RunnerPermissionRequest, bool) {
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
