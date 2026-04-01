package approval

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"or3-intern/internal/config"
	"or3-intern/internal/db"
	"or3-intern/internal/security"
)

type SubjectType string

const (
	SubjectExec         SubjectType = "exec"
	SubjectSkillExec    SubjectType = "skill_execution"
	SubjectSecretAccess SubjectType = "secret_access"
	SubjectMessageSend  SubjectType = "message_send"
	SubjectFileTransfer SubjectType = "file_transfer"

	RoleOperator      = "operator"
	RoleServiceClient = "service-client"
	RoleWebUI         = "web-ui"
	RoleNode          = "node"

	StatusPending   = "pending"
	StatusApproved  = "approved"
	StatusDenied    = "denied"
	StatusExpired   = "expired"
	StatusExchanged = "exchanged"
	StatusActive    = "active"
	StatusRevoked   = "revoked"
)

type Broker struct {
	DB      *db.DB
	Audit   *security.AuditLogger
	Config  config.ApprovalConfig
	HostID  string
	SignKey []byte
	Now     func() time.Time
}

type Decision struct {
	Allowed          bool
	RequiresApproval bool
	RequestID        int64
	SubjectHash      string
	Reason           string
}

type ExecEvaluation struct {
	ExecutablePath string
	Argv           []string
	WorkingDir     string
	EnvBindingHash string
	ScriptHash     string
	AgentID        string
	SessionID      string
	ToolName       string
	AccessProfile  string
	SandboxID      string
	ApprovalToken  string
}

type SkillEvaluation struct {
	SkillID        string
	Version        string
	Origin         string
	TrustState     string
	ScriptHash     string
	EnvBindingHash string
	TimeoutSeconds int
	AgentID        string
	SessionID      string
	ApprovalToken  string
}

type ExecSubject struct {
	Type            string   `json:"type"`
	ExecutionHostID string   `json:"execution_host_id"`
	SandboxID       string   `json:"sandbox_id,omitempty"`
	ExecutablePath  string   `json:"executable_path"`
	Argv            []string `json:"argv"`
	WorkingDir      string   `json:"working_dir"`
	EnvBindingHash  string   `json:"env_binding_hash"`
	ScriptHash      string   `json:"script_hash,omitempty"`
	RequestingAgent string   `json:"requesting_agent_id,omitempty"`
	SessionID       string   `json:"session_id,omitempty"`
	ToolName        string   `json:"tool_name"`
	AccessProfile   string   `json:"access_profile,omitempty"`
}

type SkillExecutionSubject struct {
	Type            string `json:"type"`
	SkillID         string `json:"skill_id"`
	Version         string `json:"version,omitempty"`
	Origin          string `json:"origin,omitempty"`
	TrustState      string `json:"trust_state,omitempty"`
	ScriptHash      string `json:"script_hash"`
	ExecutionHostID string `json:"execution_host_id"`
	EnvBindingHash  string `json:"env_binding_hash"`
	TimeoutSeconds  int    `json:"timeout_seconds"`
	RequestingAgent string `json:"requesting_agent_id,omitempty"`
	SessionID       string `json:"session_id,omitempty"`
}

type AllowlistScope struct {
	HostID  string `json:"host_id,omitempty"`
	Tool    string `json:"tool,omitempty"`
	Profile string `json:"profile,omitempty"`
	Agent   string `json:"agent,omitempty"`
}

type ExecAllowlistMatcher struct {
	ExecutablePath string   `json:"executable_path,omitempty"`
	PathGlob       string   `json:"path_glob,omitempty"`
	Argv           []string `json:"argv,omitempty"`
	WorkingDir     string   `json:"working_dir,omitempty"`
	WorkingDirPref string   `json:"working_dir_prefix,omitempty"`
	ScriptHash     string   `json:"script_hash,omitempty"`
}

