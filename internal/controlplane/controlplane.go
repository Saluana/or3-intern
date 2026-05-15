package controlplane

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"or3-intern/internal/agent"
	"or3-intern/internal/approval"
	"or3-intern/internal/config"
	"or3-intern/internal/db"
	intdoctor "or3-intern/internal/doctor"
	"or3-intern/internal/mcp"
	"or3-intern/internal/memory"
	"or3-intern/internal/providers"
	"or3-intern/internal/scope"
	"or3-intern/internal/security"
)

var (
	ErrApprovalBrokerUnavailable = errors.New("approval broker unavailable")
	ErrJobRegistryUnavailable    = errors.New("job registry unavailable")
	ErrJobNotFound               = errors.New("job not found")
	ErrDatabaseUnavailable       = errors.New("database unavailable")
	ErrProviderUnavailable       = errors.New("provider unavailable")
	ErrAuditUnavailable          = errors.New("audit logger unavailable")
)

const (
	defaultListLimit = 100
	maxListLimit     = 200
)

var processStartedAt = time.Now().UTC()

type Service struct {
	Config          config.Config
	Runtime         *agent.Runtime
	Broker          *approval.Broker
	Jobs            *agent.JobRegistry
	SubagentManager *agent.SubagentManager
	DB              *db.DB
	Provider        *providers.Client
	Audit           *security.AuditLogger
	MCPStatus       MCPStatusProvider
}

// MCPStatusProvider exposes runtime MCP connection state to control-plane reports.
type MCPStatusProvider interface {
	ServerStatus() map[string]mcp.ServerStatus
}

type ApprovalFilter struct {
	Status string
	Type   string
	Limit  int
}

type CapabilitiesProfileSummary struct {
	Name           string   `json:"name,omitempty"`
	MaxCapability  string   `json:"maxCapability,omitempty"`
	AllowedTools   []string `json:"allowedTools,omitempty"`
	AllowedHosts   []string `json:"allowedHosts,omitempty"`
	WritablePaths  []string `json:"writablePaths,omitempty"`
	AllowSubagents bool     `json:"allowSubagents"`
}

type CapabilitiesIngressSummary struct {
	Name          string                      `json:"name"`
	Enabled       bool                        `json:"enabled"`
	InboundPolicy string                      `json:"inboundPolicy,omitempty"`
	Profile       *CapabilitiesProfileSummary `json:"effectiveProfile,omitempty"`
}

type CapabilitiesMCPServerInfo struct {
	Name      string `json:"name"`
	Transport string `json:"transport"`
	ToolCount int    `json:"toolCount"`
	Connected bool   `json:"connected"`
}

type CapabilitiesReport struct {
	RuntimeProfile     string                       `json:"runtimeProfile"`
	Hosted             bool                         `json:"hosted"`
	HostID             string                       `json:"hostId"`
	ApprovalBroker     map[string]any               `json:"approvalBroker"`
	Approvals          map[string]string            `json:"approvals"`
	SubagentsEnabled   bool                         `json:"subagentsEnabled"`
	SkillExecEnabled   bool                         `json:"skillExecEnabled"`
	ExecAvailable      bool                         `json:"execAvailable"`
	ShellModeAvailable bool                         `json:"shellModeAvailable"`
	SandboxEnabled     bool                         `json:"sandboxEnabled"`
	SandboxRequired    bool                         `json:"sandboxRequired"`
	EnabledMCPServers  []CapabilitiesMCPServerInfo  `json:"enabledMcpServers,omitempty"`
	MCPServers         []CapabilitiesMCPServerInfo  `json:"mcpServers,omitempty"`
	NetworkPolicy      config.NetworkPolicyConfig   `json:"networkPolicy"`
	Channels           []CapabilitiesIngressSummary `json:"channels,omitempty"`
	Triggers           []CapabilitiesIngressSummary `json:"triggers,omitempty"`
	HeartbeatEnabled   bool                         `json:"heartbeatEnabled"`
	CronEnabled        bool                         `json:"cronEnabled"`
}

type HealthReport struct {
	Status                  string `json:"status"`
	RuntimeAvailable        bool   `json:"runtimeAvailable"`
	JobRegistryAvailable    bool   `json:"jobRegistryAvailable"`
	SubagentManagerEnabled  bool   `json:"subagentManagerEnabled"`
	ApprovalBrokerAvailable bool   `json:"approvalBrokerAvailable"`
	ProcessID               int    `json:"processId"`
	StartedAt               string `json:"startedAt"`
}

type ReadinessReport struct {
	Status   string              `json:"status"`
	Ready    bool                `json:"ready"`
	Summary  intdoctor.Summary   `json:"summary"`
	Findings []intdoctor.Finding `json:"findings,omitempty"`
}

type EmbeddingStatusReport struct {
	Status                  string `json:"status"`
	MemoryVectorDims        int    `json:"memoryVectorDims"`
	StoredEmbedFingerprint  string `json:"storedEmbedFingerprint,omitempty"`
	CurrentEmbedFingerprint string `json:"currentEmbedFingerprint,omitempty"`
	DocIndexEnabled         bool   `json:"docIndexEnabled"`
	DocRootsConfigured      bool   `json:"docRootsConfigured"`
}

