// Package config defines the persisted runtime configuration and validation
// rules for or3-intern.
package config

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
)

func validateProviderRouting(cfg Config) error {
	if len(cfg.Providers) == 0 {
		return fmt.Errorf("providers must include at least one provider")
	}
	for key, profile := range cfg.Providers {
		if normalizeProviderKey(key) == "" {
			return fmt.Errorf("provider key cannot be empty")
		}
		if key != "custom" && strings.TrimSpace(profile.APIBase) == "" {
			return fmt.Errorf("provider %s apiBase is required", key)
		}
		if profile.TimeoutSeconds <= 0 {
			return fmt.Errorf("provider %s timeoutSeconds must be positive", key)
		}
	}
	roles := map[string]ModelRoleConfig{
		"chat":           cfg.ModelRouting.Chat,
		"agents":         cfg.ModelRouting.Agents,
		"subagents":      cfg.ModelRouting.Subagents,
		"summarization":  cfg.ModelRouting.Summarization,
		"contextManager": cfg.ModelRouting.ContextManager,
		"embeddings":     cfg.ModelRouting.Embeddings,
	}
	for roleName, role := range roles {
		if err := validateModelRef(cfg, roleName, "primary", role.Primary); err != nil {
			return err
		}
		seen := map[string]struct{}{}
		for i, ref := range role.Fallbacks {
			if err := validateModelRef(cfg, roleName, fmt.Sprintf("fallback[%d]", i), ref); err != nil {
				return err
			}
			key := ref.Provider + "\x00" + ref.Model
			if _, ok := seen[key]; ok {
				return fmt.Errorf("%s fallback chain contains duplicate %s/%s", roleName, ref.Provider, ref.Model)
			}
			seen[key] = struct{}{}
		}
		if role.EmbedDimensions < 0 {
			return fmt.Errorf("%s embedDimensions cannot be negative", roleName)
		}
	}
	return nil
}

func validateModelRef(cfg Config, roleName, slot string, ref ModelRef) error {
	provider := normalizeProviderKey(ref.Provider)
	if provider == "" {
		return fmt.Errorf("%s %s provider is required", roleName, slot)
	}
	if _, ok := cfg.Providers[provider]; !ok {
		return fmt.Errorf("%s %s provider %q is not configured", roleName, slot, ref.Provider)
	}
	if strings.TrimSpace(ref.Model) == "" {
		return fmt.Errorf("%s %s model is required", roleName, slot)
	}
	return nil
}

func normalizeInboundPolicy(policy InboundPolicy) InboundPolicy {
	return InboundPolicy(strings.ToLower(strings.TrimSpace(string(policy))))
}

// EffectiveInboundPolicy resolves the effective ingress policy for a channel,
// preserving legacy openAccess behavior when inboundPolicy is unset.
func EffectiveInboundPolicy(configured InboundPolicy, openAccess bool, hasAllowlist bool) string {
	switch normalizeInboundPolicy(configured) {
	case InboundPolicyDeny:
		return string(InboundPolicyDeny)
	case InboundPolicyAllowlist:
		if hasAllowlist {
			return string(InboundPolicyAllowlist)
		}
		if openAccess {
			return "open"
		}
		return string(InboundPolicyDeny)
	case InboundPolicyPairing:
		return string(InboundPolicyPairing)
	}
	if hasAllowlist {
		return string(InboundPolicyAllowlist)
	}
	if openAccess {
		return "open"
	}
	return string(InboundPolicyDeny)
}

