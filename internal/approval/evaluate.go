package approval

import (
	"context"
	"strings"

	"or3-intern/internal/config"
)

func (b *Broker) EvaluateExec(ctx context.Context, req ExecEvaluation) (Decision, error) {
	subject := ExecSubject{
		Type:            string(SubjectExec),
		ExecutionHostID: b.hostID(),
		SandboxID:       strings.TrimSpace(req.SandboxID),
		ExecutablePath:  strings.TrimSpace(req.ExecutablePath),
		Argv:            append([]string{}, req.Argv...),
		WorkingDir:      strings.TrimSpace(req.WorkingDir),
		EnvBindingHash:  strings.TrimSpace(req.EnvBindingHash),
		ScriptHash:      strings.TrimSpace(req.ScriptHash),
		RequestingAgent: strings.TrimSpace(req.AgentID),
		SessionID:       strings.TrimSpace(req.SessionID),
		ToolName:        firstNonEmpty(req.ToolName, "exec"),
		AccessProfile:   strings.TrimSpace(req.AccessProfile),
	}
	return b.evaluate(ctx, SubjectExec, subject, req.ApprovalToken,
		AllowlistScope{HostID: subject.ExecutionHostID, Tool: subject.ToolName, Profile: subject.AccessProfile, Agent: subject.RequestingAgent},
		ExecAllowlistMatcher{ExecutablePath: subject.ExecutablePath, Argv: subject.Argv, WorkingDir: subject.WorkingDir, ScriptHash: subject.ScriptHash},
	)
}

func (b *Broker) EvaluateSkillExec(ctx context.Context, req SkillEvaluation) (Decision, error) {
	subject := SkillExecutionSubject{
		Type:            string(SubjectSkillExec),
		SkillID:         strings.TrimSpace(req.SkillID),
		Version:         strings.TrimSpace(req.Version),
		Origin:          strings.TrimSpace(req.Origin),
		TrustState:      strings.TrimSpace(req.TrustState),
		ToolName:        firstNonEmpty(req.ToolName, "run_skill"),
		PlanID:          strings.TrimSpace(req.PlanID),
		PlanHash:        strings.TrimSpace(req.PlanHash),
		ScriptHash:      strings.TrimSpace(req.ScriptHash),
		ExecutionHostID: b.hostID(),
		EnvBindingHash:  strings.TrimSpace(req.EnvBindingHash),
		TimeoutSeconds:  req.TimeoutSeconds,
		RequestingAgent: strings.TrimSpace(req.AgentID),
		SessionID:       strings.TrimSpace(req.SessionID),
	}
	return b.evaluate(ctx, SubjectSkillExec, subject, req.ApprovalToken,
		AllowlistScope{HostID: subject.ExecutionHostID, Tool: subject.ToolName, Agent: subject.RequestingAgent},
		SkillAllowlistMatcher{SkillID: subject.SkillID, Version: subject.Version, Origin: subject.Origin, TrustState: subject.TrustState, PlanHash: subject.PlanHash, ScriptHash: subject.ScriptHash, EnvBindingHash: subject.EnvBindingHash, TimeoutSeconds: subject.TimeoutSeconds},
	)
}

func (b *Broker) EvaluateSecretAccess(ctx context.Context, req SecretAccessEvaluation) (Decision, error) {
	subject := SecretAccessSubject{
		Type:            string(SubjectSecretAccess),
		ExecutionHostID: b.hostID(),
		SecretName:      strings.TrimSpace(req.SecretName),
		Operation:       firstNonEmpty(req.Operation, "read"),
		RequestingAgent: strings.TrimSpace(req.AgentID),
		SessionID:       strings.TrimSpace(req.SessionID),
	}
	return b.evaluate(ctx, SubjectSecretAccess, subject, req.ApprovalToken,
		AllowlistScope{HostID: subject.ExecutionHostID, Agent: subject.RequestingAgent},
		nil,
	)
}