type EmbeddingRebuildResult struct {
	Status             string   `json:"status"`
	Target             string   `json:"target"`
	Fingerprint        string   `json:"fingerprint,omitempty"`
	MemoryNotesRebuilt int      `json:"memoryNotesRebuilt,omitempty"`
	DocsRebuilt        bool     `json:"docsRebuilt"`
	Skipped            []string `json:"skipped,omitempty"`
}

type AuditStatusReport struct {
	Status        string `json:"status"`
	Enabled       bool   `json:"enabled"`
	Available     bool   `json:"available"`
	Strict        bool   `json:"strict"`
	VerifyOnStart bool   `json:"verifyOnStart"`
	EventCount    int64  `json:"eventCount"`
	LastEventID   int64  `json:"lastEventId,omitempty"`
	LastEventType string `json:"lastEventType,omitempty"`
	LastActor     string `json:"lastActor,omitempty"`
	LastEventAt   int64  `json:"lastEventAt,omitempty"`
}

type AuditVerifyResult struct {
	Verified   bool  `json:"verified"`
	EventCount int64 `json:"eventCount"`
}

type ScopeLinkInput struct {
	SessionKey string
	ScopeKey   string
	Meta       map[string]any
}

type ScopeLinkResult struct {
	SessionKey string `json:"sessionKey"`
	ScopeKey   string `json:"scopeKey"`
}

func New(cfg config.Config, rt *agent.Runtime, broker *approval.Broker, jobs *agent.JobRegistry, subagentManager *agent.SubagentManager) *Service {
	svc := &Service{
		Config:          cfg,
		Runtime:         rt,
		Broker:          broker,
		Jobs:            jobs,
		SubagentManager: subagentManager,
	}
	if rt != nil {
		svc.DB = rt.DB
		svc.Provider = rt.Provider
		svc.Audit = rt.Audit
	}
	return svc
}

func NewLocal(cfg config.Config, database *db.DB, provider *providers.Client, audit *security.AuditLogger, broker *approval.Broker) *Service {
	return &Service{
		Config:   cfg,
		DB:       database,
		Provider: provider,
		Audit:    audit,
		Broker:   broker,
	}
}

func (s *Service) GetHealth() HealthReport {
	report := HealthReport{
		Status:                  "ok",
		RuntimeAvailable:        s != nil && s.Runtime != nil,
		JobRegistryAvailable:    s != nil && s.Jobs != nil,
		SubagentManagerEnabled:  s != nil && s.SubagentManager != nil,
		ApprovalBrokerAvailable: s != nil && s.Broker != nil,
		ProcessID:               os.Getpid(),
		StartedAt:               processStartedAt.Format(time.RFC3339Nano),
	}
	if !report.RuntimeAvailable || !report.JobRegistryAvailable {
		report.Status = "degraded"
	}
	return report
}

func (s *Service) GetReadiness() ReadinessReport {
	cfg := config.Config{}
	if s != nil {
		cfg = s.Config
	}
	report := intdoctor.Evaluate(cfg, intdoctor.Options{Mode: intdoctor.ModeStartupService})
	return ReadinessReport{
		Status:   report.Summary.Status,
		Ready:    report.Summary.ErrorCount == 0 && report.Summary.BlockCount == 0,
		Summary:  report.Summary,
		Findings: append([]intdoctor.Finding{}, report.Findings...),
	}
}

func (s *Service) GetCapabilities(channelFilter, triggerFilter string) CapabilitiesReport {
	cfg := config.Config{}
	var broker *approval.Broker
	if s != nil {
		cfg = s.Config
		broker = s.Broker
	}
	var mcpStatus MCPStatusProvider
	if s != nil {
		mcpStatus = s.MCPStatus
	}
	return CollectCapabilitiesReportWithMCPStatus(cfg, broker, mcpStatus, channelFilter, triggerFilter)
}

func (s *Service) ListApprovalRequests(ctx context.Context, filter ApprovalFilter) ([]db.ApprovalRequestRecord, error) {
	broker, err := s.requireBroker()
	if err != nil {
		return nil, err
	}
	return broker.ListApprovalRequestsFiltered(ctx, strings.TrimSpace(filter.Status), strings.TrimSpace(filter.Type), normalizeLimit(filter.Limit))
}

func (s *Service) GetApproval(ctx context.Context, requestID int64) (db.ApprovalRequestRecord, error) {
	broker, err := s.requireBroker()
	if err != nil {
		return db.ApprovalRequestRecord{}, err
	}
	return broker.DB.GetApprovalRequest(ctx, requestID)
}

func (s *Service) ApproveApproval(ctx context.Context, requestID int64, actor string, allowlist bool, note string) (approval.IssuedApproval, error) {
	broker, err := s.requireBroker()
	if err != nil {
		return approval.IssuedApproval{}, err
	}
	return broker.ApproveRequest(ctx, requestID, actor, allowlist, note)
}

func (s *Service) DenyApproval(ctx context.Context, requestID int64, actor, note string) error {
	broker, err := s.requireBroker()
	if err != nil {
		return err
	}
	return broker.DenyRequest(ctx, requestID, actor, note)
}

func (s *Service) CancelApproval(ctx context.Context, requestID int64, actor, note string) error {
	broker, err := s.requireBroker()
	if err != nil {
		return err
	}
	return broker.CancelRequest(ctx, requestID, actor, note)
}

