package approval

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"or3-intern/internal/db"
)

func (b *Broker) AddAllowlist(ctx context.Context, domain string, scope AllowlistScope, matcher any, actor string, expiresAt int64) (db.ApprovalAllowlistRecord, error) {
	if err := ValidateAllowlistMatcher(domain, matcher); err != nil {
		return db.ApprovalAllowlistRecord{}, err
	}
	scopeJSON, err := marshalCanonical(scope)
	if err != nil {
		return db.ApprovalAllowlistRecord{}, err
	}
	matcherJSON, err := marshalCanonical(matcher)
	if err != nil {
		return db.ApprovalAllowlistRecord{}, err
	}
	rec, err := b.DB.CreateApprovalAllowlist(ctx, db.ApprovalAllowlistRecord{Domain: domain, ScopeJSON: scopeJSON, MatcherJSON: matcherJSON, CreatedBy: actor, CreatedAt: b.now().UnixMilli(), ExpiresAt: expiresAt})
	if err != nil {
		return db.ApprovalAllowlistRecord{}, err
	}
	_ = b.audit(ctx, "approval.allowlist_changed", map[string]any{"allowlist_id": rec.ID, "domain": domain, "actor": actor, "host_id": b.hostID(), "action": "add"})
	return rec, nil
}

func (b *Broker) RemoveAllowlist(ctx context.Context, id int64, actor string) error {
	if err := b.DB.DisableApprovalAllowlist(ctx, id, b.now().UnixMilli()); err != nil {
		return err
	}
	_ = b.audit(ctx, "approval.allowlist_changed", map[string]any{"allowlist_id": id, "actor": actor, "host_id": b.hostID(), "action": "remove"})
	return nil
}

func (b *Broker) ListAllowlists(ctx context.Context, domain string, limit int) ([]db.ApprovalAllowlistRecord, error) {
	return b.DB.ListApprovalAllowlists(ctx, domain, limit)
}

func (b *Broker) allowlistMatches(ctx context.Context, subjectType SubjectType, scope AllowlistScope, matcher any) (bool, error) {
	records, err := b.DB.ListApprovalAllowlists(ctx, string(subjectType), defaultPageSize)
	if err != nil {
		return false, err
	}
	nowMS := b.now().UnixMilli()
	for _, record := range records {
		if record.DisabledAt > 0 {
			continue
		}
		if record.ExpiresAt > 0 && record.ExpiresAt < nowMS {
			continue
		}
		if !allowlistScopeMatches(scope, record.ScopeJSON) {
			continue
		}
		matched, err := allowlistMatcherMatches(subjectType, matcher, record.MatcherJSON)
		if err != nil {
			return false, err
		}
		if matched {
			return true, nil
		}
	}
	return false, nil
}

func allowlistScopeMatches(scope AllowlistScope, raw string) bool {
	if strings.TrimSpace(raw) == "" {
		return true
	}
	var rec AllowlistScope
	if err := json.Unmarshal([]byte(raw), &rec); err != nil {
		return false
	}
	if rec.HostID != "" && rec.HostID != scope.HostID {
		return false
	}
	if rec.Tool != "" && rec.Tool != scope.Tool {
		return false
	}
	if rec.Profile != "" && rec.Profile != scope.Profile {
		return false
	}
	if rec.Agent != "" && rec.Agent != scope.Agent {
		return false
	}
	return true
}

