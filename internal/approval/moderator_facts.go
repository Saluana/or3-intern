package approval

import (
	"context"
	"path/filepath"
	"strings"

	"or3-intern/internal/config"
)

func buildModeratorReviewInput(workspace string, subjectType SubjectType, subject any, sh SubjectHash, mode config.ApprovalMode, scope AllowlistScope, ctx context.Context, maxSubjectChars int) ModeratorReviewInput {
	preview := SafeSubjectPreview(string(subjectType), sh.JSON)
	facts := moderatorSubjectFacts(workspace, subjectType, subject, scope)
	redactedFacts, factStats := redactModeratorMap(facts, maxSubjectChars)
	redactedPreview, previewStats := redactModeratorText(preview, maxSubjectChars)
	stats := redactionStats{}
	mergeRedactionStats(&stats, factStats)
	mergeRedactionStats(&stats, previewStats)
	accessProfile := strings.TrimSpace(scope.Profile)
	if accessProfile == "" {
		switch typed := subject.(type) {
		case ExecSubject:
			accessProfile = strings.TrimSpace(typed.AccessProfile)
		}
	}
	return ModeratorReviewInput{
		SubjectType:    subjectType,
		SubjectHash:    sh.Hash,
		SubjectPreview: redactedPreview,
		SubjectFacts:   redactedFacts,
		PolicyMode:     mode,
		AccessProfile:  accessProfile,
		Requester:      RequesterContextFromContext(ctx),
		Redactions:     stats,
	}
}

func moderatorSubjectFacts(workspace string, subjectType SubjectType, subject any, scope AllowlistScope) map[string]any {
	facts := map[string]any{
		"subject_type": string(subjectType),
		"host_id":      strings.TrimSpace(scope.HostID),
		"tool":         strings.TrimSpace(scope.Tool),
		"agent":        strings.TrimSpace(scope.Agent),
		"profile":      strings.TrimSpace(scope.Profile),
	}
	switch subjectType {
	case SubjectExec:
		if typed, ok := subject.(ExecSubject); ok {
			facts["executable"] = filepath.Base(strings.TrimSpace(typed.ExecutablePath))
			facts["argv"] = append([]string{}, typed.Argv...)
			facts["working_dir"] = strings.TrimSpace(typed.WorkingDir)
			facts["sandbox_id"] = strings.TrimSpace(typed.SandboxID)
			facts["legacy_shell"] = looksLikeLegacyShell(typed.ExecutablePath, typed.Argv)
			facts["workspace_relation"] = workspaceRelation(workspace, strings.TrimSpace(typed.WorkingDir))
		}
	case SubjectSkillExec:
		if typed, ok := subject.(SkillExecutionSubject); ok {
			facts["skill_id"] = strings.TrimSpace(typed.SkillID)
			facts["version"] = strings.TrimSpace(typed.Version)
			facts["origin"] = strings.TrimSpace(typed.Origin)
			facts["trust_state"] = strings.TrimSpace(typed.TrustState)
			facts["timeout_seconds"] = typed.TimeoutSeconds
		}
	case SubjectRunnerPermission:
		if typed, ok := subject.(RunnerPermissionSubject); ok {
			facts["runner_id"] = strings.TrimSpace(typed.RunnerID)
			facts["permission_kind"] = strings.TrimSpace(typed.PermissionKind)
			facts["access"] = strings.TrimSpace(typed.Access)
			facts["target_path"] = strings.TrimSpace(typed.TargetPath)
			facts["workspace_relation"] = workspaceRelation(workspace, strings.TrimSpace(typed.TargetPath))
		}
	case SubjectSecretAccess:
		if typed, ok := subject.(SecretAccessSubject); ok {
			facts["secret_name"] = strings.TrimSpace(typed.SecretName)
			facts["operation"] = strings.TrimSpace(typed.Operation)
		}
	case SubjectToolQuota:
		if typed, ok := subject.(ToolQuotaSubject); ok {
			facts["scope"] = strings.TrimSpace(typed.Scope)
			facts["limit_name"] = strings.TrimSpace(typed.LimitName)
			facts["tool_name"] = strings.TrimSpace(typed.ToolName)
			facts["current"] = typed.Current
			facts["limit"] = typed.Limit
		}
	case SubjectMessageSend:
		if typed, ok := subject.(MessageSendSubject); ok {
			facts["channel"] = strings.TrimSpace(typed.Channel)
			facts["to_hash"] = hashForDiagnostics(typed.To)
			facts["text_length"] = typed.TextLength
			facts["media_count"] = typed.MediaCount
			facts["reply_in_thread"] = typed.ReplyInThread
		}
	case SubjectFileTransfer:
		if typed, ok := subject.(FileTransferSubject); ok {
			facts["path"] = strings.TrimSpace(typed.Path)
			facts["destination"] = strings.TrimSpace(typed.Destination)
			facts["workspace_relation"] = workspaceRelation(workspace, strings.TrimSpace(typed.Path))
		}
	}
	return facts
}

func looksLikeLegacyShell(executable string, argv []string) bool {
	base := strings.ToLower(filepath.Base(strings.TrimSpace(executable)))
	if base == "sh" || base == "bash" || base == "zsh" {
		return true
	}
	for _, arg := range argv {
		if strings.Contains(strings.ToLower(arg), "&&") || strings.Contains(strings.ToLower(arg), "|") {
			return true
		}
	}
	return false
}