func (s *Service) ExpireApprovals(ctx context.Context, actor string) (int64, error) {
	broker, err := s.requireBroker()
	if err != nil {
		return 0, err
	}
	return broker.ExpirePendingRequests(ctx, actor)
}

func (s *Service) ListAllowlists(ctx context.Context, domain string, limit int) ([]db.ApprovalAllowlistRecord, error) {
	broker, err := s.requireBroker()
	if err != nil {
		return nil, err
	}
	return broker.ListAllowlists(ctx, strings.TrimSpace(domain), normalizeLimit(limit))
}

func (s *Service) AddAllowlist(ctx context.Context, domain string, scope approval.AllowlistScope, matcher any, actor string, expiresAt int64) (db.ApprovalAllowlistRecord, error) {
	broker, err := s.requireBroker()
	if err != nil {
		return db.ApprovalAllowlistRecord{}, err
	}
	return broker.AddAllowlist(ctx, strings.TrimSpace(domain), scope, matcher, actor, expiresAt)
}

func (s *Service) RemoveAllowlist(ctx context.Context, id int64, actor string) error {
	broker, err := s.requireBroker()
	if err != nil {
		return err
	}
	return broker.RemoveAllowlist(ctx, id, actor)
}

func (s *Service) ListDevices(ctx context.Context, limit int) ([]db.PairedDeviceRecord, error) {
	broker, err := s.requireBroker()
	if err != nil {
		return nil, err
	}
	return broker.ListDevices(ctx, normalizeLimit(limit))
}

func (s *Service) RotateDevice(ctx context.Context, deviceID string) (db.PairedDeviceRecord, string, error) {
	broker, err := s.requireBroker()
	if err != nil {
		return db.PairedDeviceRecord{}, "", err
	}
	return broker.RotatePairedDeviceToken(ctx, strings.TrimSpace(deviceID))
}

func (s *Service) RevokeDevice(ctx context.Context, deviceID, actor string) error {
	broker, err := s.requireBroker()
	if err != nil {
		return err
	}
	return broker.RevokeDevice(ctx, strings.TrimSpace(deviceID), actor)
}

func (s *Service) CreatePairingRequest(ctx context.Context, input approval.PairingRequestInput) (db.PairingRequestRecord, string, error) {
	broker, err := s.requireBroker()
	if err != nil {
		return db.PairingRequestRecord{}, "", err
	}
	return broker.CreatePairingRequest(ctx, input)
}

func (s *Service) ListPairingRequests(ctx context.Context, status string, limit int) ([]db.PairingRequestRecord, error) {
	broker, err := s.requireBroker()
	if err != nil {
		return nil, err
	}
	return broker.ListPairingRequests(ctx, strings.TrimSpace(status), normalizeLimit(limit))
}

func (s *Service) ApprovePairingRequest(ctx context.Context, requestID int64, actor string) (db.PairingRequestRecord, error) {
	broker, err := s.requireBroker()
	if err != nil {
		return db.PairingRequestRecord{}, err
	}
	return broker.ApprovePairingRequest(ctx, requestID, actor)
}

func (s *Service) ApprovePairingRequestByCode(ctx context.Context, code string, actor string) (db.PairingRequestRecord, error) {
	broker, err := s.requireBroker()
	if err != nil {
		return db.PairingRequestRecord{}, err
	}
	return broker.ApprovePairingRequestByCode(ctx, code, actor)
}

func (s *Service) DenyPairingRequest(ctx context.Context, requestID int64, actor string) error {
	broker, err := s.requireBroker()
	if err != nil {
		return err
	}
	return broker.DenyPairingRequest(ctx, requestID, actor)
}

func (s *Service) ExchangePairingCode(ctx context.Context, input approval.PairingExchangeInput) (db.PairedDeviceRecord, string, error) {
	broker, err := s.requireBroker()
	if err != nil {
		return db.PairedDeviceRecord{}, "", err
	}
	return broker.ExchangePairingCode(ctx, input)
}

func (s *Service) GetJob(jobID string) (agent.JobSnapshot, error) {
	if s == nil || s.Jobs == nil {
		return agent.JobSnapshot{}, ErrJobRegistryUnavailable
	}
	snapshot, ok := s.Jobs.Snapshot(strings.TrimSpace(jobID))
	if !ok {
		return agent.JobSnapshot{}, ErrJobNotFound
	}
	return snapshot, nil
}

func (s *Service) GetEmbeddingStatus(ctx context.Context) (EmbeddingStatusReport, error) {
	database, err := s.requireDB()
	if err != nil {
		return EmbeddingStatusReport{}, err
	}
	dims, err := database.MemoryVectorDims(ctx)
	if err != nil {
		return EmbeddingStatusReport{}, err
	}
	storedFingerprint, err := database.MemoryVectorFingerprint(ctx)
	if err != nil {
		return EmbeddingStatusReport{}, err
	}
	currentFingerprint := providers.EmbeddingFingerprint(s.Config.Provider.APIBase, s.Config.Provider.EmbedModel, s.Config.Provider.EmbedDimensions)
	status := "ok"
	if strings.TrimSpace(storedFingerprint) == "" && dims > 0 {
		status = "legacy-unknown"
	} else if strings.TrimSpace(storedFingerprint) != "" && strings.TrimSpace(storedFingerprint) != strings.TrimSpace(currentFingerprint) {
		status = "mismatch"
	}
	return EmbeddingStatusReport{
		Status:                  status,
		MemoryVectorDims:        dims,
		StoredEmbedFingerprint:  storedFingerprint,
		CurrentEmbedFingerprint: currentFingerprint,
		DocIndexEnabled:         s.Config.DocIndex.Enabled,
		DocRootsConfigured:      len(s.Config.DocIndex.Roots) > 0,
	}, nil
}