func (b *Broker) EvaluateRunnerPermission(ctx context.Context, req RunnerPermissionEvaluation) (Decision, error) {
	subject := RunnerPermissionSubject{
		Type:            string(SubjectRunnerPermission),
		ExecutionHostID: b.hostID(),
		RunnerID:        strings.TrimSpace(req.RunnerID),
		PermissionKind:  firstNonEmpty(req.PermissionKind, "filesystem"),
		Access:          firstNonEmpty(req.Access, "read"),
		TargetPath:      strings.TrimSpace(req.TargetPath),
		RequestingAgent: strings.TrimSpace(req.AgentID),
		SessionID:       strings.TrimSpace(req.SessionID),
	}
	return b.evaluateWithMode(ctx, SubjectRunnerPermission, subject, req.ApprovalToken, b.Config.Exec.Mode,
		AllowlistScope{HostID: subject.ExecutionHostID, Tool: subject.RunnerID, Agent: subject.RequestingAgent},
		RunnerPermissionAllowlistMatcher{RunnerID: subject.RunnerID, PermissionKind: subject.PermissionKind, Access: subject.Access, TargetPath: subject.TargetPath},
	)
}

func (b *Broker) EvaluateToolQuota(ctx context.Context, req ToolQuotaEvaluation, mode config.ApprovalMode) (Decision, error) {
	subject := ToolQuotaSubject{
		Type:            string(SubjectToolQuota),
		ExecutionHostID: b.hostID(),
		Scope:           strings.TrimSpace(req.Scope),
		LimitName:       strings.TrimSpace(req.LimitName),
		ToolName:        strings.TrimSpace(req.ToolName),
		Current:         req.Current,
		Limit:           req.Limit,
		RequestingAgent: strings.TrimSpace(req.AgentID),
		SessionID:       strings.TrimSpace(req.SessionID),
	}
	return b.evaluateWithMode(ctx, SubjectToolQuota, subject, req.ApprovalToken, mode,
		AllowlistScope{HostID: subject.ExecutionHostID, Tool: subject.ToolName, Agent: subject.RequestingAgent},
		nil,
	)
}

func (b *Broker) EvaluateMessageSend(ctx context.Context, req MessageSendEvaluation) (Decision, error) {
	subject := MessageSendSubject{
		Type:            string(SubjectMessageSend),
		ExecutionHostID: b.hostID(),
		Channel:         strings.TrimSpace(req.Channel),
		To:              strings.TrimSpace(req.To),
		TextLength:      len(strings.TrimSpace(req.Text)),
		MediaCount:      req.MediaCount,
		ReplyInThread:   req.ReplyInThread,
		RequestingAgent: strings.TrimSpace(req.AgentID),
		SessionID:       strings.TrimSpace(req.SessionID),
	}
	return b.evaluate(ctx, SubjectMessageSend, subject, req.ApprovalToken,
		AllowlistScope{HostID: subject.ExecutionHostID, Tool: "send_message", Agent: subject.RequestingAgent},
		nil,
	)
}

func (b *Broker) evaluate(ctx context.Context, subjectType SubjectType, subject any, approvalToken string, scope AllowlistScope, matcher any) (Decision, error) {
	return b.evaluateWithMode(ctx, subjectType, subject, approvalToken, b.modeFor(subjectType), scope, matcher)
}

func (b *Broker) evaluateWithMode(ctx context.Context, subjectType SubjectType, subject any, approvalToken string, mode config.ApprovalMode, scope AllowlistScope, matcher any) (Decision, error) {
	sh, err := CanonicalSubjectHash(subject)
	if err != nil {
		return Decision{}, err
	}

	if dec, ok := b.checkExistingToken(ctx, approvalToken, sh.Hash); ok {
		return dec, nil
	}
	if dec, ok := b.checkPolicyMode(ctx, subjectType, sh, mode); ok {
		return dec, nil
	}
	if mode == config.ApprovalModeAllowlist {
		if dec, ok := b.checkAllowlist(ctx, subjectType, scope, matcher, sh, mode); ok {
			return dec, nil
		}
	}
	return b.evaluateModerator(ctx, subjectType, subject, sh, scope, mode)
}

func (b *Broker) checkExistingToken(ctx context.Context, approvalToken string, subjectHash string) (Decision, bool) {
	if strings.TrimSpace(approvalToken) == "" {
		return Decision{}, false
	}
	claims, err := b.VerifyApprovalTokenClaims(ctx, approvalToken, subjectHash, b.hostID())
	if err != nil {
		return Decision{}, false
	}
	return Decision{Allowed: true, RequestID: claims.RequestID, SubjectHash: subjectHash, Reason: "approved_token"}, true
}

