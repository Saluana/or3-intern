package approval

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"or3-intern/internal/db"
)

func (b *Broker) resolveRequest(ctx context.Context, requestID int64, actor string, targetStatus string, note string) (db.ApprovalRequestRecord, error) {
	req, err := b.DB.GetApprovalRequest(ctx, requestID)
	if err != nil {
		return db.ApprovalRequestRecord{}, err
	}
	if req.Status != StatusPending {
		return db.ApprovalRequestRecord{}, fmt.Errorf("approval request is not pending")
	}
	nowMS := b.now().UnixMilli()
	resolved, err := b.DB.ResolveApprovalRequest(ctx, requestID, StatusPending, targetStatus, nowMS, strings.TrimSpace(actor), targetStatus, strings.TrimSpace(note))
	if err != nil {
		return db.ApprovalRequestRecord{}, err
	}
	if !resolved {
		return db.ApprovalRequestRecord{}, fmt.Errorf("approval request is not pending")
	}
	return req, nil
}

func (b *Broker) ApproveRequest(ctx context.Context, requestID int64, actor string, alwaysAllow bool, note string) (IssuedApproval, error) {
	req, err := b.DB.GetApprovalRequest(ctx, requestID)
	if err != nil {
		return IssuedApproval{}, err
	}
	if req.Status != StatusPending {
		return IssuedApproval{}, fmt.Errorf("approval request is not pending")
	}
	nowMS := b.now().UnixMilli()
	if req.ExpiresAt > 0 && req.ExpiresAt < nowMS {
		_, _ = b.DB.ResolveApprovalRequest(ctx, requestID, StatusPending, StatusExpired, nowMS, actor, StatusExpired, "expired before approval")
		return IssuedApproval{}, fmt.Errorf("approval request expired")
	}
	resolved, err := b.DB.ResolveApprovalRequest(ctx, requestID, StatusPending, StatusApproved, nowMS, strings.TrimSpace(actor), resolutionKind(alwaysAllow), strings.TrimSpace(note))
	if err != nil {
		return IssuedApproval{}, err
	}
	if !resolved {
		return IssuedApproval{}, fmt.Errorf("approval request is not pending")
	}
	b.syncSkillRunPlanResolution(ctx, requestID, db.SkillRunStatusApproved, "")
	allowlistID := int64(0)
	if alwaysAllow {
		allowlistID, err = b.createAllowlistFromRequest(ctx, req, actor)
		if err != nil {
			return IssuedApproval{}, err
		}
	}
	token, err := b.issueTokenForRequest(ctx, req, actor)
	if err != nil {
		return IssuedApproval{}, err
	}
	_ = b.audit(ctx, "approval.resolved", map[string]any{
		"request_id": requestID, "subject_hash": req.SubjectHash,
		"host_id": req.ExecutionHostID, "outcome": "approved",
		"actor": actor, "allowlist_id": allowlistID,
	})
	return IssuedApproval{Request: req, Token: token, AllowlistID: allowlistID}, nil
}

func (b *Broker) DenyRequest(ctx context.Context, requestID int64, actor string, note string) error {
	req, err := b.resolveRequest(ctx, requestID, actor, StatusDenied, note)
	if err != nil {
		return err
	}
	b.syncSkillRunPlanResolution(ctx, requestID, db.SkillRunStatusDenied, firstNonEmpty(note, "approval denied"))
	_ = b.audit(ctx, "approval.resolved", map[string]any{
		"request_id": requestID, "subject_hash": req.SubjectHash,
		"host_id": req.ExecutionHostID, "outcome": "denied", "actor": actor,
	})
	return nil
}

func (b *Broker) CancelRequest(ctx context.Context, requestID int64, actor string, note string) error {
	req, err := b.resolveRequest(ctx, requestID, actor, StatusCanceled, note)
	if err != nil {
		return err
	}
	b.syncSkillRunPlanResolution(ctx, requestID, db.SkillRunStatusCancelled, firstNonEmpty(note, "approval canceled"))
	_ = b.audit(ctx, "approval.resolved", map[string]any{
		"request_id": requestID, "subject_hash": req.SubjectHash,
		"host_id": req.ExecutionHostID, "outcome": "canceled", "actor": actor,
	})
	return nil
}

