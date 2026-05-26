package approval

import (
	"context"
	"fmt"
	"strings"
	"time"

	"or3-intern/internal/config"
	"or3-intern/internal/db"
)

func (b *Broker) moderatorEnabled() bool {
	return b != nil && b.Moderator != nil && b.Config.Moderator.Enabled && b.Config.Enabled
}

func (b *Broker) createApprovalRequest(ctx context.Context, subjectType SubjectType, subject any, sh SubjectHash, scope AllowlistScope, mode config.ApprovalMode) (db.ApprovalRequestRecord, bool, error) {
	nowMS := b.now().UnixMilli()
	return b.DB.CreateOrGetPendingApprovalRequest(ctx, db.ApprovalRequestRecord{
		Type: string(subjectType), SubjectHash: sh.Hash, SubjectJSON: sh.JSON,
		RequesterAgentID: scope.Agent, RequesterSessionID: extractSessionID(subject),
		RequesterContextJSON: MarshalRequesterContext(RequesterContextFromContext(ctx)),
		ExecutionHostID: b.hostID(), Status: StatusPending, PolicyMode: string(mode),
		RequestedAt: nowMS, ExpiresAt: nowMS + int64(b.Config.PendingTTLSeconds*1000),
	}, nowMS)
}

func (b *Broker) evaluateModerator(ctx context.Context, subjectType SubjectType, subject any, sh SubjectHash, scope AllowlistScope, mode config.ApprovalMode) (Decision, error) {
	if !b.moderatorEnabled() {
		return b.requireApproval(ctx, subjectType, subject, sh, scope, mode)
	}
	req, reused, err := b.createApprovalRequest(ctx, subjectType, subject, sh, scope, mode)
	if err != nil {
		return Decision{}, err
	}
	if hard, ok := deterministicHardDeny(subjectType, subject); ok {
		return b.finishModeratorDecision(ctx, subjectType, req, sh, hard, "hard_deny", 0, nil, ModeratorReviewInput{})
	}
	input := buildModeratorReviewInput(b.Workspace, subjectType, subject, sh, mode, scope, ctx, b.Config.Moderator.MaxSubjectChars)
	input.RequestID = req.ID
	_ = b.audit(ctx, "approval.moderator.requested", moderatorAuditPayload(b.Moderator, subjectType, req.ID, sh, ModeratorReviewResult{}, input, 0, "requested", map[string]any{
		"reused": reused,
	}))
	start := time.Now()
	result, reviewErr := b.Moderator.ReviewApproval(ctx, input)
	latencyMS := time.Since(start).Milliseconds()
	if reviewErr != nil {
		failure := ModeratorReviewResult{
			Action: configFailureAction(b.Config.Moderator),
			Risk:   RiskHigh,
			Reason: "moderator review failed",
		}
		payload := moderatorAuditPayload(b.Moderator, subjectType, req.ID, sh, failure, input, latencyMS, "failure", map[string]any{
			"error_category": moderatorErrorCategory(reviewErr),
			"failure_action": failure.Action,
		})
		_ = b.audit(ctx, "approval.moderator.failure", payload)
		return b.finishModeratorDecision(ctx, subjectType, req, sh, failure, "failure", latencyMS, reviewErr, input)
	}
	result = enforceModeratorDecision(b.Config.Moderator, result, subjectType, subject, b.Config.Moderator.UserPolicy)
	return b.finishModeratorDecision(ctx, subjectType, req, sh, result, "reviewed", latencyMS, nil, input)
}