func (b *Broker) checkPolicyMode(ctx context.Context, subjectType SubjectType, sh SubjectHash, mode config.ApprovalMode) (Decision, bool) {
	switch mode {
	case config.ApprovalModeTrusted:
		_ = b.audit(ctx, "approval.trusted", map[string]any{
			"subject_hash": sh.Hash, "host_id": b.hostID(),
			"type": string(subjectType), "outcome": "allowed",
		})
		return Decision{Allowed: true, SubjectHash: sh.Hash, Reason: "trusted"}, true
	case config.ApprovalModeDeny:
		_ = b.audit(ctx, "approval.blocked", map[string]any{
			"subject_hash": sh.Hash, "host_id": b.hostID(),
			"type": string(subjectType), "outcome": "blocked", "reason": "deny",
		})
		return Decision{Allowed: false, SubjectHash: sh.Hash, Reason: "approval denied by policy"}, true
	default:
		if b.DB == nil {
			_ = b.audit(ctx, "approval.blocked", map[string]any{
				"subject_hash": sh.Hash, "host_id": b.hostID(),
				"type": string(subjectType), "outcome": "blocked", "reason": "broker_unavailable",
			})
			return Decision{Allowed: false, SubjectHash: sh.Hash, Reason: "approval broker unavailable"}, true
		}
		if len(b.SignKey) == 0 {
			_ = b.audit(ctx, "approval.blocked", map[string]any{
				"subject_hash": sh.Hash, "host_id": b.hostID(),
				"type": string(subjectType), "outcome": "blocked", "reason": "broker_unavailable",
			})
			return Decision{Allowed: false, SubjectHash: sh.Hash, Reason: "approval broker unavailable"}, true
		}
		return Decision{}, false
	}
}

func (b *Broker) checkAllowlist(ctx context.Context, subjectType SubjectType, scope AllowlistScope, matcher any, sh SubjectHash, mode config.ApprovalMode) (Decision, bool) {
	if mode != config.ApprovalModeAllowlist {
		return Decision{}, false
	}
	matched, err := b.allowlistMatches(ctx, subjectType, scope, matcher)
	if err != nil || !matched {
		return Decision{}, false
	}
	_ = b.audit(ctx, "approval.allowlist_match", map[string]any{
		"subject_hash": sh.Hash, "host_id": b.hostID(),
		"type": string(subjectType), "outcome": "allowed",
	})
	return Decision{Allowed: true, SubjectHash: sh.Hash, Reason: "allowlist"}, true
}

func (b *Broker) requireApproval(ctx context.Context, subjectType SubjectType, subject any, sh SubjectHash, scope AllowlistScope, mode config.ApprovalMode) (Decision, error) {
	req, reused, err := b.createApprovalRequest(ctx, subjectType, subject, sh, scope, mode)
	if err != nil {
		return Decision{}, err
	}
	_ = reused
	_ = b.audit(ctx, "approval.requested", map[string]any{
		"request_id": req.ID, "subject_hash": sh.Hash, "host_id": b.hostID(),
		"type": string(subjectType), "policy_mode": string(mode), "outcome": "pending",
	})
	return Decision{Allowed: false, RequiresApproval: true, RequestID: req.ID, SubjectHash: sh.Hash, Reason: "approval required"}, nil
}

func (b *Broker) modeFor(subjectType SubjectType) config.ApprovalMode {
	switch subjectType {
	case SubjectExec:
		return b.Config.Exec.Mode
	case SubjectSkillExec:
		return b.Config.SkillExecution.Mode
	case SubjectRunnerPermission:
		return b.Config.Exec.Mode
	case SubjectSecretAccess:
		return b.Config.SecretAccess.Mode
	case SubjectMessageSend:
		return b.Config.MessageSend.Mode
	default:
		return config.ApprovalModeDeny
	}
}

type subject interface {
	GetSessionID() string
}

func extractSessionID(s any) string {
	if sub, ok := s.(subject); ok {
		return sub.GetSessionID()
	}
	return ""
}