func validateChannelAccess(cfg Config) error {
	cfg.Channels.Telegram.InboundPolicy = normalizeInboundPolicy(cfg.Channels.Telegram.InboundPolicy)
	cfg.Channels.Slack.InboundPolicy = normalizeInboundPolicy(cfg.Channels.Slack.InboundPolicy)
	cfg.Channels.Discord.InboundPolicy = normalizeInboundPolicy(cfg.Channels.Discord.InboundPolicy)
	cfg.Channels.WhatsApp.InboundPolicy = normalizeInboundPolicy(cfg.Channels.WhatsApp.InboundPolicy)
	cfg.Channels.Email.InboundPolicy = normalizeInboundPolicy(cfg.Channels.Email.InboundPolicy)
	for name, policy := range map[string]InboundPolicy{
		"channels.telegram.inboundPolicy": cfg.Channels.Telegram.InboundPolicy,
		"channels.slack.inboundPolicy":    cfg.Channels.Slack.InboundPolicy,
		"channels.discord.inboundPolicy":  cfg.Channels.Discord.InboundPolicy,
		"channels.whatsApp.inboundPolicy": cfg.Channels.WhatsApp.InboundPolicy,
		"channels.email.inboundPolicy":    cfg.Channels.Email.InboundPolicy,
	} {
		if policy != "" && policy != InboundPolicyDeny && policy != InboundPolicyAllowlist && policy != InboundPolicyPairing {
			return errors.New(name + ": must be deny, allowlist, or pairing")
		}
	}
	if cfg.Channels.Telegram.Enabled && requiresChannelAllowlist(cfg.Channels.Telegram.InboundPolicy, cfg.Channels.Telegram.OpenAccess, hasNonEmpty(cfg.Channels.Telegram.AllowedChatIDs)) {
		return errors.New("telegram enabled: set channels.telegram.allowedChatIds, channels.telegram.inboundPolicy=pairing, or channels.telegram.openAccess=true")
	}
	if cfg.Channels.Slack.Enabled && requiresChannelAllowlist(cfg.Channels.Slack.InboundPolicy, cfg.Channels.Slack.OpenAccess, hasNonEmpty(cfg.Channels.Slack.AllowedUserIDs)) {
		return errors.New("slack enabled: set channels.slack.allowedUserIds, channels.slack.inboundPolicy=pairing, or channels.slack.openAccess=true")
	}
	if cfg.Channels.Discord.Enabled && requiresChannelAllowlist(cfg.Channels.Discord.InboundPolicy, cfg.Channels.Discord.OpenAccess, hasNonEmpty(cfg.Channels.Discord.AllowedUserIDs)) {
		return errors.New("discord enabled: set channels.discord.allowedUserIds, channels.discord.inboundPolicy=pairing, or channels.discord.openAccess=true")
	}
	if cfg.Channels.WhatsApp.Enabled && requiresChannelAllowlist(cfg.Channels.WhatsApp.InboundPolicy, cfg.Channels.WhatsApp.OpenAccess, hasNonEmpty(cfg.Channels.WhatsApp.AllowedFrom)) {
		return errors.New("whatsApp enabled: set channels.whatsApp.allowedFrom, channels.whatsApp.inboundPolicy=pairing, or channels.whatsApp.openAccess=true")
	}
	if cfg.Channels.Email.Enabled {
		if !cfg.Channels.Email.ConsentGranted {
			return errors.New("email enabled: set channels.email.consentGranted=true after explicit permission")
		}
		if requiresChannelAllowlist(cfg.Channels.Email.InboundPolicy, cfg.Channels.Email.OpenAccess, hasNonEmpty(cfg.Channels.Email.AllowedSenders)) {
			return errors.New("email enabled: set channels.email.allowedSenders, channels.email.inboundPolicy=pairing, or channels.email.openAccess=true")
		}
		if strings.TrimSpace(cfg.Channels.Email.IMAPHost) == "" || strings.TrimSpace(cfg.Channels.Email.IMAPUsername) == "" || strings.TrimSpace(cfg.Channels.Email.IMAPPassword) == "" {
			return errors.New("email enabled: imapHost, imapUsername, and imapPassword are required")
		}
		if strings.TrimSpace(cfg.Channels.Email.SMTPHost) == "" || strings.TrimSpace(cfg.Channels.Email.SMTPUsername) == "" || strings.TrimSpace(cfg.Channels.Email.SMTPPassword) == "" {
			return errors.New("email enabled: smtpHost, smtpUsername, and smtpPassword are required")
		}
	}
	return nil
}

func requiresChannelAllowlist(policy InboundPolicy, openAccess bool, hasAllowlist bool) bool {
	switch normalizeInboundPolicy(policy) {
	case InboundPolicyAllowlist:
		return !hasAllowlist
	case InboundPolicyPairing, InboundPolicyDeny:
		return false
	default:
		return !openAccess && !hasAllowlist
	}
}

