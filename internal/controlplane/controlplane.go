package controlplane

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"or3-intern/internal/agent"
	"or3-intern/internal/approval"
	"or3-intern/internal/config"
	"or3-intern/internal/db"
	intdoctor "or3-intern/internal/doctor"
)

var (
	ErrApprovalBrokerUnavailable = errors.New("approval broker unavailable")
	ErrJobRegistryUnavailable    = errors.New("job registry unavailable")
	ErrJobNotFound               = errors.New("job not found")
)

const (
	defaultListLimit = 100
	maxListLimit     = 200
)

type Service struct {
	Config          config.Config
	Runtime         *agent.Runtime
	Broker          *approval.Broker
	Jobs            *agent.JobRegistry
	SubagentManager *agent.SubagentManager
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
	EnabledMCPServers  []string                     `json:"enabledMcpServers,omitempty"`
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
}

type ReadinessReport struct {
	Status   string              `json:"status"`
	Ready    bool                `json:"ready"`
	Summary  intdoctor.Summary   `json:"summary"`
	Findings []intdoctor.Finding `json:"findings,omitempty"`
}

func New(cfg config.Config, rt *agent.Runtime, broker *approval.Broker, jobs *agent.JobRegistry, subagentManager *agent.SubagentManager) *Service {
	return &Service{
		Config:          cfg,
		Runtime:         rt,
		Broker:          broker,
		Jobs:            jobs,
		SubagentManager: subagentManager,
	}
}

func (s *Service) GetHealth() HealthReport {
	report := HealthReport{
		Status:                  "ok",
		RuntimeAvailable:        s != nil && s.Runtime != nil,
		JobRegistryAvailable:    s != nil && s.Jobs != nil,
		SubagentManagerEnabled:  s != nil && s.SubagentManager != nil,
		ApprovalBrokerAvailable: s != nil && s.Broker != nil,
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
		Ready:    !report.HasBlockingFindings(),
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

func (s *Service) requireBroker() (*approval.Broker, error) {
	if s == nil || s.Broker == nil {
		return nil, ErrApprovalBrokerUnavailable
	}
	return s.Broker, nil
}

func CollectCapabilitiesReport(cfg config.Config, broker *approval.Broker, channelFilter, triggerFilter string) CapabilitiesReport {
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
	report.EnabledMCPServers = enabledMCPServers(cfg)
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

func enabledMCPServers(cfg config.Config) []string {
	out := make([]string, 0, len(cfg.Tools.MCPServers))
	for name, server := range cfg.Tools.MCPServers {
		if server.Enabled {
			out = append(out, name)
		}
	}
	sort.Strings(out)
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

func DescribeUnavailable(err error) error {
	switch {
	case errors.Is(err, ErrApprovalBrokerUnavailable):
		return fmt.Errorf("approval broker unavailable")
	case errors.Is(err, ErrJobRegistryUnavailable):
		return fmt.Errorf("job registry unavailable")
	default:
		return err
	}
}