type SkillAllowlistMatcher struct {
	SkillID        string `json:"skill_id,omitempty"`
	Version        string `json:"version,omitempty"`
	Origin         string `json:"origin,omitempty"`
	TrustState     string `json:"trust_state,omitempty"`
	ScriptHash     string `json:"script_hash,omitempty"`
	TimeoutSeconds int    `json:"timeout_seconds,omitempty"`
}

type ApprovalTokenClaims struct {
	TokenID       int64  `json:"tid"`
	RequestID     int64  `json:"rid"`
	SubjectHash   string `json:"sub"`
	ExecutionHost string `json:"host"`
	IssuedAt      int64  `json:"iat"`
	ExpiresAt     int64  `json:"exp"`
}

type IssuedApproval struct {
	Request     db.ApprovalRequestRecord
	Token       string
	AllowlistID int64
}

type PairingRequestInput struct {
	Role        string
	DisplayName string
	Origin      string
	Metadata    map[string]any
	DeviceID    string
}

type PairingExchangeInput struct {
	RequestID int64
	Code      string
}

func (b *Broker) now() time.Time {
	if b != nil && b.Now != nil {
		return b.Now().UTC()
	}
	return time.Now().UTC()
}

func (b *Broker) hostID() string {
	if b == nil {
		return ""
	}
	if strings.TrimSpace(b.HostID) != "" {
		return strings.TrimSpace(b.HostID)
	}
	return strings.TrimSpace(b.Config.HostID)
}

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
		ScriptHash:      strings.TrimSpace(req.ScriptHash),
		ExecutionHostID: b.hostID(),
		EnvBindingHash:  strings.TrimSpace(req.EnvBindingHash),
		TimeoutSeconds:  req.TimeoutSeconds,
		RequestingAgent: strings.TrimSpace(req.AgentID),
		SessionID:       strings.TrimSpace(req.SessionID),
	}
	return b.evaluate(ctx, SubjectSkillExec, subject, req.ApprovalToken,
		AllowlistScope{HostID: subject.ExecutionHostID, Tool: "run_skill_script", Agent: subject.RequestingAgent},
		SkillAllowlistMatcher{SkillID: subject.SkillID, Version: subject.Version, Origin: subject.Origin, TrustState: subject.TrustState, ScriptHash: subject.ScriptHash, TimeoutSeconds: subject.TimeoutSeconds},
	)
}