func (s *Service) RebuildEmbeddings(ctx context.Context, target string) (EmbeddingRebuildResult, error) {
	database, err := s.requireDB()
	if err != nil {
		return EmbeddingRebuildResult{}, err
	}
	provider, err := s.requireProvider()
	if err != nil {
		return EmbeddingRebuildResult{}, err
	}
	target = strings.ToLower(strings.TrimSpace(target))
	if target == "" {
		target = "memory"
	}
	result := EmbeddingRebuildResult{
		Status:      "ok",
		Target:      target,
		Fingerprint: providers.EmbeddingFingerprint(s.Config.Provider.APIBase, s.Config.Provider.EmbedModel, s.Config.Provider.EmbedDimensions),
	}
	if strings.TrimSpace(s.Config.Provider.EmbedModel) == "" {
		return EmbeddingRebuildResult{}, fmt.Errorf("provider.embedModel is not configured")
	}
	switch target {
	case "memory":
		count, skipped, err := rebuildMemoryEmbeddings(ctx, database, provider, s.Config.Provider.EmbedModel, result.Fingerprint)
		if err != nil {
			return EmbeddingRebuildResult{}, err
		}
		result.MemoryNotesRebuilt = count
		result.Skipped = append(result.Skipped, skipped...)
	case "docs":
		docsRebuilt, skipped, err := rebuildDocEmbeddings(ctx, s.Config, database, provider, result.Fingerprint)
		if err != nil {
			return EmbeddingRebuildResult{}, err
		}
		result.DocsRebuilt = docsRebuilt
		result.Skipped = append(result.Skipped, skipped...)
	case "all":
		count, skipped, err := rebuildMemoryEmbeddings(ctx, database, provider, s.Config.Provider.EmbedModel, result.Fingerprint)
		if err != nil {
			return EmbeddingRebuildResult{}, err
		}
		result.MemoryNotesRebuilt = count
		result.Skipped = append(result.Skipped, skipped...)
		docsRebuilt, skipped, err := rebuildDocEmbeddings(ctx, s.Config, database, provider, result.Fingerprint)
		if err != nil {
			return EmbeddingRebuildResult{}, err
		}
		result.DocsRebuilt = docsRebuilt
		result.Skipped = append(result.Skipped, skipped...)
	default:
		return EmbeddingRebuildResult{}, fmt.Errorf("unsupported embeddings rebuild target")
	}
	return result, nil
}

func (s *Service) GetAuditStatus(ctx context.Context) (AuditStatusReport, error) {
	report := AuditStatusReport{
		Enabled:       s.Config.Security.Audit.Enabled,
		Strict:        s.Config.Security.Audit.Strict,
		VerifyOnStart: s.Config.Security.Audit.VerifyOnStart,
	}
	audit, ok := s.auditLogger()
	if !ok {
		if report.Enabled {
			report.Status = "unavailable"
		} else {
			report.Status = "disabled"
		}
		return report, nil
	}
	report.Available = true
	report.Strict = audit.Strict
	report.Status = "ok"
	count, err := audit.DB.CountAuditEvents(ctx)
	if err != nil {
		return AuditStatusReport{}, err
	}
	report.EventCount = count
	latest, found, err := audit.DB.LatestAuditEventSummary(ctx)
	if err != nil {
		return AuditStatusReport{}, err
	}
	if found {
		report.LastEventID = latest.ID
		report.LastEventType = latest.EventType
		report.LastActor = latest.Actor
		report.LastEventAt = latest.CreatedAt
	}
	return report, nil
}

func (s *Service) VerifyAudit(ctx context.Context) (AuditVerifyResult, error) {
	audit, ok := s.auditLogger()
	if !ok {
		return AuditVerifyResult{}, ErrAuditUnavailable
	}
	if err := audit.Verify(ctx); err != nil {
		return AuditVerifyResult{}, err
	}
	count, err := audit.DB.CountAuditEvents(ctx)
	if err != nil {
		return AuditVerifyResult{}, err
	}
	return AuditVerifyResult{Verified: true, EventCount: count}, nil
}

func (s *Service) LinkSessionScope(ctx context.Context, input ScopeLinkInput) (ScopeLinkResult, error) {
	database, err := s.requireDB()
	if err != nil {
		return ScopeLinkResult{}, err
	}
	if err := database.LinkSession(ctx, strings.TrimSpace(input.SessionKey), strings.TrimSpace(input.ScopeKey), input.Meta); err != nil {
		return ScopeLinkResult{}, err
	}
	resolved, err := database.ResolveScopeKey(ctx, strings.TrimSpace(input.SessionKey))
	if err != nil {
		return ScopeLinkResult{}, err
	}
	return ScopeLinkResult{SessionKey: strings.TrimSpace(input.SessionKey), ScopeKey: resolved}, nil
}

func (s *Service) ResolveScopeKey(ctx context.Context, sessionKey string) (string, error) {
	database, err := s.requireDB()
	if err != nil {
		return "", err
	}
	return database.ResolveScopeKey(ctx, strings.TrimSpace(sessionKey))
}

