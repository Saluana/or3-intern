package approval

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

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
	if b == nil || b.DB == nil {
		return IssuedApproval{}, fmt.Errorf("approval broker unavailable")
	}
	if len(b.SignKey) == 0 {
		return IssuedApproval{}, fmt.Errorf("approval signing key unavailable")
	}
	now := b.now()
	nowMS := now.UnixMilli()
	tokenExpiresAt := now.Add(time.Duration(b.Config.ApprovalTokenTTLSeconds) * time.Second).UnixMilli()
	artifacts, err := b.DB.ApproveRequestWithArtifacts(ctx, requestID, actor, alwaysAllow, resolutionKind(alwaysAllow), note, nowMS, tokenExpiresAt, func(req db.ApprovalRequestRecord) (db.ApprovalAllowlistRecord, error) {
		return b.allowlistRecordFromRequest(ctx, req, actor)
	})
	if err != nil {
		return IssuedApproval{}, err
	}
	token, err := b.signTokenFromRecord(artifacts.Request, artifacts.TokenRecord)
	if err != nil {
		return IssuedApproval{}, err
	}
	_ = b.audit(ctx, "approval.resolved", map[string]any{
		"request_id": requestID, "subject_hash": artifacts.Request.SubjectHash,
		"host_id": artifacts.Request.ExecutionHostID, "outcome": "approved",
		"actor": actor, "allowlist_id": artifacts.AllowlistID,
	})
	return IssuedApproval{Request: artifacts.Request, Token: token, AllowlistID: artifacts.AllowlistID}, nil
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
	expiredIDs, count, err := b.DB.ExpireApprovalRequestsReturning(ctx, nowMS, strings.TrimSpace(actor), "expired by operator request")
	if err != nil {
		return 0, err
	}
	for _, requestID := range expiredIDs {
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

func (b *Broker) allowlistRecordFromRequest(ctx context.Context, req db.ApprovalRequestRecord, actor string) (db.ApprovalAllowlistRecord, error) {
	scopeJSON, matcherJSON, keys, err := b.allowlistPayloadFromRequest(req)
	if err != nil {
		return db.ApprovalAllowlistRecord{}, err
	}
	if strings.TrimSpace(scopeJSON) == "" && strings.TrimSpace(matcherJSON) == "" {
		return db.ApprovalAllowlistRecord{}, nil
	}
	return db.ApprovalAllowlistRecord{
		Domain:              req.Type,
		ScopeJSON:           scopeJSON,
		MatcherJSON:         matcherJSON,
		CreatedBy:           actor,
		CreatedAt:           b.now().UnixMilli(),
		ScopeHostID:         keys.ScopeHostID,
		ScopeTool:           keys.ScopeTool,
		ScopeProfile:        keys.ScopeProfile,
		ScopeAgent:          keys.ScopeAgent,
		MatchExecutablePath: keys.MatchExecutablePath,
		MatchWorkingDir:     keys.MatchWorkingDir,
		MatchScriptHash:     keys.MatchScriptHash,
		MatchSkillID:        keys.MatchSkillID,
		MatchPlanHash:       keys.MatchPlanHash,
		MatchRunnerID:       keys.MatchRunnerID,
		MatchTargetPath:     keys.MatchTargetPath,
		MatchPathPrefix:     keys.MatchPathPrefix,
		MatchFingerprint:    keys.MatchFingerprint,
	}, nil
}

func (b *Broker) allowlistPayloadFromRequest(req db.ApprovalRequestRecord) (string, string, AllowlistMatchKeys, error) {
	switch SubjectType(req.Type) {
	case SubjectExec:
		var subject ExecSubject
		if err := json.Unmarshal([]byte(req.SubjectJSON), &subject); err != nil {
			return "", "", AllowlistMatchKeys{}, err
		}
		scope := AllowlistScope{HostID: subject.ExecutionHostID, Tool: subject.ToolName, Profile: subject.AccessProfile, Agent: subject.RequestingAgent}
		matcher := ExecAllowlistMatcher{ExecutablePath: subject.ExecutablePath, Argv: subject.Argv, WorkingDir: subject.WorkingDir, ScriptHash: subject.ScriptHash}
		scopeJSON, err := marshalCanonical(scope)
		if err != nil {
			return "", "", AllowlistMatchKeys{}, err
		}
		matcherJSON, err := marshalCanonical(matcher)
		if err != nil {
			return "", "", AllowlistMatchKeys{}, err
		}
		return scopeJSON, matcherJSON, allowlistMatchKeys(req.Type, scope, matcher), nil
	case SubjectSkillExec:
		var subject SkillExecutionSubject
		if err := json.Unmarshal([]byte(req.SubjectJSON), &subject); err != nil {
			return "", "", AllowlistMatchKeys{}, err
		}
		scope := AllowlistScope{HostID: subject.ExecutionHostID, Tool: subject.ToolName, Agent: subject.RequestingAgent}
		matcher := SkillAllowlistMatcher{SkillID: subject.SkillID, Version: subject.Version, Origin: subject.Origin, TrustState: subject.TrustState, PlanHash: subject.PlanHash, ScriptHash: subject.ScriptHash, EnvBindingHash: subject.EnvBindingHash, TimeoutSeconds: subject.TimeoutSeconds}
		scopeJSON, err := marshalCanonical(scope)
		if err != nil {
			return "", "", AllowlistMatchKeys{}, err
		}
		matcherJSON, err := marshalCanonical(matcher)
		if err != nil {
			return "", "", AllowlistMatchKeys{}, err
		}
		return scopeJSON, matcherJSON, allowlistMatchKeys(req.Type, scope, matcher), nil
	case SubjectRunnerPermission:
		var subject RunnerPermissionSubject
		if err := json.Unmarshal([]byte(req.SubjectJSON), &subject); err != nil {
			return "", "", AllowlistMatchKeys{}, err
		}
		scope := AllowlistScope{HostID: subject.ExecutionHostID, Tool: subject.RunnerID, Agent: subject.RequestingAgent}
		matcher := RunnerPermissionAllowlistMatcher{RunnerID: subject.RunnerID, PermissionKind: subject.PermissionKind, Access: subject.Access, PathPrefix: subject.TargetPath}
		scopeJSON, err := marshalCanonical(scope)
		if err != nil {
			return "", "", AllowlistMatchKeys{}, err
		}
		matcherJSON, err := marshalCanonical(matcher)
		if err != nil {
			return "", "", AllowlistMatchKeys{}, err
		}
		return scopeJSON, matcherJSON, allowlistMatchKeys(req.Type, scope, matcher), nil
	default:
		return "", "", AllowlistMatchKeys{}, nil
	}
}

func (b *Broker) createAllowlistFromRequest(ctx context.Context, req db.ApprovalRequestRecord, actor string) (int64, error) {
	input, err := b.allowlistRecordFromRequest(ctx, req, actor)
	if err != nil {
		return 0, err
	}
	rec, err := b.AddAllowlistRecord(ctx, input)
	if err != nil {
		return 0, err
	}
	return rec.ID, nil
}
