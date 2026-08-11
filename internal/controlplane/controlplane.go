package controlplane

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"or3-intern/internal/approval"
	"or3-intern/internal/config"
	"or3-intern/internal/db"
	intdoctor "or3-intern/internal/doctor"
	"or3-intern/internal/jobs"
	"or3-intern/internal/memory"
	"or3-intern/internal/providers"
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
	runtimeMu sync.RWMutex
	Config    config.Config
	Broker    *approval.Broker
	Jobs      *jobs.Registry
	DB        *db.DB
	Provider  *providers.Client
	Audit     *security.AuditLogger
}

type ApprovalFilter struct {
	Status string
	Type   string
	Limit  int
}

type CapabilitiesProfileSummary struct {
	Name          string   `json:"name,omitempty"`
	MaxCapability string   `json:"maxCapability,omitempty"`
	AllowedHosts  []string `json:"allowedHosts,omitempty"`
	WritablePaths []string `json:"writablePaths,omitempty"`
}

type CapabilitiesIngressSummary struct {
	Name          string                      `json:"name"`
	Enabled       bool                        `json:"enabled"`
	InboundPolicy string                      `json:"inboundPolicy,omitempty"`
	Profile       *CapabilitiesProfileSummary `json:"effectiveProfile,omitempty"`
}

type CapabilitiesReport struct {
	RuntimeProfile     string                       `json:"runtimeProfile"`
	Hosted             bool                         `json:"hosted"`
	HostID             string                       `json:"hostId"`
	ExecutionModel     string                       `json:"executionModel"`
	DefaultRunner      string                       `json:"defaultRunner"`
	RunnerMode         string                       `json:"runnerMode"`
	RunnerIsolation    string                       `json:"runnerIsolation"`
	TerminalAvailable  bool                         `json:"terminalAvailable"`
	ApprovalBroker     map[string]any               `json:"approvalBroker"`
	Approvals          map[string]string            `json:"approvals"`
	SkillExecEnabled   bool                         `json:"skillExecEnabled"`
	ExecAvailable      bool                         `json:"execAvailable"`
	ShellModeAvailable bool                         `json:"shellModeAvailable"`
	SandboxEnabled     bool                         `json:"sandboxEnabled"`
	SandboxRequired    bool                         `json:"sandboxRequired"`
	NetworkPolicy      config.NetworkPolicyConfig   `json:"networkPolicy"`
	Channels           []CapabilitiesIngressSummary `json:"channels,omitempty"`
	Triggers           []CapabilitiesIngressSummary `json:"triggers,omitempty"`
	HeartbeatEnabled   bool                         `json:"heartbeatEnabled"`
	CronEnabled        bool                         `json:"cronEnabled"`
}

type HealthReport struct {
	Status                  string            `json:"status"`
	RuntimeAvailable        bool              `json:"runtimeAvailable"`
	JobRegistryAvailable    bool              `json:"jobRegistryAvailable"`
	ApprovalBrokerAvailable bool              `json:"approvalBrokerAvailable"`
	ProcessID               int               `json:"processId"`
	StartedAt               string            `json:"startedAt"`
	ChannelStatuses         map[string]string `json:"channelStatuses,omitempty"`
}

type ReadinessReport struct {
	Status   string              `json:"status"`
	Ready    bool                `json:"ready"`
	Summary  intdoctor.Summary   `json:"summary"`
	Findings []intdoctor.Finding `json:"findings,omitempty"`
}

type EmbeddingStatusReport struct {
	Status                   string `json:"status"`
	MemoryVectorDims         int    `json:"memoryVectorDims"`
	StoredEmbedFingerprint   string `json:"storedEmbedFingerprint,omitempty"`
	CurrentEmbedFingerprint  string `json:"currentEmbedFingerprint,omitempty"`
	NoteCount                int    `json:"noteCount"`
	EmbeddedNoteCount        int    `json:"embeddedNoteCount"`
	VectorRowCount           int    `json:"vectorRowCount"`
	MissingVectorCount       int    `json:"missingVectorCount"`
	FingerprintMismatchCount int    `json:"fingerprintMismatchCount"`
	DirtyVectorCount         int    `json:"dirtyVectorCount"`
	ActiveDocCount           int    `json:"activeDocCount"`
	InactiveDocCount         int    `json:"inactiveDocCount"`
	LastDocSyncAt            int64  `json:"lastDocSyncAt,omitempty"`
	DocSyncPartial           bool   `json:"docSyncPartial"`
	DocSyncWarning           string `json:"docSyncWarning,omitempty"`
	LastVectorIndexError     string `json:"lastVectorIndexError,omitempty"`
	LastDocRetrievalError    string `json:"lastDocRetrievalError,omitempty"`
	SearchMode               string `json:"searchMode,omitempty"`
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
	Actor      string
}