func (b *Broker) evaluate(ctx context.Context, subjectType SubjectType, subject any, approvalToken string, scope AllowlistScope, matcher any) (Decision, error) {
	mode := b.modeFor(subjectType)
	subjectJSON, subjectHash, err := CanonicalSubjectHash(subject)
	if err != nil {
		return Decision{}, err
	}
	if strings.TrimSpace(approvalToken) != "" {
		if err := b.VerifyApprovalToken(ctx, approvalToken, subjectHash, b.hostID()); err == nil {
			return Decision{Allowed: true, SubjectHash: subjectHash, Reason: "approved_token"}, nil
		}
	}
	switch mode {
	case config.ApprovalModeTrusted:
		_ = b.audit(ctx, "approval.trusted", map[string]any{"subject_hash": subjectHash, "host_id": b.hostID(), "type": string(subjectType), "outcome": "allowed"})
		return Decision{Allowed: true, SubjectHash: subjectHash, Reason: "trusted"}, nil
	case config.ApprovalModeDeny:
		_ = b.audit(ctx, "approval.blocked", map[string]any{"subject_hash": subjectHash, "host_id": b.hostID(), "type": string(subjectType), "outcome": "blocked", "reason": "deny"})
		return Decision{Allowed: false, SubjectHash: subjectHash, Reason: "approval denied by policy"}, nil
	}
	if len(b.SignKey) == 0 || b.DB == nil {
		_ = b.audit(ctx, "approval.blocked", map[string]any{"subject_hash": subjectHash, "host_id": b.hostID(), "type": string(subjectType), "outcome": "blocked", "reason": "broker_unavailable"})
		return Decision{Allowed: false, SubjectHash: subjectHash, Reason: "approval broker unavailable"}, nil
	}
	if mode == config.ApprovalModeAllowlist {
		matched, err := b.allowlistMatches(ctx, subjectType, scope, matcher)
		if err != nil {
			return Decision{}, err
		}
		if matched {
			_ = b.audit(ctx, "approval.allowlist_match", map[string]any{"subject_hash": subjectHash, "host_id": b.hostID(), "type": string(subjectType), "outcome": "allowed"})
			return Decision{Allowed: true, SubjectHash: subjectHash, Reason: "allowlist"}, nil
		}
	}
	nowMS := b.now().UnixMilli()
	existing, ok, err := b.DB.FindPendingApprovalRequest(ctx, string(subjectType), subjectHash, b.hostID(), nowMS)
	if err != nil {
		return Decision{}, err
	}
	if ok {
		return Decision{Allowed: false, RequiresApproval: true, RequestID: existing.ID, SubjectHash: subjectHash, Reason: "approval required"}, nil
	}
	req, err := b.DB.CreateApprovalRequest(ctx, db.ApprovalRequestRecord{Type: string(subjectType), SubjectHash: subjectHash, SubjectJSON: subjectJSON, RequesterAgentID: scope.Agent, RequesterSessionID: extractSessionID(subject), ExecutionHostID: b.hostID(), Status: StatusPending, PolicyMode: string(mode), RequestedAt: nowMS, ExpiresAt: nowMS + int64(b.Config.PendingTTLSeconds*1000)})
	if err != nil {
		return Decision{}, err
	}
	_ = b.audit(ctx, "approval.requested", map[string]any{"request_id": req.ID, "subject_hash": subjectHash, "host_id": b.hostID(), "type": string(subjectType), "policy_mode": string(mode), "outcome": "pending"})
	return Decision{Allowed: false, RequiresApproval: true, RequestID: req.ID, SubjectHash: subjectHash, Reason: "approval required"}, nil
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
		_ = b.DB.UpdateApprovalRequestResolution(ctx, requestID, StatusExpired, nowMS, actor, StatusExpired, "expired before approval")
		return IssuedApproval{}, fmt.Errorf("approval request expired")
	}
	if err := b.DB.UpdateApprovalRequestResolution(ctx, requestID, StatusApproved, nowMS, strings.TrimSpace(actor), resolutionKind(alwaysAllow), strings.TrimSpace(note)); err != nil {
		return IssuedApproval{}, err
	}
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
	_ = b.audit(ctx, "approval.resolved", map[string]any{"request_id": requestID, "subject_hash": req.SubjectHash, "host_id": req.ExecutionHostID, "outcome": "approved", "actor": actor, "allowlist_id": allowlistID})
	return IssuedApproval{Request: req, Token: token, AllowlistID: allowlistID}, nil
}

func (b *Broker) DenyRequest(ctx context.Context, requestID int64, actor string, note string) error {
	req, err := b.DB.GetApprovalRequest(ctx, requestID)
	if err != nil {
		return err
	}
	nowMS := b.now().UnixMilli()
	if err := b.DB.UpdateApprovalRequestResolution(ctx, requestID, StatusDenied, nowMS, strings.TrimSpace(actor), StatusDenied, strings.TrimSpace(note)); err != nil {
		return err
	}
	_ = b.audit(ctx, "approval.resolved", map[string]any{"request_id": requestID, "subject_hash": req.SubjectHash, "host_id": req.ExecutionHostID, "outcome": "denied", "actor": actor})
	return nil
}

func (b *Broker) VerifyApprovalToken(ctx context.Context, token string, subjectHash string, hostID string) error {
	claims, err := b.parseApprovalToken(token)
	if err != nil {
		return err
	}
	now := b.now().Unix()
	if claims.ExpiresAt < now {
		return fmt.Errorf("approval token expired")
	}
	if claims.ExecutionHost != strings.TrimSpace(hostID) {
		return fmt.Errorf("approval token host mismatch")
	}
	if claims.SubjectHash != strings.TrimSpace(subjectHash) {
		return fmt.Errorf("approval token subject mismatch")
	}
	record, err := b.DB.GetApprovalToken(ctx, claims.TokenID)
	if err != nil {
		return fmt.Errorf("approval token record not found")
	}
	if record.RevokedAt > 0 {
		return fmt.Errorf("approval token revoked")
	}
	if record.SubjectHash != claims.SubjectHash {
		return fmt.Errorf("approval token subject mismatch")
	}
	return nil
}