func (s *Service) ListScopeSessions(ctx context.Context, scopeKey string) ([]string, error) {
	database, err := s.requireDB()
	if err != nil {
		return nil, err
	}
	return database.ListScopeSessions(ctx, strings.TrimSpace(scopeKey))
}

func (s *Service) requireBroker() (*approval.Broker, error) {
	if s == nil || s.Broker == nil {
		return nil, ErrApprovalBrokerUnavailable
	}
	return s.Broker, nil
}

func (s *Service) requireDB() (*db.DB, error) {
	if s == nil {
		return nil, ErrDatabaseUnavailable
	}
	if s.DB != nil {
		return s.DB, nil
	}
	if s.Runtime != nil && s.Runtime.DB != nil {
		return s.Runtime.DB, nil
	}
	return nil, ErrDatabaseUnavailable
}

func (s *Service) requireProvider() (*providers.Client, error) {
	if s == nil {
		return nil, ErrProviderUnavailable
	}
	if s.Provider != nil {
		return s.Provider, nil
	}
	if s.Runtime != nil && s.Runtime.Provider != nil {
		return s.Runtime.Provider, nil
	}
	return nil, ErrProviderUnavailable
}

func (s *Service) auditLogger() (*security.AuditLogger, bool) {
	if s == nil {
		return nil, false
	}
	if s.Audit != nil && s.Audit.DB != nil && len(s.Audit.Key) > 0 {
		return s.Audit, true
	}
	if s.Runtime != nil && s.Runtime.Audit != nil && s.Runtime.Audit.DB != nil && len(s.Runtime.Audit.Key) > 0 {
		return s.Runtime.Audit, true
	}
	return nil, false
}

func rebuildMemoryEmbeddings(ctx context.Context, database *db.DB, provider *providers.Client, model, fingerprint string) (int, []string, error) {
	rows, err := database.ListMemoryNotesForReembed(ctx)
	if err != nil {
		return 0, nil, err
	}
	if len(rows) == 0 {
		return 0, []string{"no_memory_notes"}, nil
	}
	wantDims := 0
	for _, row := range rows {
		vec, err := provider.Embed(ctx, model, strings.TrimSpace(row.Text))
		if err != nil {
			return 0, nil, fmt.Errorf("rebuild memory note %d: %w", row.ID, err)
		}
		if wantDims == 0 {
			wantDims = len(vec)
		} else if len(vec) != wantDims {
			return 0, nil, fmt.Errorf("embedding dimension changed during rebuild: have %d want %d", len(vec), wantDims)
		}
		if err := database.ReplaceMemoryNoteEmbedding(ctx, row.ID, memory.PackFloat32(vec), fingerprint); err != nil {
			return 0, nil, fmt.Errorf("persist memory note %d: %w", row.ID, err)
		}
	}
	if wantDims > 0 {
		if err := database.RebuildMemoryVecIndexWithProfile(ctx, wantDims, fingerprint); err != nil {
			return 0, nil, err
		}
	}
	return len(rows), nil, nil
}

func rebuildDocEmbeddings(ctx context.Context, cfg config.Config, database *db.DB, provider *providers.Client, fingerprint string) (bool, []string, error) {
	if !cfg.DocIndex.Enabled {
		return false, []string{"doc_index_disabled"}, nil
	}
	if len(cfg.DocIndex.Roots) == 0 {
		return false, []string{"doc_index_no_roots"}, nil
	}
	indexer := &memory.DocIndexer{
		DB:               database,
		Provider:         provider,
		EmbedModel:       cfg.Provider.EmbedModel,
		EmbedFingerprint: fingerprint,
		Config: memory.DocIndexConfig{
			Roots:          cfg.DocIndex.Roots,
			MaxFiles:       cfg.DocIndex.MaxFiles,
			MaxFileBytes:   cfg.DocIndex.MaxFileBytes,
			MaxChunks:      cfg.DocIndex.MaxChunks,
			EmbedMaxBytes:  cfg.DocIndex.EmbedMaxBytes,
			RefreshSeconds: cfg.DocIndex.RefreshSeconds,
			RetrieveLimit:  cfg.DocIndex.RetrieveLimit,
		},
	}
	if err := indexer.SyncRoots(ctx, scope.GlobalMemoryScope); err != nil {
		return false, nil, err
	}
	return true, nil, nil
}

func CollectCapabilitiesReport(cfg config.Config, broker *approval.Broker, channelFilter, triggerFilter string) CapabilitiesReport {
	return CollectCapabilitiesReportWithMCPStatus(cfg, broker, nil, channelFilter, triggerFilter)
}