func validateMCPServers(servers map[string]MCPServerConfig) error {
	for name, server := range servers {
		name = strings.TrimSpace(name)
		if name == "" {
			return errors.New("tools.mcpServers contains an empty server name")
		}
		if !server.Enabled {
			continue
		}
		switch server.Transport {
		case "stdio":
			if server.Command == "" {
				return errors.New("tools.mcpServers." + name + ": stdio transport requires command")
			}
		case "sse", "streamablehttp":
			if err := validateMCPHTTPURL(name, server); err != nil {
				return err
			}
		default:
			return errors.New("tools.mcpServers." + name + ": unsupported transport " + strconv.Quote(server.Transport))
		}
	}
	return nil
}

// ValidateMCPServers validates an MCP server map before persisting user edits.
func ValidateMCPServers(servers map[string]MCPServerConfig) error {
	return validateMCPServers(servers)
}

func validateMCPHTTPURL(name string, server MCPServerConfig) error {
	if server.URL == "" {
		return errors.New("tools.mcpServers." + name + ": transport " + strconv.Quote(server.Transport) + " requires url")
	}
	u, err := url.Parse(server.URL)
	if err != nil {
		return errors.New("tools.mcpServers." + name + ": invalid url")
	}
	if u.User != nil {
		return errors.New("tools.mcpServers." + name + ": url must not embed credentials")
	}
	if u.Host == "" {
		return errors.New("tools.mcpServers." + name + ": url must include host")
	}
	switch strings.ToLower(u.Scheme) {
	case "https":
		return nil
	case "http":
		if !server.AllowInsecureHTTP {
			return errors.New("tools.mcpServers." + name + ": insecure http requires allowInsecureHttp=true")
		}
		if !isLoopbackHost(u.Hostname()) {
			return errors.New("tools.mcpServers." + name + ": insecure http is limited to localhost or loopback hosts")
		}
		return nil
	default:
		return errors.New("tools.mcpServers." + name + ": url scheme must be https or http")
	}
}

func validateAccessProfiles(cfg AccessProfilesConfig) error {
	if !cfg.Enabled {
		return nil
	}
	if strings.TrimSpace(cfg.Default) != "" {
		if _, ok := cfg.Profiles[strings.TrimSpace(cfg.Default)]; !ok {
			return errors.New("security.profiles.default references unknown profile")
		}
	}
	for name, profile := range cfg.Profiles {
		name = strings.TrimSpace(name)
		if name == "" {
			return errors.New("security.profiles.profiles contains an empty profile name")
		}
		if !isSupportedCapabilityValue(profile.MaxCapability) {
			return errors.New("security.profiles.profiles." + name + ": unsupported maxCapability")
		}
	}
	for channel, profileName := range cfg.Channels {
		if strings.TrimSpace(channel) == "" {
			return errors.New("security.profiles.channels contains an empty channel name")
		}
		if _, ok := cfg.Profiles[strings.TrimSpace(profileName)]; !ok {
			return errors.New("security.profiles.channels." + channel + " references unknown profile")
		}
	}
	for trigger, profileName := range cfg.Triggers {
		if strings.TrimSpace(trigger) == "" {
			return errors.New("security.profiles.triggers contains an empty trigger name")
		}
		if _, ok := cfg.Profiles[strings.TrimSpace(profileName)]; !ok {
			return errors.New("security.profiles.triggers." + trigger + " references unknown profile")
		}
	}
	return nil
}

func normalizeCapabilityValue(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "safe":
		return "safe"
	case "guarded":
		return "guarded"
	case "privileged":
		return "privileged"
	default:
		return ""
	}
}

func isSupportedCapabilityValue(raw string) bool {
	return strings.TrimSpace(raw) == "" || normalizeCapabilityValue(raw) != ""
}

func normalizeServiceRole(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "viewer":
		return "viewer"
	case "service-client":
		return "service-client"
	case "operator":
		return "operator"
	case "admin":
		return "admin"
	default:
		return ""
	}
}