type ScopeLinkResult struct {
	SessionKey string `json:"sessionKey"`
	ScopeKey   string `json:"scopeKey"`
}

// New builds a control-plane service from explicit runtime dependencies.
func New(cfg config.Config, database *db.DB, provider *providers.Client, audit *security.AuditLogger, broker *approval.Broker, jobRegistry *jobs.Registry) *Service {
	return &Service{
		Config:   config.Clone(cfg),
		DB:       database,
		Provider: provider,
		Audit:    audit,
		Broker:   broker,
		Jobs:     jobRegistry,
	}
}

func NewLocal(cfg config.Config, database *db.DB, provider *providers.Client, audit *security.AuditLogger, broker *approval.Broker) *Service {
	return &Service{
		Config:   config.Clone(cfg),
		DB:       database,
		Provider: provider,
		Audit:    audit,
		Broker:   broker,
	}
}

// SetRuntimeConfig publishes the configuration-dependent control-plane
// dependencies as one snapshot. Database, audit, approval, and job
// dependencies are process-lifetime values and remain unchanged.
func (s *Service) SetRuntimeConfig(cfg config.Config, provider *providers.Client) {
	if s == nil {
		return
	}
	s.runtimeMu.Lock()
	s.Config = config.Clone(cfg)
	s.Provider = provider
	s.runtimeMu.Unlock()
}

func (s *Service) runtimeSnapshot() (config.Config, *providers.Client) {
	if s == nil {
		return config.Config{}, nil
	}
	s.runtimeMu.RLock()
	cfg := config.Clone(s.Config)
	provider := s.Provider
	s.runtimeMu.RUnlock()
	return cfg, provider
}

func (s *Service) configSnapshot() config.Config {
	cfg, _ := s.runtimeSnapshot()
	return cfg
}

