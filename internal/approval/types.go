package approval

import (
	"context"
	"time"

	"or3-intern/internal/config"
	"or3-intern/internal/db"
	"or3-intern/internal/security"
)

type SubjectType string

const (
	SubjectExec             SubjectType = "exec"
	SubjectSkillExec        SubjectType = "skill_execution"
	SubjectRunnerPermission SubjectType = "runner_permission"
	SubjectSecretAccess     SubjectType = "secret_access"
	SubjectMessageSend      SubjectType = "message_send"
	SubjectFileTransfer     SubjectType = "file_transfer"
	SubjectToolQuota        SubjectType = "tool_quota"

	RoleViewer        = "viewer"
	RoleOperator      = "operator"
	RoleServiceClient = "service-client"
	RoleWebUI         = "web-ui"
	RoleNode          = "node"
	RoleAdmin         = "admin"

	StatusPending   = "pending"
	StatusApproved  = "approved"
	StatusDenied    = "denied"
	StatusCanceled  = "canceled"
	StatusExpired   = "expired"
	StatusExchanged = "exchanged"
	StatusActive    = "active"
	StatusRevoked   = "revoked"

	ResolutionKindApproveOnce         = "approve_once"
	ResolutionKindApproveAndAllowlist = "approve_and_allowlist"
)

const defaultPageSize = 200

type ModeratorRisk string

const (
	RiskLow     ModeratorRisk = "low"
	RiskMedium  ModeratorRisk = "medium"
	RiskHigh    ModeratorRisk = "high"
	RiskExtreme ModeratorRisk = "extreme"
)

type ModeratorAction string

const (
	ModeratorApprove  ModeratorAction = "approve"
	ModeratorEscalate ModeratorAction = "escalate"
	ModeratorDeny     ModeratorAction = "deny"
)

type ModeratorReviewInput struct {
	RequestID      int64
	SubjectType    SubjectType
	SubjectHash    string
	SubjectPreview string
	SubjectFacts   map[string]any
	PolicyMode     config.ApprovalMode
	AccessProfile  string
	Requester      RequesterContext
	Redactions     redactionStats
}

type ModeratorReviewResult struct {
	Risk        ModeratorRisk   `json:"risk"`
	Action      ModeratorAction `json:"action"`
	Reason      string          `json:"reason"`
	Alternative string          `json:"alternative,omitempty"`
	Confidence  float64         `json:"confidence,omitempty"`
}

type Moderator interface {
	ReviewApproval(ctx context.Context, input ModeratorReviewInput) (ModeratorReviewResult, error)
	ModelIdentity() string
	PolicyHash() string
}

type Broker struct {
	DB        *db.DB
	Audit     *security.AuditLogger
	Config    config.ApprovalConfig
	HostID    string
	SignKey   []byte
	Now       func() time.Time
	Moderator Moderator
	Workspace string
}

type Decision struct {
	Allowed          bool
	RequiresApproval bool
	RequestID        int64
	SubjectHash      string
	Reason           string
}

type RequesterContext struct {
	Channel         string         `json:"channel,omitempty"`
	SessionKey      string         `json:"session_key,omitempty"`
	From            string         `json:"from,omitempty"`
	ReplyTarget     string         `json:"reply_target,omitempty"`
	ReplyMeta       map[string]any `json:"reply_meta,omitempty"`
	SourceMessageID string         `json:"source_message_id,omitempty"`
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
	ToolName       string
	PlanID         string
	PlanHash       string
	ScriptHash     string
	EnvBindingHash string
	TimeoutSeconds int
	AgentID        string
	SessionID      string
	ApprovalToken  string
}

type SecretAccessEvaluation struct {
	SecretName    string
	Operation     string
	AgentID       string
	SessionID     string
	ApprovalToken string
}

type RunnerPermissionEvaluation struct {
	RunnerID       string
	PermissionKind string
	Access         string
	TargetPath     string
	AgentID        string
	SessionID      string
	ApprovalToken  string
}

type ToolQuotaEvaluation struct {
	Scope         string
	LimitName     string
	ToolName      string
	Current       int
	Limit         int
	AgentID       string
	SessionID     string
	ApprovalToken string
}

type MessageSendEvaluation struct {
	Channel       string
	To            string
	Text          string
	MediaCount    int
	ReplyInThread bool
	AgentID       string
	SessionID     string
	ApprovalToken string
}