func (b *Broker) CreatePairingRequest(ctx context.Context, input PairingRequestInput) (db.PairingRequestRecord, string, error) {
	role := normalizeRole(input.Role)
	if role == "" {
		return db.PairingRequestRecord{}, "", fmt.Errorf("invalid pairing role")
	}
	code, err := randomDigits(6)
	if err != nil {
		return db.PairingRequestRecord{}, "", err
	}
	deviceID := strings.TrimSpace(input.DeviceID)
	if deviceID == "" {
		deviceID, err = randomHex(12)
		if err != nil {
			return db.PairingRequestRecord{}, "", err
		}
	}
	nowMS := b.now().UnixMilli()
	status := StatusPending
	approverID := ""
	approvedAt := int64(0)
	switch b.pairingMode() {
	case config.ApprovalModeDeny:
		_ = b.audit(ctx, "pairing.blocked", map[string]any{"device_id": deviceID, "role": role, "host_id": b.hostID(), "outcome": "blocked", "reason": "deny"})
		return db.PairingRequestRecord{}, "", fmt.Errorf("pairing denied by policy")
	case config.ApprovalModeTrusted:
		status = StatusApproved
		approverID = "policy:trusted"
		approvedAt = nowMS
	case config.ApprovalModeAllowlist:
		allowed, allowErr := b.pairingAllowlistMatches(ctx, deviceID, role)
		if allowErr != nil {
			return db.PairingRequestRecord{}, "", allowErr
		}
		if !allowed {
			_ = b.audit(ctx, "pairing.blocked", map[string]any{"device_id": deviceID, "role": role, "host_id": b.hostID(), "outcome": "blocked", "reason": "allowlist"})
			return db.PairingRequestRecord{}, "", fmt.Errorf("pairing denied by policy")
		}
		status = StatusApproved
		approverID = "policy:allowlist"
		approvedAt = nowMS
	}
	req, err := b.DB.CreatePairingRequest(ctx, db.PairingRequestRecord{DeviceID: deviceID, Role: role, DisplayName: strings.TrimSpace(input.DisplayName), Origin: strings.TrimSpace(input.Origin), PairingCodeHash: hashBytes(code), RequestedAt: nowMS, ExpiresAt: nowMS + int64(b.Config.PairingCodeTTLSeconds*1000), Status: status, ApproverID: approverID, ApprovedAt: approvedAt, Metadata: cloneMap(input.Metadata)})
	if err != nil {
		return db.PairingRequestRecord{}, "", err
	}
	_ = b.audit(ctx, "pairing.requested", map[string]any{"pairing_request_id": req.ID, "device_id": req.DeviceID, "role": req.Role, "host_id": b.hostID(), "outcome": req.Status})
	return req, code, nil
}

func (b *Broker) ApprovePairingRequest(ctx context.Context, id int64, actor string) (db.PairingRequestRecord, error) {
	req, err := b.DB.GetPairingRequest(ctx, id)
	if err != nil {
		return db.PairingRequestRecord{}, err
	}
	if req.Status != StatusPending {
		return db.PairingRequestRecord{}, fmt.Errorf("pairing request is not pending")
	}
	nowMS := b.now().UnixMilli()
	if req.ExpiresAt > 0 && req.ExpiresAt < nowMS {
		return db.PairingRequestRecord{}, fmt.Errorf("pairing request expired")
	}
	if err := b.DB.UpdatePairingRequestStatus(ctx, id, StatusApproved, strings.TrimSpace(actor), nowMS, 0, req.Metadata); err != nil {
		return db.PairingRequestRecord{}, err
	}
	updated, err := b.DB.GetPairingRequest(ctx, id)
	if err != nil {
		return db.PairingRequestRecord{}, err
	}
	_ = b.audit(ctx, "pairing.resolved", map[string]any{"pairing_request_id": id, "device_id": req.DeviceID, "outcome": "approved", "actor": actor, "host_id": b.hostID()})
	return updated, nil
}