func (s *Service) GetHealth() HealthReport {
	report := HealthReport{
		Status:                  "ok",
		RuntimeAvailable:        s != nil && s.DB != nil,
		JobRegistryAvailable:    s != nil && s.Jobs != nil,
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
	cfg := s.configSnapshot()
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
		cfg = s.configSnapshot()
		broker = s.Broker
	}
	return CollectCapabilitiesReport(cfg, broker, channelFilter, triggerFilter)
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

func (s *Service) GetJob(jobID string) (jobs.Snapshot, error) {
	if s == nil || s.Jobs == nil {
		return jobs.Snapshot{}, ErrJobRegistryUnavailable
	}
	snapshot, ok := s.Jobs.Snapshot(strings.TrimSpace(jobID))
	if !ok {
		return jobs.Snapshot{}, ErrJobNotFound
	}
	return snapshot, nil
}

func (s *Service) GetEmbeddingStatus(ctx context.Context) (EmbeddingStatusReport, error) {
	cfg := s.configSnapshot()
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
	currentFingerprint := providers.EmbeddingFingerprint(cfg.Provider.APIBase, cfg.Provider.EmbedModel, cfg.Provider.EmbedDimensions)
	health, err := database.CollectMemoryEmbeddingHealth(ctx, currentFingerprint)
	if err != nil {
		return EmbeddingStatusReport{}, err
	}
	docSync := memory.LatestDocSyncState()
	status := deriveEmbeddingStatus(dims, storedFingerprint, currentFingerprint, health, strings.TrimSpace(cfg.Provider.EmbedModel))
	searchMode := "fts"
	if strings.TrimSpace(cfg.Provider.EmbedModel) != "" {
		searchMode = "hybrid"
	}
	lastDocSync := health.LastDocSyncAt
	if docSync.LastSyncAtMS > lastDocSync {
		lastDocSync = docSync.LastSyncAtMS
	}
	return EmbeddingStatusReport{
		Status:                   status,
		MemoryVectorDims:         dims,
		StoredEmbedFingerprint:   storedFingerprint,
		CurrentEmbedFingerprint:  currentFingerprint,
		NoteCount:                health.NoteCount,
		EmbeddedNoteCount:        health.EmbeddedNoteCount,
		VectorRowCount:           health.VectorRowCount,
		MissingVectorCount:       health.MissingVectorCount,
		FingerprintMismatchCount: health.FingerprintMismatchCount,
		DirtyVectorCount:         health.DirtyVectorCount,
		ActiveDocCount:           health.ActiveDocCount,
		InactiveDocCount:         health.InactiveDocCount,
		LastDocSyncAt:            lastDocSync,
		DocSyncPartial:           docSync.PartialScan,
		DocSyncWarning:           docSync.Warning,
		LastVectorIndexError:     firstNonEmpty(db.LastVectorIndexError(), memory.LastVectorIndexError()),
		LastDocRetrievalError:    memory.LastDocRetrievalError(),
		SearchMode:               searchMode,
	}, nil
}

func deriveEmbeddingStatus(dims int, storedFingerprint, currentFingerprint string, health db.MemoryEmbeddingHealth, embedModel string) string {
	if strings.TrimSpace(embedModel) == "" {
		return "unavailable"
	}
	if strings.TrimSpace(storedFingerprint) == "" && dims > 0 {
		return "legacy-unknown"
	}
	if strings.TrimSpace(storedFingerprint) != "" && strings.TrimSpace(storedFingerprint) != strings.TrimSpace(currentFingerprint) {
		return "mismatch"
	}
	if health.MissingVectorCount > 0 || health.DirtyVectorCount > 0 || health.FingerprintMismatchCount > 0 {
		return "degraded"
	}
	if dims <= 0 && health.EmbeddedNoteCount > 0 {
		return "degraded"
	}
	if strings.TrimSpace(db.LastVectorIndexError()) != "" || strings.TrimSpace(memory.LastVectorIndexError()) != "" {
		return "degraded"
	}
	if strings.TrimSpace(memory.LastDocRetrievalError()) != "" {
		return "degraded"
	}
	return "ok"
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func (s *Service) RebuildEmbeddings(ctx context.Context, target string) (EmbeddingRebuildResult, error) {
	cfg, provider := s.runtimeSnapshot()
	database, err := s.requireDB()
	if err != nil {
		return EmbeddingRebuildResult{}, err
	}
	if provider == nil {
		return EmbeddingRebuildResult{}, ErrProviderUnavailable
	}
	target = strings.ToLower(strings.TrimSpace(target))
	if target == "" {
		target = "memory"
	}
	result := EmbeddingRebuildResult{
		Status:      "ok",
		Target:      target,
		Fingerprint: providers.EmbeddingFingerprint(cfg.Provider.APIBase, cfg.Provider.EmbedModel, cfg.Provider.EmbedDimensions),
	}
	if strings.TrimSpace(cfg.Provider.EmbedModel) == "" {
		return EmbeddingRebuildResult{}, fmt.Errorf("provider.embedModel is not configured")
	}
	switch target {
	case "memory":
		count, skipped, err := rebuildMemoryEmbeddings(ctx, database, provider, cfg.Provider.EmbedModel, result.Fingerprint)
		if err != nil {
			return EmbeddingRebuildResult{}, err
		}
		result.MemoryNotesRebuilt = count
		result.Skipped = append(result.Skipped, skipped...)
	case "docs":
		docsRebuilt, skipped, err := rebuildDocEmbeddings(ctx, cfg, database, provider, result.Fingerprint)
		if err != nil {
			return EmbeddingRebuildResult{}, err
		}
		result.DocsRebuilt = docsRebuilt
		result.Skipped = append(result.Skipped, skipped...)
	case "all":
		count, skipped, err := rebuildMemoryEmbeddings(ctx, database, provider, cfg.Provider.EmbedModel, result.Fingerprint)
		if err != nil {
			return EmbeddingRebuildResult{}, err
		}
		result.MemoryNotesRebuilt = count
		result.Skipped = append(result.Skipped, skipped...)
		docsRebuilt, skipped, err := rebuildDocEmbeddings(ctx, cfg, database, provider, result.Fingerprint)
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
	cfg := s.configSnapshot()
	report := AuditStatusReport{
		Enabled:       cfg.Security.Audit.Enabled,
		Strict:        cfg.Security.Audit.Strict,
		VerifyOnStart: cfg.Security.Audit.VerifyOnStart,
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
	if err := ValidateScopeLinkInput(input.SessionKey, input.ScopeKey, input.Meta); err != nil {
		return ScopeLinkResult{}, err
	}
	database, err := s.requireDB()
	if err != nil {
		return ScopeLinkResult{}, err
	}
	sessionKey := strings.TrimSpace(input.SessionKey)
	scopeKey := strings.TrimSpace(input.ScopeKey)
	if err := database.LinkSession(ctx, sessionKey, scopeKey, input.Meta); err != nil {
		return ScopeLinkResult{}, err
	}
	resolved, err := database.ResolveScopeKey(ctx, sessionKey)
	if err != nil {
		return ScopeLinkResult{}, err
	}
	s.recordScopeAudit(ctx, "scope.link", scopeActor(input.Actor, ctx), sessionKey, map[string]any{
		"scope_key": resolved,
		"meta":      input.Meta,
	})
	return ScopeLinkResult{SessionKey: sessionKey, ScopeKey: resolved}, nil
}

func (s *Service) ResolveScopeKey(ctx context.Context, sessionKey string) (string, error) {
	if err := ValidateScopeSessionKey(sessionKey); err != nil {
		return "", err
	}
	database, err := s.requireDB()
	if err != nil {
		return "", err
	}
	sessionKey = strings.TrimSpace(sessionKey)
	resolved, err := database.ResolveScopeKey(ctx, sessionKey)
	if err != nil {
		return "", err
	}
	s.recordScopeAudit(ctx, "scope.resolve", scopeActor("", ctx), sessionKey, map[string]any{"scope_key": resolved})
	return resolved, nil
}

func (s *Service) ListScopeSessions(ctx context.Context, scopeKey string) ([]string, error) {
	if err := ValidateScopeKey(scopeKey); err != nil {
		return nil, err
	}
	database, err := s.requireDB()
	if err != nil {
		return nil, err
	}
	scopeKey = strings.TrimSpace(scopeKey)
	sessions, err := database.ListScopeSessions(ctx, scopeKey)
	if err != nil {
		return nil, err
	}
	s.recordScopeAudit(ctx, "scope.list", scopeActor("", ctx), scopeKey, map[string]any{
		"session_count": len(sessions),
	})
	return sessions, nil
}

func (s *Service) recordScopeAudit(ctx context.Context, eventType, actor, sessionKey string, payload map[string]any) {
	audit, ok := s.auditLogger()
	if !ok {
		return
	}
	_ = audit.Record(ctx, eventType, sessionKey, actor, payload)
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
	return nil, ErrDatabaseUnavailable
}

func (s *Service) requireProvider() (*providers.Client, error) {
	_, provider := s.runtimeSnapshot()
	if provider != nil {
		return provider, nil
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
	staged := make([]db.StagedMemoryEmbedding, 0, len(rows))
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
		staged = append(staged, db.StagedMemoryEmbedding{ID: row.ID, Embedding: memory.PackFloat32(vec)})
	}
	if err := database.ApplyStagedMemoryEmbeddings(ctx, fingerprint, staged); err != nil {
		return 0, nil, err
	}
	if wantDims > 0 {
		if err := database.RebuildMemoryVecIndexWithProfile(ctx, wantDims, fingerprint); err != nil {
			return 0, nil, err
		}
		if _, err := database.SQL.ExecContext(ctx, `UPDATE memory_notes SET vector_index_dirty=0 WHERE typeof(embedding)='blob' AND length(embedding) >= 4`); err != nil {
			return 0, nil, err
		}
	}
	return len(rows), nil, nil
}

func rebuildDocEmbeddings(ctx context.Context, cfg config.Config, database *db.DB, provider *providers.Client, fingerprint string) (bool, []string, error) {
	return false, []string{"doc_rebuild_unavailable"}, nil
}

func CollectCapabilitiesReport(cfg config.Config, broker *approval.Broker, channelFilter, triggerFilter string) CapabilitiesReport {
	spec := config.ProfileSpec(cfg.RuntimeProfile)
	defaultRunner := strings.TrimSpace(cfg.Runners.Default)
	if defaultRunner == "" {
		defaultRunner = "opencode"
	}
	terminalAvailable := cfg.Hardening.GuardedTools && cfg.Hardening.PrivilegedTools && cfg.Hardening.EnableExecShell && !spec.ForbidExecShell && !spec.ForbidPrivilegedTools && !spec.RequireSandboxForExec
	report := CapabilitiesReport{
		RuntimeProfile:     string(cfg.RuntimeProfile),
		Hosted:             spec.Hosted,
		HostID:             cfg.Security.Approvals.HostID,
		ExecutionModel:     "runner-managed",
		DefaultRunner:      defaultRunner,
		RunnerMode:         cfg.Runners.DefaultMode,
		RunnerIsolation:    cfg.Runners.DefaultIsolation,
		TerminalAvailable:  terminalAvailable,
		Approvals:          ApprovalModes(cfg),
		SkillExecEnabled:   false,
		ExecAvailable:      false,
		ShellModeAvailable: false,
		SandboxEnabled:     cfg.Hardening.Sandbox.Enabled,
		SandboxRequired:    spec.RequireSandboxForExec,
		NetworkPolicy:      cfg.Security.Network,
		HeartbeatEnabled:   cfg.Heartbeat.Enabled,
		CronEnabled:        cfg.Cron.Enabled,
		ApprovalBroker:     approvalBrokerCapabilities(cfg, broker),
	}
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

func approvalBrokerCapabilities(cfg config.Config, broker *approval.Broker) map[string]any {
	return map[string]any{
		"enabled":       cfg.Security.Approvals.Enabled,
		"required":      approvalBrokerRequired(cfg),
		"available":     broker != nil,
		"canIssueToken": broker != nil && len(broker.SignKey) > 0,
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
	allowedHosts := append([]string{}, profile.AllowedHosts...)
	sort.Strings(allowedHosts)
	writablePaths := append([]string{}, profile.WritablePaths...)
	sort.Strings(writablePaths)
	return &CapabilitiesProfileSummary{
		Name:          name,
		MaxCapability: strings.TrimSpace(profile.MaxCapability),
		AllowedHosts:  allowedHosts,
		WritablePaths: writablePaths,
	}
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

func BuildJobResponse(snapshot jobs.Snapshot) map[string]any {
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

func BuildJobSnapshotResponse(snapshot jobs.Snapshot) map[string]any {
	response := BuildJobResponse(snapshot)
	response["created_at"] = snapshot.CreatedAt
	response["updated_at"] = snapshot.UpdatedAt
	response["events"] = snapshot.Events
	return response
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

// BuildRunnerRunResponse converts a persisted runner_runs row into a
// sanitized JSON map for the agents API.
func BuildRunnerRunResponse(run db.RunnerRun) map[string]any {
	displayTask := displayTaskForRunnerRun(run)
	out := map[string]any{
		"job_id":             run.JobID,
		"run_id":             run.ID,
		"kind":               "runner:" + run.RunnerID,
		"runner_id":          run.RunnerID,
		"parent_session_key": run.ParentSessionKey,
		"task":               displayTask,
		"mode":               run.Mode,
		"isolation":          run.Isolation,
		"status":             run.Status,
		"requested_at":       formatRunnerRunTime(run.RequestedAt),
		"updated_at":         formatRunnerRunTime(latestRunnerRunTimestamp(run)),
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
		out["started_at"] = formatRunnerRunTime(run.StartedAt)
	}
	if run.CompletedAt > 0 {
		out["completed_at"] = formatRunnerRunTime(run.CompletedAt)
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
	if displayTask != strings.TrimSpace(run.Task) {
		out["execution_prompt_injected"] = true
	}
	return out
}

func displayTaskForRunnerRun(run db.RunnerRun) string {
	meta := map[string]any{}
	if strings.TrimSpace(run.MetaJSON) != "" && strings.TrimSpace(run.MetaJSON) != "{}" {
		_ = json.Unmarshal([]byte(run.MetaJSON), &meta)
	}
	for _, key := range []string{"ui_task", "runner_chat_user_message"} {
		if value, ok := meta[key].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return strings.TrimSpace(run.Task)
}

// BuildRunnerRunListResponse renders a list of persisted runner runs.
func BuildRunnerRunListResponse(runs []db.RunnerRun) map[string]any {
	items := make([]map[string]any, 0, len(runs))
	for _, run := range runs {
		items = append(items, BuildRunnerRunResponse(run))
	}
	return map[string]any{"items": items}
}

// BuildRunnerRunEventListResponse renders a list of persisted runner run events.
func BuildRunnerRunEventListResponse(events []db.RunnerRunEvent) map[string]any {
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

func latestRunnerRunTimestamp(run db.RunnerRun) int64 {
	latest := run.RequestedAt
	if run.StartedAt > latest {
		latest = run.StartedAt
	}
	if run.CompletedAt > latest {
		latest = run.CompletedAt
	}
	return latest
}

func formatRunnerRunTime(ms int64) string {
	if ms <= 0 {
		return ""
	}
	return time.UnixMilli(ms).UTC().Format(time.RFC3339)
}