func CollectCapabilitiesReportWithMCPStatus(cfg config.Config, broker *approval.Broker, mcpStatus MCPStatusProvider, channelFilter, triggerFilter string) CapabilitiesReport {
	spec := config.ProfileSpec(cfg.RuntimeProfile)
	report := CapabilitiesReport{
		RuntimeProfile:     string(cfg.RuntimeProfile),
		Hosted:             spec.Hosted,
		HostID:             cfg.Security.Approvals.HostID,
		Approvals:          ApprovalModes(cfg),
		SubagentsEnabled:   cfg.Subagents.Enabled,
		SkillExecEnabled:   cfg.Skills.EnableExec && cfg.Hardening.PrivilegedTools && !spec.ForbidPrivilegedTools,
		ExecAvailable:      cfg.Hardening.GuardedTools && (!spec.RequireSandboxForExec || cfg.Hardening.Sandbox.Enabled),
		ShellModeAvailable: cfg.Hardening.GuardedTools && cfg.Hardening.PrivilegedTools && cfg.Hardening.EnableExecShell && !spec.ForbidExecShell && !spec.ForbidPrivilegedTools && (!spec.RequireSandboxForExec || cfg.Hardening.Sandbox.Enabled),
		SandboxEnabled:     cfg.Hardening.Sandbox.Enabled,
		SandboxRequired:    spec.RequireSandboxForExec,
		NetworkPolicy:      cfg.Security.Network,
		HeartbeatEnabled:   cfg.Heartbeat.Enabled,
		CronEnabled:        cfg.Cron.Enabled,
		ApprovalBroker: map[string]any{
			"enabled":       cfg.Security.Approvals.Enabled,
			"required":      approvalBrokerRequired(cfg),
			"available":     broker != nil,
			"canIssueToken": broker != nil && len(broker.SignKey) > 0,
		},
	}
	report.MCPServers = mcpServerCapabilities(cfg, mcpStatus)
	report.EnabledMCPServers = enabledMCPServers(report.MCPServers, cfg)
	report.Channels = collectChannelCapabilities(cfg, channelFilter)
	report.Triggers = collectTriggerCapabilities(cfg, triggerFilter)
	return report
}

func ApprovalModes(cfg config.Config) map[string]string {
	return map[string]string{
		"pairing":        string(cfg.Security.Approvals.Pairing.Mode),
		"exec":           string(cfg.Security.Approvals.Exec.Mode),
		"skillExecution": string(cfg.Security.Approvals.SkillExecution.Mode),
		"secretAccess":   string(cfg.Security.Approvals.SecretAccess.Mode),
		"messageSend":    string(cfg.Security.Approvals.MessageSend.Mode),
	}
}

func collectChannelCapabilities(cfg config.Config, filter string) []CapabilitiesIngressSummary {
	items := []CapabilitiesIngressSummary{
		{
			Name:          "telegram",
			Enabled:       cfg.Channels.Telegram.Enabled,
			InboundPolicy: config.EffectiveInboundPolicy(cfg.Channels.Telegram.InboundPolicy, cfg.Channels.Telegram.OpenAccess, hasNonEmpty(cfg.Channels.Telegram.AllowedChatIDs)),
			Profile:       effectiveProfileSummary(cfg, cfg.Security.Profiles.Channels["telegram"]),
		},
		{
			Name:          "slack",
			Enabled:       cfg.Channels.Slack.Enabled,
			InboundPolicy: config.EffectiveInboundPolicy(cfg.Channels.Slack.InboundPolicy, cfg.Channels.Slack.OpenAccess, hasNonEmpty(cfg.Channels.Slack.AllowedUserIDs)),
			Profile:       effectiveProfileSummary(cfg, cfg.Security.Profiles.Channels["slack"]),
		},
		{
			Name:          "discord",
			Enabled:       cfg.Channels.Discord.Enabled,
			InboundPolicy: config.EffectiveInboundPolicy(cfg.Channels.Discord.InboundPolicy, cfg.Channels.Discord.OpenAccess, hasNonEmpty(cfg.Channels.Discord.AllowedUserIDs)),
			Profile:       effectiveProfileSummary(cfg, cfg.Security.Profiles.Channels["discord"]),
		},
		{
			Name:          "whatsapp",
			Enabled:       cfg.Channels.WhatsApp.Enabled,
			InboundPolicy: config.EffectiveInboundPolicy(cfg.Channels.WhatsApp.InboundPolicy, cfg.Channels.WhatsApp.OpenAccess, hasNonEmpty(cfg.Channels.WhatsApp.AllowedFrom)),
			Profile:       effectiveProfileSummary(cfg, cfg.Security.Profiles.Channels["whatsapp"]),
		},
		{
			Name:          "email",
			Enabled:       cfg.Channels.Email.Enabled,
			InboundPolicy: config.EffectiveInboundPolicy(cfg.Channels.Email.InboundPolicy, cfg.Channels.Email.OpenAccess, hasNonEmpty(cfg.Channels.Email.AllowedSenders)),
			Profile:       effectiveProfileSummary(cfg, cfg.Security.Profiles.Channels["email"]),
		},
	}
	return filterIngress(items, filter)
}

func collectTriggerCapabilities(cfg config.Config, filter string) []CapabilitiesIngressSummary {
	items := []CapabilitiesIngressSummary{
		{
			Name:    "webhook",
			Enabled: cfg.Triggers.Webhook.Enabled,
			Profile: effectiveProfileSummary(cfg, cfg.Security.Profiles.Triggers["webhook"]),
		},
		{
			Name:    "filewatch",
			Enabled: cfg.Triggers.FileWatch.Enabled,
			Profile: effectiveProfileSummary(cfg, firstNonEmptyString(
				cfg.Security.Profiles.Triggers["file_change"],
				cfg.Security.Profiles.Triggers["file_watch"],
				cfg.Security.Profiles.Triggers["filewatch"],
			)),
		},
	}
	return filterIngress(items, filter)
}