func (b *Broker) DenyPairingRequest(ctx context.Context, id int64, actor string) error {
	req, err := b.DB.GetPairingRequest(ctx, id)
	if err != nil {
		return err
	}
	if req.Status != StatusPending {
		return fmt.Errorf("pairing request is not pending")
	}
	nowMS := b.now().UnixMilli()
	if err := b.DB.UpdatePairingRequestStatus(ctx, id, StatusDenied, strings.TrimSpace(actor), 0, nowMS, req.Metadata); err != nil {
		return err
	}
	_ = b.audit(ctx, "pairing.resolved", map[string]any{"pairing_request_id": id, "device_id": req.DeviceID, "outcome": "denied", "actor": actor, "host_id": b.hostID()})
	return nil
}

func (b *Broker) ExchangePairingCode(ctx context.Context, input PairingExchangeInput) (db.PairedDeviceRecord, string, error) {
	req, ok, err := b.DB.FindPairingRequestByCodeHash(ctx, input.RequestID, hashBytes(strings.TrimSpace(input.Code)))
	if err != nil {
		return db.PairedDeviceRecord{}, "", err
	}
	if !ok {
		return db.PairedDeviceRecord{}, "", fmt.Errorf("pairing request not found")
	}
	nowMS := b.now().UnixMilli()
	if req.ExpiresAt > 0 && req.ExpiresAt < nowMS {
		return db.PairedDeviceRecord{}, "", fmt.Errorf("pairing request expired")
	}
	if req.Status != StatusApproved {
		return db.PairedDeviceRecord{}, "", fmt.Errorf("pairing request is not approved")
	}
	claimed, err := b.DB.CompareAndSwapPairingRequestStatus(ctx, req.ID, StatusApproved, StatusExchanged)
	if err != nil {
		return db.PairedDeviceRecord{}, "", err
	}
	if !claimed {
		return db.PairedDeviceRecord{}, "", fmt.Errorf("pairing request is not approved")
	}
	device, token, err := b.RotateDeviceToken(ctx, req.DeviceID, req.Role, req.DisplayName, cloneMap(req.Metadata))
	if err != nil {
		_, _ = b.DB.CompareAndSwapPairingRequestStatus(ctx, req.ID, StatusExchanged, StatusApproved)
		return db.PairedDeviceRecord{}, "", err
	}
	_ = b.audit(ctx, "pairing.exchanged", map[string]any{"pairing_request_id": req.ID, "device_id": device.DeviceID, "role": device.Role, "host_id": b.hostID(), "outcome": "paired"})
	return device, token, nil
}

func (b *Broker) AuthenticateDeviceToken(ctx context.Context, rawToken string, allowedRoles ...string) (db.PairedDeviceRecord, error) {
	device, ok, err := b.DB.FindPairedDeviceByToken(ctx, rawToken)
	if err != nil {
		return db.PairedDeviceRecord{}, err
	}
	if !ok {
		return db.PairedDeviceRecord{}, fmt.Errorf("device token not found")
	}
	if device.Status != StatusActive || device.RevokedAt > 0 {
		return db.PairedDeviceRecord{}, fmt.Errorf("device token revoked")
	}
	if len(allowedRoles) > 0 && !slices.Contains(allowedRoles, device.Role) {
		return db.PairedDeviceRecord{}, fmt.Errorf("device role not allowed")
	}
	_ = b.DB.TouchPairedDevice(ctx, device.DeviceID, b.now().UnixMilli())
	return device, nil
}