func allowlistMatcherMatches(subjectType SubjectType, current any, raw string) (bool, error) {
	switch subjectType {
	case SubjectExec:
		var expected ExecAllowlistMatcher
		if err := json.Unmarshal([]byte(raw), &expected); err != nil {
			return false, err
		}
		if isEmptyExecAllowlistMatcher(expected) {
			return false, nil
		}
		actual, _ := current.(ExecAllowlistMatcher)
		if expected.ExecutablePath != "" && expected.ExecutablePath != actual.ExecutablePath {
			return false, nil
		}
		if expected.PathGlob != "" {
			matched, err := filepath.Match(expected.PathGlob, actual.ExecutablePath)
			if err != nil || !matched {
				return false, err
			}
		}
		if len(expected.Argv) > 0 && !slices.Equal(expected.Argv, actual.Argv) {
			return false, nil
		}
		if expected.WorkingDir != "" && expected.WorkingDir != actual.WorkingDir {
			return false, nil
		}
		if expected.WorkingDirPref != "" && !strings.HasPrefix(actual.WorkingDir, expected.WorkingDirPref) {
			return false, nil
		}
		if expected.ScriptHash != "" && expected.ScriptHash != actual.ScriptHash {
			return false, nil
		}
		return true, nil
	case SubjectSkillExec:
		var expected SkillAllowlistMatcher
		if err := json.Unmarshal([]byte(raw), &expected); err != nil {
			return false, err
		}
		if isEmptySkillAllowlistMatcher(expected) {
			return false, nil
		}
		actual, _ := current.(SkillAllowlistMatcher)
		if expected.SkillID != "" && expected.SkillID != actual.SkillID {
			return false, nil
		}
		if expected.Version != "" && expected.Version != actual.Version {
			return false, nil
		}
		if expected.Origin != "" && expected.Origin != actual.Origin {
			return false, nil
		}
		if expected.TrustState != "" && expected.TrustState != actual.TrustState {
			return false, nil
		}
		if expected.PlanHash != "" && expected.PlanHash != actual.PlanHash {
			return false, nil
		}
		if expected.ScriptHash != "" && expected.ScriptHash != actual.ScriptHash {
			return false, nil
		}
		if expected.EnvBindingHash != "" && expected.EnvBindingHash != actual.EnvBindingHash {
			return false, nil
		}
		if expected.TimeoutSeconds > 0 && expected.TimeoutSeconds != actual.TimeoutSeconds {
			return false, nil
		}
		return true, nil
	case SubjectRunnerPermission:
		var expected RunnerPermissionAllowlistMatcher
		if err := json.Unmarshal([]byte(raw), &expected); err != nil {
			return false, err
		}
		if isEmptyRunnerPermissionAllowlistMatcher(expected) {
			return false, nil
		}
		actual, _ := current.(RunnerPermissionAllowlistMatcher)
		if expected.RunnerID != "" && expected.RunnerID != actual.RunnerID {
			return false, nil
		}
		if expected.PermissionKind != "" && expected.PermissionKind != actual.PermissionKind {
			return false, nil
		}
		if expected.Access != "" && expected.Access != actual.Access {
			return false, nil
		}
		if expected.TargetPath != "" && expected.TargetPath != actual.TargetPath {
			return false, nil
		}
		if expected.PathPrefix != "" {
			if actual.TargetPath != expected.PathPrefix && !strings.HasPrefix(actual.TargetPath, expected.PathPrefix+string(filepath.Separator)) {
				return false, nil
			}
		}
		return true, nil
	default:
		return false, nil
	}
}

func ValidateAllowlistMatcher(domain string, matcher any) error {
	switch SubjectType(strings.TrimSpace(domain)) {
	case SubjectExec:
		candidate, ok := matcher.(ExecAllowlistMatcher)
		if !ok {
			return fmt.Errorf("exec allowlist matcher is invalid")
		}
		if isEmptyExecAllowlistMatcher(candidate) {
			return fmt.Errorf("exec allowlist matcher must include at least one executable, path, argv, working directory, or script constraint")
		}
		return nil
	case SubjectSkillExec:
		candidate, ok := matcher.(SkillAllowlistMatcher)
		if !ok {
			return fmt.Errorf("skill allowlist matcher is invalid")
		}
		if isEmptySkillAllowlistMatcher(candidate) {
			return fmt.Errorf("skill allowlist matcher must include at least one skill, version, origin, trust, plan, script, environment, or timeout constraint")
		}
		return nil
	case SubjectRunnerPermission:
		candidate, ok := matcher.(RunnerPermissionAllowlistMatcher)
		if !ok {
			return fmt.Errorf("runner permission allowlist matcher is invalid")
		}
		if isEmptyRunnerPermissionAllowlistMatcher(candidate) {
			return fmt.Errorf("runner permission allowlist matcher must include at least one runner, permission kind, access, or path constraint")
		}
		return nil
	default:
		return fmt.Errorf("unsupported allowlist domain")
	}
}

func isEmptyExecAllowlistMatcher(matcher ExecAllowlistMatcher) bool {
	return strings.TrimSpace(matcher.ExecutablePath) == "" &&
		strings.TrimSpace(matcher.PathGlob) == "" &&
		len(matcher.Argv) == 0 &&
		strings.TrimSpace(matcher.WorkingDir) == "" &&
		strings.TrimSpace(matcher.WorkingDirPref) == "" &&
		strings.TrimSpace(matcher.ScriptHash) == ""
}

func isEmptySkillAllowlistMatcher(matcher SkillAllowlistMatcher) bool {
	return strings.TrimSpace(matcher.SkillID) == "" &&
		strings.TrimSpace(matcher.Version) == "" &&
		strings.TrimSpace(matcher.Origin) == "" &&
		strings.TrimSpace(matcher.TrustState) == "" &&
		strings.TrimSpace(matcher.PlanHash) == "" &&
		strings.TrimSpace(matcher.ScriptHash) == "" &&
		strings.TrimSpace(matcher.EnvBindingHash) == "" &&
		matcher.TimeoutSeconds == 0
}

func isEmptyRunnerPermissionAllowlistMatcher(matcher RunnerPermissionAllowlistMatcher) bool {
	return strings.TrimSpace(matcher.RunnerID) == "" &&
		strings.TrimSpace(matcher.PermissionKind) == "" &&
		strings.TrimSpace(matcher.Access) == "" &&
		strings.TrimSpace(matcher.TargetPath) == "" &&
		strings.TrimSpace(matcher.PathPrefix) == ""
}