func validateApprovals(cfg ApprovalConfig) error {
	if strings.TrimSpace(cfg.HostID) == "" {
		return errors.New("security.approvals.hostId is required")
	}
	for name, mode := range map[string]ApprovalMode{
		"pairing":        cfg.Pairing.Mode,
		"exec":           cfg.Exec.Mode,
		"skillExecution": cfg.SkillExecution.Mode,
		"secretAccess":   cfg.SecretAccess.Mode,
		"messageSend":    cfg.MessageSend.Mode,
	} {
		if !isValidApprovalMode(mode) {
			return errors.New("security.approvals." + name + ": unsupported mode")
		}
	}
	return nil
}

func normalizeAuthEnforcementMode(mode AuthEnforcementMode) AuthEnforcementMode {
	switch AuthEnforcementMode(strings.ToLower(strings.TrimSpace(string(mode)))) {
	case AuthEnforcementOff:
		return AuthEnforcementOff
	case AuthEnforcementWarn:
		return AuthEnforcementWarn
	case AuthEnforcementSensitive:
		return AuthEnforcementSensitive
	case AuthEnforcementSession:
		return AuthEnforcementSession
	default:
		return ""
	}
}

func normalizeAuthFallbackPolicy(policy string) string {
	switch strings.ToLower(strings.TrimSpace(policy)) {
	case AuthFallbackPairedTokenOnly:
		return AuthFallbackPairedTokenOnly
	case AuthFallbackPairedTokenPlusWarn:
		return AuthFallbackPairedTokenPlusWarn
	case AuthFallbackAdminRecoveryOnly:
		return AuthFallbackAdminRecoveryOnly
	default:
		return ""
	}
}

func validateAuthConfig(cfg AuthConfig) error {
	if cfg.SessionIdleTTLSeconds <= 0 {
		return errors.New("auth.sessionIdleTtlSeconds must be greater than zero")
	}
	if cfg.SessionAbsoluteTTLSeconds <= 0 {
		return errors.New("auth.sessionAbsoluteTtlSeconds must be greater than zero")
	}
	if cfg.SessionAbsoluteTTLSeconds < cfg.SessionIdleTTLSeconds {
		return errors.New("auth.sessionAbsoluteTtlSeconds must be greater than or equal to auth.sessionIdleTtlSeconds")
	}
	if cfg.StepUpTTLSeconds <= 0 {
		return errors.New("auth.stepUpTtlSeconds must be greater than zero")
	}
	if normalizeAuthFallbackPolicy(cfg.FallbackPolicy) == "" {
		return errors.New("auth.fallbackPolicy must be paired-token-only, paired-token-plus-warning, or admin-recovery-only")
	}
	if normalizeAuthEnforcementMode(cfg.EnforcementMode) == "" {
		return errors.New("auth.enforcementMode must be off, warn, enforce-sensitive, or enforce-session")
	}
	if !cfg.Enabled {
		return nil
	}
	if strings.TrimSpace(cfg.RPID) == "" {
		return errors.New("auth.rpId is required when auth.enabled=true")
	}
	if len(cfg.AllowedOrigins) == 0 {
		return errors.New("auth.allowedOrigins is required when auth.enabled=true")
	}
	rpid := strings.ToLower(strings.TrimSpace(cfg.RPID))
	if strings.Contains(rpid, "://") {
		return errors.New("auth.rpId must be a domain, not a URL")
	}
	if strings.Contains(rpid, "*") {
		return errors.New("auth.rpId must not contain wildcards")
	}
	if ip := net.ParseIP(strings.Trim(rpid, "[]")); ip != nil {
		return errors.New("auth.rpId must not be a raw IP address")
	}
	for _, origin := range append(append([]string{}, cfg.AllowedOrigins...), cfg.RelatedOrigins...) {
		if err := validateAuthOrigin(rpid, origin); err != nil {
			return err
		}
	}
	return nil
}

