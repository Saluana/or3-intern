package approval

import (
	"path/filepath"
	"strings"
)

const runnerAutopilotPolicyHash = "runner-autopilot-v1"

func reviewRunnerPermissionAutopilot(req RunnerPermissionEvaluation) ModeratorDecision {
	if !req.Autopilot {
		return ModeratorDecision{}
	}
	kind := strings.ToLower(strings.TrimSpace(req.PermissionKind))
	if kind == "" {
		kind = "filesystem"
	}
	access := strings.ToLower(strings.TrimSpace(req.Access))
	if access == "" {
		access = "read"
	}
	target := filepath.Clean(strings.TrimSpace(req.TargetPath))
	mode := strings.ToLower(strings.TrimSpace(req.RunnerMode))
	if mode == "" {
		mode = "safe_edit"
	}
	decision := ModeratorDecision{
		Reviewed:   true,
		Status:     "reviewed",
		Risk:       "high",
		Action:     "escalate",
		Reason:     "Runner permission needs human review.",
		Model:      "or3-policy",
		PolicyHash: runnerAutopilotPolicyHash,
	}
	if kind != "filesystem" {
		decision.Reason = "Non-filesystem runner permissions need human review."
		return decision
	}
	if target == "" || target == "." || target == string(filepath.Separator) {
		decision.Reason = "Broad filesystem permission needs human review."
		return decision
	}
	if runnerAutopilotSensitiveTarget(target) {
		decision.Risk = "extreme"
		decision.Reason = "Sensitive path or configuration change needs human review."
		return decision
	}
	if access == "read" {
		decision.Risk = "low"
		decision.Action = "approve_once"
		decision.Reason = "Read-only filesystem access is low risk."
		return decision
	}
	if access == "write" && mode != "review" && !runnerAutopilotBroadWrite(target) {
		decision.Risk = "medium"
		decision.Action = "approve_once"
		decision.Reason = "Workspace write request is allowed in Work mode."
		return decision
	}
	if access == "write" && mode == "review" {
		decision.Reason = "Ask mode does not auto-approve writes."
		return decision
	}
	decision.Reason = "Runner permission is outside low-risk autopilot policy."
	return decision
}

func runnerAutopilotSensitiveTarget(target string) bool {
	segments := runnerAutopilotPathSegments(target)
	if len(segments) == 0 {
		return false
	}
	for _, segment := range segments {
		lower := strings.ToLower(segment)
		switch lower {
		case ".ssh", ".gnupg", ".aws", ".config":
			return true
		}
	}
	if runnerAutopilotSystemPath(segments) {
		return true
	}
	base := strings.ToLower(filepath.Base(target))
	if runnerAutopilotSensitiveBasename(base) {
		return true
	}
	return runnerAutopilotSensitiveBasenameKeywords(base)
}

func runnerAutopilotPathSegments(target string) []string {
	normalized := filepath.ToSlash(filepath.Clean(target))
	normalized = strings.Trim(normalized, "/")
	if normalized == "" {
		return nil
	}
	parts := strings.Split(normalized, "/")
	segments := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" || part == "." {
			continue
		}
		segments = append(segments, part)
	}
	return segments
}

func runnerAutopilotSystemPath(segments []string) bool {
	if len(segments) == 0 {
		return false
	}
	switch strings.ToLower(segments[0]) {
	case "etc":
		return true
	case "var":
		return len(segments) >= 2 && strings.EqualFold(segments[1], "db")
	}
	return false
}

func runnerAutopilotSensitiveBasename(base string) bool {
	switch base {
	case ".env", "approvals.key", "config.json", "config.yaml", "config.yml":
		return true
	}
	if strings.HasPrefix(base, ".env.") {
		return true
	}
	return false
}

func runnerAutopilotSensitiveBasenameKeywords(base string) bool {
	for _, keyword := range []string{
		"secret", "token", "credential", "keychain", "approval", "sandbox", "security",
	} {
		if strings.Contains(base, keyword) {
			return true
		}
	}
	return false
}

func runnerAutopilotBroadWrite(target string) bool {
	base := strings.Trim(target, "/")
	if base == "" {
		return true
	}
	for _, suffix := range []string{"/", "/..", "/.", "/*"} {
		if strings.HasSuffix(target, suffix) {
			return true
		}
	}
	return false
}