func (b *Broker) RevokeDevice(ctx context.Context, deviceID string, actor string) error {
	device, err := b.DB.GetPairedDevice(ctx, deviceID)
	if err != nil {
		return err
	}
	device.Status = StatusRevoked
	device.RevokedAt = b.now().UnixMilli()
	device.LastSeenAt = device.RevokedAt
	if _, err := b.DB.UpsertPairedDevice(ctx, device); err != nil {
		return err
	}
	_ = b.audit(ctx, "device.revoked", map[string]any{"device_id": deviceID, "actor": actor, "host_id": b.hostID(), "outcome": "revoked"})
	return nil
}

func (b *Broker) RotateDeviceToken(ctx context.Context, deviceID, role, displayName string, metadata map[string]any) (db.PairedDeviceRecord, string, error) {
	rawToken, err := randomHex(24)
	if err != nil {
		return db.PairedDeviceRecord{}, "", err
	}
	nowMS := b.now().UnixMilli()
	device, err := b.DB.UpsertPairedDevice(ctx, db.PairedDeviceRecord{DeviceID: strings.TrimSpace(deviceID), Role: normalizeRole(role), DisplayName: strings.TrimSpace(displayName), TokenHash: hashBytes(rawToken), Status: StatusActive, CreatedAt: nowMS, LastSeenAt: nowMS, Metadata: cloneMap(metadata)})
	if err != nil {
		return db.PairedDeviceRecord{}, "", err
	}
	_ = b.audit(ctx, "device.rotated", map[string]any{"device_id": device.DeviceID, "role": device.Role, "host_id": b.hostID(), "outcome": "rotated"})
	return device, rawToken, nil
}

func (b *Broker) ListApprovalRequests(ctx context.Context, status string, limit int) ([]db.ApprovalRequestRecord, error) {
	return b.DB.ListApprovalRequests(ctx, status, limit)
}

func (b *Broker) ListPairingRequests(ctx context.Context, status string, limit int) ([]db.PairingRequestRecord, error) {
	return b.DB.ListPairingRequests(ctx, status, limit)
}

func (b *Broker) ListDevices(ctx context.Context, limit int) ([]db.PairedDeviceRecord, error) {
	return b.DB.ListPairedDevices(ctx, limit)
}

func (b *Broker) IsPairedChannelIdentity(ctx context.Context, channel, identity string) (bool, error) {
	if b == nil || b.DB == nil {
		return false, nil
	}
	channel = strings.ToLower(strings.TrimSpace(channel))
	identity = strings.TrimSpace(identity)
	if channel == "" || identity == "" {
		return false, nil
	}
	for _, deviceID := range []string{channel + ":" + identity, identity} {
		rec, err := b.DB.GetPairedDevice(ctx, deviceID)
		if err == nil {
			return rec.Status == StatusActive && rec.RevokedAt == 0, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return false, err
		}
	}
	items, err := b.DB.ListPairedDevices(ctx, 500)
	if err != nil {
		return false, err
	}
	for _, item := range items {
		if item.Status != StatusActive || item.RevokedAt > 0 {
			continue
		}
		if pairedMetadataMatches(item.Metadata, channel, identity) {
			return true, nil
		}
	}
	return false, nil
}