func validateAuthOrigin(rpid, origin string) error {
	origin = strings.TrimSpace(origin)
	if origin == "" {
		return errors.New("auth origins must not be empty")
	}
	if strings.Contains(origin, "*") {
		return fmt.Errorf("auth origin %q must not contain wildcards", origin)
	}
	u, err := url.Parse(origin)
	if err != nil {
		return fmt.Errorf("auth origin %q is invalid", origin)
	}
	if u.Hostname() == "" {
		return fmt.Errorf("auth origin %q must include a hostname", origin)
	}
	host := strings.ToLower(strings.TrimSpace(u.Hostname()))
	if ip := net.ParseIP(strings.Trim(host, "[]")); ip != nil {
		return fmt.Errorf("auth origin %q must not use a raw IP address", origin)
	}
	if strings.EqualFold(u.Scheme, "http") {
		if host != "localhost" || rpid != "localhost" {
			return fmt.Errorf("auth origin %q is insecure; only localhost development may use http", origin)
		}
		return nil
	}
	if !strings.EqualFold(u.Scheme, "https") {
		return fmt.Errorf("auth origin %q must use https or localhost http", origin)
	}
	if rpid == "localhost" {
		return fmt.Errorf("auth origin %q cannot be used with localhost rpId", origin)
	}
	if host != rpid && !strings.HasSuffix(host, "."+rpid) {
		return fmt.Errorf("auth origin %q does not match rpId %q", origin, rpid)
	}
	return nil
}

func normalizeApprovalMode(mode ApprovalMode, fallback ApprovalMode) ApprovalMode {
	normalized := ApprovalMode(strings.ToLower(strings.TrimSpace(string(mode))))
	if normalized == "" {
		return fallback
	}
	return normalized
}

func isValidApprovalMode(mode ApprovalMode) bool {
	switch normalizeApprovalMode(mode, "") {
	case ApprovalModeDeny, ApprovalModeAsk, ApprovalModeAllowlist, ApprovalModeTrusted:
		return true
	default:
		return false
	}
}

func validateRuntimeProfile(p RuntimeProfile) error {
	switch p {
	case "", ProfileLocalDev, ProfileSingleUserHardened,
		ProfileHostedService, ProfileHostedNoExec, ProfileHostedRemoteSandbox:
		return nil
	}
	return errors.New("unrecognized runtimeProfile: " + string(p))
}

// ValidateProfile checks that the profile+config combination is safe.
// It returns the first constraint violation found.
func ValidateProfile(cfg Config) error {
	spec := ProfileSpec(cfg.RuntimeProfile)
	if spec.RequireSecretStore {
		if !cfg.Security.SecretStore.Enabled {
			return errors.New("hosted profiles require security.secretStore.enabled")
		}
	}
	if spec.RequireAudit {
		if !cfg.Security.Audit.Enabled {
			return errors.New("hosted profiles require security.audit.enabled")
		}
	}
	if spec.RequireNetworkPolicy {
		if !cfg.Security.Network.Enabled && !cfg.Security.Network.DefaultDeny {
			return errors.New("hosted profiles require security.network policy to be configured")
		}
	}
	if spec.Hosted && hasRemoteHTTPMCPServers(cfg.Tools.MCPServers) {
		if !cfg.Security.Network.Enabled || !cfg.Security.Network.DefaultDeny {
			return errors.New("hosted profiles require deny-by-default security.network for remote MCP HTTP")
		}
		if networkAllowlistTooBroad(cfg.Security.Network.AllowedHosts) {
			return errors.New("hosted profiles require a narrow security.network.allowedHosts for remote MCP HTTP")
		}
	}
	if spec.RequireStrictAudit {
		if !cfg.Security.Audit.Strict {
			return errors.New("profile requires security.audit.strict")
		}
	}
	if spec.RequireAuditVerifyStart {
		if !cfg.Security.Audit.VerifyOnStart {
			return errors.New("profile requires security.audit.verifyOnStart")
		}
	}
	if spec.RequireSecretStoreKey {
		if !cfg.Security.SecretStore.Required {
			return errors.New("profile requires security.secretStore.required")
		}
	}
	if spec.ForbidExecShell {
		if cfg.Hardening.EnableExecShell {
			return errors.New("hosted-no-exec profile does not allow enableExecShell")
		}
	}
	if spec.ForbidPrivilegedTools {
		if cfg.Hardening.PrivilegedTools {
			return errors.New("hosted-no-exec profile does not allow privilegedTools")
		}
	}
	if spec.RequireSandboxForExec {
		if cfg.Hardening.EnableExecShell && !cfg.Hardening.Sandbox.Enabled {
			return errors.New("hosted-remote-sandbox-only profile requires sandbox for exec")
		}
	}
	return nil
}