func (b *Broker) ExpirePendingRequests(ctx context.Context, actor string) (int64, error) {
	nowMS := b.now().UnixMilli()
	requestIDs, err := b.DB.ListExpiredPendingApprovalRequestIDs(ctx, nowMS)
	if err != nil {
		return 0, err
	}
	count, err := b.DB.ExpireApprovalRequests(ctx, nowMS, strings.TrimSpace(actor), "expired by operator request")
	if err != nil {
		return 0, err
	}
	for _, requestID := range requestIDs {
		b.syncSkillRunPlanResolution(ctx, requestID, db.SkillRunStatusExpired, "approval expired")
	}
	if count > 0 {
		_ = b.audit(ctx, "approval.expired", map[string]any{"count": count, "actor": actor, "host_id": b.hostID()})
	}
	return count, nil
}

func (b *Broker) syncSkillRunPlanResolution(ctx context.Context, requestID int64, status string, lastError string) {
	if b == nil || b.DB == nil || requestID <= 0 {
		return
	}
	if _, err := b.DB.UpdateSkillRunPlansByApprovalRequest(ctx, requestID, []string{string(db.SkillRunStatusPendingApproval)}, status, strings.TrimSpace(lastError), b.now().UnixMilli()); err != nil {
		_ = b.audit(ctx, "approval.plan_sync_failed", map[string]any{
			"request_id": requestID, "status": strings.TrimSpace(status),
			"error": err.Error(), "host_id": b.hostID(),
		})
	}
}

func (b *Broker) createAllowlistFromRequest(ctx context.Context, req db.ApprovalRequestRecord, actor string) (int64, error) {
	switch SubjectType(req.Type) {
	case SubjectExec:
		var subject ExecSubject
		if err := json.Unmarshal([]byte(req.SubjectJSON), &subject); err != nil {
			return 0, err
		}
		rec, err := b.AddAllowlist(ctx, req.Type,
			AllowlistScope{HostID: subject.ExecutionHostID, Tool: subject.ToolName, Profile: subject.AccessProfile, Agent: subject.RequestingAgent},
			ExecAllowlistMatcher{ExecutablePath: subject.ExecutablePath, Argv: subject.Argv, WorkingDir: subject.WorkingDir, ScriptHash: subject.ScriptHash},
			actor, 0,
		)
		if err != nil {
			return 0, err
		}
		return rec.ID, nil
	case SubjectSkillExec:
		var subject SkillExecutionSubject
		if err := json.Unmarshal([]byte(req.SubjectJSON), &subject); err != nil {
			return 0, err
		}
		rec, err := b.AddAllowlist(ctx, req.Type,
			AllowlistScope{HostID: subject.ExecutionHostID, Tool: subject.ToolName, Agent: subject.RequestingAgent},
			SkillAllowlistMatcher{SkillID: subject.SkillID, Version: subject.Version, Origin: subject.Origin, TrustState: subject.TrustState, PlanHash: subject.PlanHash, ScriptHash: subject.ScriptHash, EnvBindingHash: subject.EnvBindingHash, TimeoutSeconds: subject.TimeoutSeconds},
			actor, 0,
		)
		if err != nil {
			return 0, err
		}
		return rec.ID, nil
	case SubjectRunnerPermission:
		var subject RunnerPermissionSubject
		if err := json.Unmarshal([]byte(req.SubjectJSON), &subject); err != nil {
			return 0, err
		}
		rec, err := b.AddAllowlist(ctx, req.Type,
			AllowlistScope{HostID: subject.ExecutionHostID, Tool: subject.RunnerID, Agent: subject.RequestingAgent},
			RunnerPermissionAllowlistMatcher{RunnerID: subject.RunnerID, PermissionKind: subject.PermissionKind, Access: subject.Access, PathPrefix: subject.TargetPath},
			actor, 0,
		)
		if err != nil {
			return 0, err
		}
		return rec.ID, nil
	default:
		return 0, nil
	}
}