func (b *Broker) finishModeratorDecision(ctx context.Context, subjectType SubjectType, req db.ApprovalRequestRecord, sh SubjectHash, result ModeratorReviewResult, status string, latencyMS int64, reviewErr error, input ModeratorReviewInput) (Decision, error) {
	meta := db.ApprovalModeratorMetadata{
		Status: status, Risk: string(result.Risk), Action: string(result.Action),
		Reason: result.Reason, Model: b.Moderator.ModelIdentity(),
		PolicyHash: b.Moderator.PolicyHash(), ReviewedAt: b.now().UnixMilli(), LatencyMS: latencyMS,
	}
	if err := b.DB.UpdateApprovalRequestModeratorMetadata(ctx, req.ID, meta); err != nil {
		if result.Action == ModeratorApprove || result.Action == ModeratorDeny {
			return Decision{}, fmt.Errorf("persist moderator metadata: %w", err)
		}
		_ = b.audit(ctx, "approval.moderator.metadata_failed", moderatorAuditPayload(b.Moderator, subjectType, req.ID, sh, result, input, latencyMS, status, map[string]any{
			"error": err.Error(),
		}))
	}

	switch result.Action {
	case ModeratorApprove:
		if len(b.SignKey) == 0 {
			payload := moderatorAuditPayload(b.Moderator, subjectType, req.ID, sh, result, input, latencyMS, "escalated", map[string]any{
				"reason": "broker_cannot_issue_token",
			})
			_ = b.audit(ctx, "approval.moderator.escalated", payload)
			return Decision{Allowed: false, RequiresApproval: true, RequestID: req.ID, SubjectHash: sh.Hash, Reason: "approval required"}, nil
		}
		if _, err := b.ApproveRequest(ctx, req.ID, b.Moderator.ModelIdentity(), false, result.Reason); err != nil {
			return Decision{}, err
		}
		_ = b.audit(ctx, "approval.moderator.approved", moderatorAuditPayload(b.Moderator, subjectType, req.ID, sh, result, input, latencyMS, status, nil))
		return Decision{Allowed: true, RequestID: req.ID, SubjectHash: sh.Hash, Reason: "moderator_approved"}, nil
	case ModeratorDeny:
		note := result.Reason
		if alt := strings.TrimSpace(result.Alternative); alt != "" {
			note = note + "; alternative: " + alt
		}
		if err := b.DenyRequest(ctx, req.ID, b.Moderator.ModelIdentity(), note); err != nil {
			return Decision{}, err
		}
		_ = b.audit(ctx, "approval.moderator.denied", moderatorAuditPayload(b.Moderator, subjectType, req.ID, sh, result, input, latencyMS, status, nil))
		reason := result.Reason
		if alt := strings.TrimSpace(result.Alternative); alt != "" {
			reason = fmt.Sprintf("%s. Try: %s", reason, alt)
		}
		return Decision{Allowed: false, RequestID: req.ID, SubjectHash: sh.Hash, Reason: reason}, nil
	default:
		_ = b.audit(ctx, "approval.moderator.escalated", moderatorAuditPayload(b.Moderator, subjectType, req.ID, sh, result, input, latencyMS, status, nil))
		if reviewErr != nil {
			return Decision{Allowed: false, RequiresApproval: true, RequestID: req.ID, SubjectHash: sh.Hash, Reason: "approval required"}, nil
		}
		return Decision{Allowed: false, RequiresApproval: true, RequestID: req.ID, SubjectHash: sh.Hash, Reason: "approval required"}, nil
	}
}

func moderatorAuditPayload(moderator Moderator, subjectType SubjectType, requestID int64, sh SubjectHash, result ModeratorReviewResult, input ModeratorReviewInput, latencyMS int64, status string, extra map[string]any) map[string]any {
	payload := map[string]any{
		"request_id":   requestID,
		"subject_hash": sh.Hash,
		"type":         string(subjectType),
		"status":       status,
		"latency_ms":   latencyMS,
	}
	if result.Risk != "" {
		payload["risk"] = string(result.Risk)
	}
	if result.Action != "" {
		payload["action"] = string(result.Action)
	}
	if strings.TrimSpace(result.Reason) != "" {
		payload["reason"] = result.Reason
	}
	if r := input.Redactions; r.Secrets > 0 || r.Tokens > 0 || r.Truncations > 0 {
		payload["redaction_secrets"] = input.Redactions.Secrets
		payload["redaction_tokens"] = input.Redactions.Tokens
		payload["redaction_truncations"] = input.Redactions.Truncations
	}
	if moderator != nil {
		payload["model"] = moderator.ModelIdentity()
		payload["policy_hash"] = moderator.PolicyHash()
	}
	for k, v := range extra {
		payload[k] = v
	}
	return payload
}

func moderatorErrorCategory(err error) string {
	if err == nil {
		return ""
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "timeout") || strings.Contains(msg, "deadline"):
		return "timeout"
	case strings.Contains(msg, "parse"):
		return "parse"
	default:
		return "provider"
	}
}