func (b *Broker) AddAllowlist(ctx context.Context, domain string, scope AllowlistScope, matcher any, actor string, expiresAt int64) (db.ApprovalAllowlistRecord, error) {
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

func CanonicalSubjectHash(subject any) (string, string, error) {
	payload, err := marshalCanonical(subject)
	if err != nil {
		return "", "", err
	}
	hash := sha256.Sum256([]byte(payload))
	return payload, hex.EncodeToString(hash[:]), nil
}

func marshalCanonical(value any) (string, error) {
	blob, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return string(blob), nil
}

func (b *Broker) allowlistMatches(ctx context.Context, subjectType SubjectType, scope AllowlistScope, matcher any) (bool, error) {
	records, err := b.DB.ListApprovalAllowlists(ctx, string(subjectType), 200)
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

func pairedMetadataMatches(metadata map[string]any, channel, identity string) bool {
	if len(metadata) == 0 {
		return false
	}
	compare := func(key, want string) bool {
		if want == "" {
			return false
		}
		value := strings.TrimSpace(fmt.Sprint(metadata[key]))
		return value != "" && strings.EqualFold(value, want)
	}
	if compare("channel", channel) && (compare("identity", identity) || compare("sender", identity) || compare("user_id", identity) || compare("chat_id", identity) || compare("from", identity)) {
		return true
	}
	if compare(channel+"_identity", identity) || compare(channel+"_user_id", identity) || compare(channel+"_chat_id", identity) || compare(channel+"_from", identity) {
		return true
	}
	return false
}

func allowlistMatcherMatches(subjectType SubjectType, current any, raw string) (bool, error) {
	switch subjectType {
	case SubjectExec:
		var expected ExecAllowlistMatcher
		if err := json.Unmarshal([]byte(raw), &expected); err != nil {
			return false, err
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
		if expected.ScriptHash != "" && expected.ScriptHash != actual.ScriptHash {
			return false, nil
		}
		if expected.TimeoutSeconds > 0 && expected.TimeoutSeconds != actual.TimeoutSeconds {
			return false, nil
		}
		return true, nil
	default:
		return false, nil
	}
}

func (b *Broker) createAllowlistFromRequest(ctx context.Context, req db.ApprovalRequestRecord, actor string) (int64, error) {
	switch SubjectType(req.Type) {
	case SubjectExec:
		var subject ExecSubject
		if err := json.Unmarshal([]byte(req.SubjectJSON), &subject); err != nil {
			return 0, err
		}
		rec, err := b.AddAllowlist(ctx, req.Type, AllowlistScope{HostID: subject.ExecutionHostID, Tool: subject.ToolName, Profile: subject.AccessProfile, Agent: subject.RequestingAgent}, ExecAllowlistMatcher{ExecutablePath: subject.ExecutablePath, Argv: subject.Argv, WorkingDir: subject.WorkingDir, ScriptHash: subject.ScriptHash}, actor, 0)
		if err != nil {
			return 0, err
		}
		return rec.ID, nil
	case SubjectSkillExec:
		var subject SkillExecutionSubject
		if err := json.Unmarshal([]byte(req.SubjectJSON), &subject); err != nil {
			return 0, err
		}
		rec, err := b.AddAllowlist(ctx, req.Type, AllowlistScope{HostID: subject.ExecutionHostID, Tool: "run_skill_script", Agent: subject.RequestingAgent}, SkillAllowlistMatcher{SkillID: subject.SkillID, Version: subject.Version, Origin: subject.Origin, TrustState: subject.TrustState, ScriptHash: subject.ScriptHash, TimeoutSeconds: subject.TimeoutSeconds}, actor, 0)
		if err != nil {
			return 0, err
		}
		return rec.ID, nil
	default:
		return 0, nil
	}
}

func (b *Broker) issueTokenForRequest(ctx context.Context, req db.ApprovalRequestRecord, actor string) (string, error) {
	now := b.now()
	record, err := b.DB.CreateApprovalToken(ctx, db.ApprovalTokenRecord{ApprovalRequestID: req.ID, SubjectHash: req.SubjectHash, IssuedAt: now.UnixMilli(), ExpiresAt: now.Add(time.Duration(b.Config.ApprovalTokenTTLSeconds) * time.Second).UnixMilli(), Issuer: actor})
	if err != nil {
		return "", err
	}
	claims := ApprovalTokenClaims{TokenID: record.ID, RequestID: req.ID, SubjectHash: req.SubjectHash, ExecutionHost: req.ExecutionHostID, IssuedAt: now.Unix(), ExpiresAt: now.Add(time.Duration(b.Config.ApprovalTokenTTLSeconds) * time.Second).Unix()}
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	payloadPart := base64.RawURLEncoding.EncodeToString(payload)
	signature := hex.EncodeToString(signToken(b.SignKey, payloadPart))
	_ = b.audit(ctx, "approval.token_issued", map[string]any{"request_id": req.ID, "token_id": record.ID, "subject_hash": req.SubjectHash, "host_id": req.ExecutionHostID, "actor": actor, "outcome": "issued"})
	return payloadPart + "." + signature, nil
}

func (b *Broker) parseApprovalToken(token string) (ApprovalTokenClaims, error) {
	payloadPart, signaturePart, ok := strings.Cut(strings.TrimSpace(token), ".")
	if !ok || payloadPart == "" || signaturePart == "" {
		return ApprovalTokenClaims{}, fmt.Errorf("invalid approval token format")
	}
	signature, err := hex.DecodeString(signaturePart)
	if err != nil {
		return ApprovalTokenClaims{}, fmt.Errorf("invalid approval token signature")
	}
	expected := signToken(b.SignKey, payloadPart)
	if !hmac.Equal(signature, expected) {
		return ApprovalTokenClaims{}, fmt.Errorf("invalid approval token signature")
	}
	payload, err := base64.RawURLEncoding.DecodeString(payloadPart)
	if err != nil {
		return ApprovalTokenClaims{}, fmt.Errorf("invalid approval token payload")
	}
	var claims ApprovalTokenClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return ApprovalTokenClaims{}, fmt.Errorf("invalid approval token payload")
	}
	return claims, nil
}

func signToken(key []byte, payload string) []byte {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(payload))
	return mac.Sum(nil)
}

func (b *Broker) modeFor(subjectType SubjectType) config.ApprovalMode {
	switch subjectType {
	case SubjectExec:
		return b.Config.Exec.Mode
	case SubjectSkillExec:
		return b.Config.SkillExecution.Mode
	case SubjectSecretAccess:
		return b.Config.SecretAccess.Mode
	case SubjectMessageSend:
		return b.Config.MessageSend.Mode
	default:
		return config.ApprovalModeDeny
	}
}

func (b *Broker) pairingMode() config.ApprovalMode {
	if b == nil {
		return config.ApprovalModeDeny
	}
	return b.Config.Pairing.Mode
}

func (b *Broker) pairingAllowlistMatches(ctx context.Context, deviceID, role string) (bool, error) {
	deviceID = strings.TrimSpace(deviceID)
	if deviceID == "" || b == nil || b.DB == nil {
		return false, nil
	}
	device, err := b.DB.GetPairedDevice(ctx, deviceID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	return device.Status == StatusActive && device.RevokedAt == 0 && device.Role == role, nil
}

func (b *Broker) audit(ctx context.Context, eventType string, payload map[string]any) error {
	if b == nil || b.Audit == nil {
		return nil
	}
	return b.Audit.Record(ctx, eventType, "", "approval", payload)
}

func extractSessionID(subject any) string {
	switch value := subject.(type) {
	case ExecSubject:
		return value.SessionID
	case SkillExecutionSubject:
		return value.SessionID
	default:
		return ""
	}
}

func resolutionKind(alwaysAllow bool) string {
	if alwaysAllow {
		return "approve_and_allowlist"
	}
	return "approve_once"
}

func hashBytes(raw string) []byte {
	sum := sha256.Sum256([]byte(strings.TrimSpace(raw)))
	return sum[:]
}

func randomHex(size int) (string, error) {
	if size <= 0 {
		return "", nil
	}
	buf := make([]byte, size)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func randomDigits(length int) (string, error) {
	if length <= 0 {
		length = 6
	}
	max := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(length)), nil)
	n, err := rand.Int(rand.Reader, max)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%0*d", length, n.Int64()), nil
}

func normalizeRole(role string) string {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case RoleOperator:
		return RoleOperator
	case RoleServiceClient:
		return RoleServiceClient
	case RoleWebUI:
		return RoleWebUI
	case RoleNode:
		return RoleNode
	default:
		return ""
	}
}

func cloneMap(in map[string]any) map[string]any {
	if len(in) == 0 {
		return map[string]any{}
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}