type MessageSendSubject struct {
	Type            string `json:"type"`
	ExecutionHostID string `json:"execution_host_id"`
	Channel         string `json:"channel"`
	To              string `json:"to"`
	TextLength      int    `json:"text_length"`
	MediaCount      int    `json:"media_count"`
	ReplyInThread   bool   `json:"reply_in_thread"`
	RequestingAgent string `json:"requesting_agent_id,omitempty"`
	SessionID       string `json:"session_id,omitempty"`
}

func (s MessageSendSubject) GetSessionID() string { return s.SessionID }

type FileTransferSubject struct {
	Type            string `json:"type"`
	ExecutionHostID string `json:"execution_host_id"`
	Path            string `json:"path"`
	Destination     string `json:"destination,omitempty"`
	RequestingAgent string `json:"requesting_agent_id,omitempty"`
	SessionID       string `json:"session_id,omitempty"`
}

func (s FileTransferSubject) GetSessionID() string { return s.SessionID }

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

func (s ExecSubject) GetSessionID() string { return s.SessionID }

type SkillExecutionSubject struct {
	Type            string `json:"type"`
	SkillID         string `json:"skill_id"`
	Version         string `json:"version,omitempty"`
	Origin          string `json:"origin,omitempty"`
	TrustState      string `json:"trust_state,omitempty"`
	ToolName        string `json:"tool_name,omitempty"`
	PlanID          string `json:"plan_id,omitempty"`
	PlanHash        string `json:"plan_hash,omitempty"`
	ScriptHash      string `json:"script_hash"`
	ExecutionHostID string `json:"execution_host_id"`
	EnvBindingHash  string `json:"env_binding_hash"`
	TimeoutSeconds  int    `json:"timeout_seconds"`
	RequestingAgent string `json:"requesting_agent_id,omitempty"`
	SessionID       string `json:"session_id,omitempty"`
}

func (s SkillExecutionSubject) GetSessionID() string { return s.SessionID }

type SecretAccessSubject struct {
	Type            string `json:"type"`
	ExecutionHostID string `json:"execution_host_id"`
	SecretName      string `json:"secret_name"`
	Operation       string `json:"operation"`
	RequestingAgent string `json:"requesting_agent_id,omitempty"`
	SessionID       string `json:"session_id,omitempty"`
}

func (s SecretAccessSubject) GetSessionID() string { return s.SessionID }

type RunnerPermissionSubject struct {
	Type            string `json:"type"`
	ExecutionHostID string `json:"execution_host_id"`
	RunnerID        string `json:"runner_id"`
	PermissionKind  string `json:"permission_kind"`
	Access          string `json:"access"`
	TargetPath      string `json:"target_path"`
	RequestingAgent string `json:"requesting_agent_id,omitempty"`
	SessionID       string `json:"session_id,omitempty"`
}

func (s RunnerPermissionSubject) GetSessionID() string { return s.SessionID }

type ToolQuotaSubject struct {
	Type            string `json:"type"`
	ExecutionHostID string `json:"execution_host_id"`
	Scope           string `json:"scope"`
	LimitName       string `json:"limit_name"`
	ToolName        string `json:"tool_name"`
	Current         int    `json:"current"`
	Limit           int    `json:"limit"`
	RequestingAgent string `json:"requesting_agent_id,omitempty"`
	SessionID       string `json:"session_id,omitempty"`
}

func (s ToolQuotaSubject) GetSessionID() string { return s.SessionID }

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
	PlanHash       string `json:"plan_hash,omitempty"`
	ScriptHash     string `json:"script_hash,omitempty"`
	EnvBindingHash string `json:"env_binding_hash,omitempty"`
	TimeoutSeconds int    `json:"timeout_seconds,omitempty"`
}

type RunnerPermissionAllowlistMatcher struct {
	RunnerID       string `json:"runner_id,omitempty"`
	PermissionKind string `json:"permission_kind,omitempty"`
	Access         string `json:"access,omitempty"`
	TargetPath     string `json:"target_path,omitempty"`
	PathPrefix     string `json:"path_prefix,omitempty"`
}

type ApprovalTokenClaims struct {
	TokenID       int64  `json:"tid"`
	RequestID     int64  `json:"rid"`
	SubjectHash   string `json:"sub"`
	ExecutionHost string `json:"host"`
	IssuedAt      int64  `json:"iat"`
	ExpiresAt     int64  `json:"exp"`
}

type SubjectHash struct {
	JSON string
	Hash string
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