// IsHostedProfile reports whether p is one of the hosted runtime profiles.
func IsHostedProfile(p RuntimeProfile) bool {
	switch p {
	case ProfileHostedService, ProfileHostedNoExec, ProfileHostedRemoteSandbox:
		return true
	}
	return false
}

func normalizeQuotaExceededAction(action QuotaExceededAction, fallback QuotaExceededAction) QuotaExceededAction {
	normalized := QuotaExceededAction(strings.ToLower(strings.TrimSpace(string(action))))
	if normalized == "" {
		return fallback
	}
	switch normalized {
	case QuotaExceededActionAsk, QuotaExceededActionFail:
		return normalized
	default:
		return fallback
	}
}

func compactStrings(values []string) []string {
	if len(values) == 0 {
		return []string{}
	}
	out := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		key := strings.ToLower(trimmed)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, trimmed)
	}
	return out
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(strings.TrimSpace(host), "localhost") {
		return true
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	return ip != nil && ip.IsLoopback()
}

func hasRemoteHTTPMCPServers(servers map[string]MCPServerConfig) bool {
	for _, server := range servers {
		if !server.Enabled {
			continue
		}
		transport := strings.ToLower(strings.TrimSpace(server.Transport))
		if transport != "sse" && transport != "streamablehttp" {
			continue
		}
		u, err := url.Parse(strings.TrimSpace(server.URL))
		if err != nil {
			return true
		}
		if u.Hostname() == "" || !isLoopbackHost(u.Hostname()) {
			return true
		}
	}
	return false
}

func networkAllowlistTooBroad(hosts []string) bool {
	if len(hosts) > 10 {
		return true
	}
	for _, host := range hosts {
		host = strings.TrimSpace(strings.ToLower(host))
		if host == "*" {
			return true
		}
		if strings.HasPrefix(host, "*.") {
			return true
		}
		if strings.Contains(host, "*") {
			return true
		}
	}
	return false
}

func validateAgentCLIConfig(cfg AgentCLIConfig) error {
	mode := strings.TrimSpace(cfg.DefaultMode)
	switch mode {
	case "review", "safe_edit", "sandbox_auto":
	default:
		return errors.New("agentCLI.defaultMode must be review, safe_edit, or sandbox_auto")
	}
	iso := strings.TrimSpace(cfg.DefaultIsolation)
	switch iso {
	case "host_readonly", "host_workspace_write", "sandbox_workspace_write", "sandbox_dangerous":
	default:
		return errors.New("agentCLI.defaultIsolation must be host_readonly, host_workspace_write, sandbox_workspace_write, or sandbox_dangerous")
	}
	if mode == "sandbox_auto" && iso != "sandbox_dangerous" {
		return errors.New("agentCLI.defaultMode=sandbox_auto requires agentCLI.defaultIsolation=sandbox_dangerous")
	}
	for runner, runtimeMode := range cfg.RuntimeMode {
		switch strings.TrimSpace(runtimeMode) {
		case "", "auto", "native", "cli":
		default:
			return fmt.Errorf("agentCLI.runtimeMode[%s] must be auto, native, or cli", runner)
		}
	}
	for runner, endpoint := range cfg.NativeServerURLs {
		u, err := url.Parse(strings.TrimSpace(endpoint))
		if err != nil || u.Scheme == "" || u.Host == "" {
			return fmt.Errorf("agentCLI.nativeServerUrls[%s] must be an absolute loopback http URL", runner)
		}
		if u.Scheme != "http" && u.Scheme != "https" {
			return fmt.Errorf("agentCLI.nativeServerUrls[%s] must use http or https", runner)
		}
		if !isLoopbackHost(u.Hostname()) {
			return fmt.Errorf("agentCLI.nativeServerUrls[%s] must point at loopback", runner)
		}
	}
	return nil
}

func hasNonEmpty(values []string) bool {
	for _, value := range values {
		if value != "" {
			return true
		}
	}
	return false
}