func filterIngress(items []CapabilitiesIngressSummary, filter string) []CapabilitiesIngressSummary {
	filter = strings.ToLower(strings.TrimSpace(filter))
	if filter == "" {
		return items
	}
	out := make([]CapabilitiesIngressSummary, 0, 1)
	for _, item := range items {
		if item.Name == filter {
			out = append(out, item)
		}
	}
	return out
}

func effectiveProfileSummary(cfg config.Config, name string) *CapabilitiesProfileSummary {
	name = strings.TrimSpace(name)
	if !cfg.Security.Profiles.Enabled && name == "" {
		return nil
	}
	if name == "" {
		name = strings.TrimSpace(cfg.Security.Profiles.Default)
	}
	if name == "" {
		return nil
	}
	profile, ok := cfg.Security.Profiles.Profiles[name]
	if !ok {
		return &CapabilitiesProfileSummary{Name: name}
	}
	allowedTools := append([]string{}, profile.AllowedTools...)
	sort.Strings(allowedTools)
	allowedHosts := append([]string{}, profile.AllowedHosts...)
	sort.Strings(allowedHosts)
	writablePaths := append([]string{}, profile.WritablePaths...)
	sort.Strings(writablePaths)
	return &CapabilitiesProfileSummary{
		Name:           name,
		MaxCapability:  strings.TrimSpace(profile.MaxCapability),
		AllowedTools:   allowedTools,
		AllowedHosts:   allowedHosts,
		WritablePaths:  writablePaths,
		AllowSubagents: profile.AllowSubagents,
	}
}

func mcpServerCapabilities(cfg config.Config, provider MCPStatusProvider) []CapabilitiesMCPServerInfo {
	statuses := map[string]mcp.ServerStatus{}
	if provider != nil {
		statuses = provider.ServerStatus()
	}
	out := make([]CapabilitiesMCPServerInfo, 0, len(cfg.Tools.MCPServers))
	for name, server := range cfg.Tools.MCPServers {
		status := statuses[name]
		out = append(out, CapabilitiesMCPServerInfo{
			Name:      name,
			Transport: strings.TrimSpace(server.Transport),
			ToolCount: status.ToolCount,
			Connected: status.Connected,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out
}

func enabledMCPServers(items []CapabilitiesMCPServerInfo, cfg config.Config) []CapabilitiesMCPServerInfo {
	out := make([]CapabilitiesMCPServerInfo, 0, len(items))
	for _, item := range items {
		if cfg.Tools.MCPServers[item.Name].Enabled {
			out = append(out, item)
		}
	}
	return out
}

func hasNonEmpty(values []string) bool {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return true
		}
	}
	return false
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func normalizeLimit(limit int) int {
	if limit <= 0 || limit > maxListLimit {
		return defaultListLimit
	}
	return limit
}

func approvalBrokerRequired(cfg config.Config) bool {
	report := intdoctor.Evaluate(cfg, intdoctor.Options{Mode: intdoctor.ModeAdvisory})
	for _, finding := range report.Findings {
		if finding.ID == "approvals.key_missing" || finding.ID == "approvals.public_service_without_key" {
			return true
		}
	}
	for _, mode := range []config.ApprovalMode{
		cfg.Security.Approvals.Pairing.Mode,
		cfg.Security.Approvals.Exec.Mode,
		cfg.Security.Approvals.SkillExecution.Mode,
		cfg.Security.Approvals.SecretAccess.Mode,
		cfg.Security.Approvals.MessageSend.Mode,
	} {
		switch mode {
		case config.ApprovalModeAsk, config.ApprovalModeAllowlist:
			return true
		}
	}
	return false
}

func BuildJobResponse(snapshot agent.JobSnapshot) map[string]any {
	response := map[string]any{
		"job_id": snapshot.ID,
		"kind":   snapshot.Kind,
		"status": snapshot.Status,
	}
	for i := len(snapshot.Events) - 1; i >= 0; i-- {
		event := snapshot.Events[i]
		switch event.Type {
		case "completion":
			for key, value := range event.Data {
				response[key] = value
			}
			return response
		case "error":
			response["error"] = event.Data["message"]
			return response
		}
	}
	return response
}

func BuildJobSnapshotResponse(snapshot agent.JobSnapshot) map[string]any {
	response := BuildJobResponse(snapshot)
	response["created_at"] = snapshot.CreatedAt
	response["updated_at"] = snapshot.UpdatedAt
	response["events"] = snapshot.Events
	return response
}

// BuildSubagentJobResponse converts a persisted subagent_jobs row into a
// sanitized JSON map for the agents API. It deliberately omits raw metadata
// JSON, approval tokens, and any internal scratch fields.
func BuildSubagentJobResponse(job db.SubagentJob) map[string]any {
	out := map[string]any{
		"job_id":             job.ID,
		"kind":               "subagent",
		"parent_session_key": job.ParentSessionKey,
		"child_session_key":  job.ChildSessionKey,
		"task":               job.Task,
		"status":             job.Status,
		"requested_at":       formatSubagentTime(job.RequestedAt),
		"updated_at":         formatSubagentTime(latestSubagentTimestamp(job)),
	}
	if preview := strings.TrimSpace(job.ResultPreview); preview != "" {
		out["result_preview"] = preview
	}
	if artifact := strings.TrimSpace(job.ArtifactID); artifact != "" {
		out["artifact_id"] = artifact
	}
	if errText := strings.TrimSpace(job.ErrorText); errText != "" {
		out["error"] = errText
	}
	if job.StartedAt > 0 {
		out["started_at"] = formatSubagentTime(job.StartedAt)
	}
	if job.FinishedAt > 0 {
		out["finished_at"] = formatSubagentTime(job.FinishedAt)
	}
	if job.Attempts > 0 {
		out["attempts"] = job.Attempts
	}
	return out
}

// BuildSubagentJobListResponse renders a list of persisted subagent jobs.
func BuildSubagentJobListResponse(jobs []db.SubagentJob) map[string]any {
	items := make([]map[string]any, 0, len(jobs))
	for _, job := range jobs {
		items = append(items, BuildSubagentJobResponse(job))
	}
	return map[string]any{"items": items}
}

func latestSubagentTimestamp(job db.SubagentJob) int64 {
	latest := job.RequestedAt
	if job.StartedAt > latest {
		latest = job.StartedAt
	}
	if job.FinishedAt > latest {
		latest = job.FinishedAt
	}
	return latest
}

func formatSubagentTime(ms int64) string {
	if ms <= 0 {
		return ""
	}
	return time.UnixMilli(ms).UTC().Format(time.RFC3339)
}

func DescribeUnavailable(err error) error {
	switch {
	case errors.Is(err, ErrApprovalBrokerUnavailable):
		return fmt.Errorf("approval broker unavailable")
	case errors.Is(err, ErrJobRegistryUnavailable):
		return fmt.Errorf("job registry unavailable")
	case errors.Is(err, ErrDatabaseUnavailable):
		return fmt.Errorf("database unavailable")
	case errors.Is(err, ErrProviderUnavailable):
		return fmt.Errorf("provider unavailable")
	case errors.Is(err, ErrAuditUnavailable):
		return fmt.Errorf("audit logger unavailable")
	default:
		return err
	}
}

// BuildAgentCLIRunResponse converts a persisted agent_cli_runs row into a
// sanitized JSON map for the agents API.
func BuildAgentCLIRunResponse(run db.AgentCLIRun) map[string]any {
	out := map[string]any{
		"job_id":             run.JobID,
		"run_id":             run.ID,
		"kind":               "agent_cli:" + run.RunnerID,
		"runner_id":          run.RunnerID,
		"parent_session_key": run.ParentSessionKey,
		"task":               run.Task,
		"mode":               run.Mode,
		"isolation":          run.Isolation,
		"status":             run.Status,
		"requested_at":       formatAgentCLITime(run.RequestedAt),
		"updated_at":         formatAgentCLITime(latestAgentCLITimestamp(run)),
	}
	if preview := strings.TrimSpace(run.StdoutPreview); preview != "" {
		out["output_preview"] = preview
	}
	if preview := strings.TrimSpace(run.FinalTextPreview); preview != "" {
		out["final_text_preview"] = preview
	}
	if errPreview := strings.TrimSpace(run.StderrPreview); errPreview != "" {
		out["error_preview"] = errPreview
	}
	if errMsg := strings.TrimSpace(run.ErrorMessage); errMsg != "" {
		out["error"] = errMsg
	}
	if run.StartedAt > 0 {
		out["started_at"] = formatAgentCLITime(run.StartedAt)
	}
	if run.CompletedAt > 0 {
		out["completed_at"] = formatAgentCLITime(run.CompletedAt)
	}
	if run.TimeoutSeconds > 0 {
		out["timeout_seconds"] = run.TimeoutSeconds
	}
	if run.ExitCode.Valid {
		out["exit_code"] = run.ExitCode.Int64
	}
	if run.Attempts > 0 {
		out["attempts"] = run.Attempts
	}
	if model := strings.TrimSpace(run.Model); model != "" {
		out["model"] = model
	}
	return out
}

// BuildAgentCLIRunListResponse renders a list of persisted agent CLI runs.
func BuildAgentCLIRunListResponse(runs []db.AgentCLIRun) map[string]any {
	items := make([]map[string]any, 0, len(runs))
	for _, run := range runs {
		items = append(items, BuildAgentCLIRunResponse(run))
	}
	return map[string]any{"items": items}
}

// BuildAgentCLIEventListResponse renders a list of persisted agent CLI events.
func BuildAgentCLIEventListResponse(events []db.AgentCLIEvent) map[string]any {
	items := make([]map[string]any, 0, len(events))
	for _, e := range events {
		item := map[string]any{
			"seq":    e.Seq,
			"ts":     e.TS,
			"type":   e.Type,
			"stream": e.Stream,
			"chunk":  e.Chunk,
		}
		if e.PayloadJSON != "" {
			item["payload"] = json.RawMessage(e.PayloadJSON)
		}
		items = append(items, item)
	}
	return map[string]any{"events": items}
}

func latestAgentCLITimestamp(run db.AgentCLIRun) int64 {
	latest := run.RequestedAt
	if run.StartedAt > latest {
		latest = run.StartedAt
	}
	if run.CompletedAt > latest {
		latest = run.CompletedAt
	}
	return latest
}

func formatAgentCLITime(ms int64) string {
	if ms <= 0 {
		return ""
	}
	return time.UnixMilli(ms).UTC().Format(time.RFC3339)
}
