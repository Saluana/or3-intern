package main

import (
	"flag"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"sort"
	"strings"

	"or3-intern/internal/config"
)

type doctorFinding struct {
	Level   string
	Area    string
	Message string
}

func runDoctorCommand(cfg config.Config, args []string, stdout, stderr io.Writer) error {
	if stdout == nil {
		stdout = os.Stdout
	}
	if stderr == nil {
		stderr = os.Stderr
	}
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	fs.SetOutput(stderr)
	strict := fs.Bool("strict", false, "exit non-zero when warnings are found")
	if err := fs.Parse(args); err != nil {
		return err
	}
	findings := doctorFindings(cfg)
	if len(findings) == 0 {
		_, _ = fmt.Fprintln(stdout, "[ok] configuration looks safe")
		return nil
	}
	for _, finding := range findings {
		_, _ = fmt.Fprintf(stdout, "[%s] %s: %s\n", finding.Level, finding.Area, finding.Message)
	}
	if *strict && hasDoctorWarnings(findings) {
		return fmt.Errorf("doctor found warnings")
	}
	return nil
}

func doctorFindings(cfg config.Config) []doctorFinding {
	findings := make([]doctorFinding, 0, 32)
	findings = append(findings, filesystemFindings(cfg)...)
	findings = append(findings, hardeningFindings(cfg)...)
	findings = append(findings, securityFindings(cfg)...)
	findings = append(findings, webhookFindings(cfg)...)
	findings = append(findings, serviceFindings(cfg)...)
	findings = append(findings, mcpFindings(cfg)...)
	findings = append(findings, networkFindings(cfg)...)
	findings = append(findings, profileFindings(cfg)...)
	findings = append(findings, execFindings(cfg)...)
	findings = append(findings, skillFindings(cfg)...)
	findings = append(findings, channelExposureFindings(cfg)...)
	findings = append(findings, runtimeProfileFindings(cfg)...)
	sort.SliceStable(findings, func(i, j int) bool {
		if findings[i].Area == findings[j].Area {
			return findings[i].Message < findings[j].Message
		}
		return findings[i].Area < findings[j].Area
	})
	return findings
}

func filesystemFindings(cfg config.Config) []doctorFinding {
	findings := []doctorFinding{}
	addWarn := func(area, message string) {
		findings = append(findings, doctorFinding{Level: "warn", Area: area, Message: message})
	}
	if !cfg.Tools.RestrictToWorkspace {
		addWarn("filesystem", "workspace restriction is disabled")
	}
	if cfg.Tools.RestrictToWorkspace && strings.TrimSpace(cfg.WorkspaceDir) == "" {
		addWarn("filesystem", "workspace restriction is enabled but workspaceDir is empty")
	}
	return findings
}

func hardeningFindings(cfg config.Config) []doctorFinding {
	findings := []doctorFinding{}
	addWarn := func(area, message string) {
		findings = append(findings, doctorFinding{Level: "warn", Area: area, Message: message})
	}
	if len(cfg.Hardening.ChildEnvAllowlist) == 0 {
		addWarn("env", "child process environment allowlist is empty")
	}
	if !cfg.Hardening.Quotas.Enabled {
		addWarn("quotas", "tool quotas are disabled")
	}
	if cfg.Hardening.Quotas.MaxToolCalls <= 0 || cfg.Hardening.Quotas.MaxExecCalls <= 0 || cfg.Hardening.Quotas.MaxWebCalls <= 0 || cfg.Hardening.Quotas.MaxSubagentCalls <= 0 {
		addWarn("quotas", "one or more quota limits are unset")
	}
	if cfg.Hardening.PrivilegedTools && !cfg.Hardening.Sandbox.Enabled {
		addWarn("privileged-exec", "privileged tools are enabled without Bubblewrap sandboxing")
	}
	if cfg.Hardening.Sandbox.Enabled && strings.TrimSpace(cfg.Hardening.Sandbox.BubblewrapPath) == "" {
		addWarn("privileged-exec", "Bubblewrap sandbox is enabled without a bubblewrapPath")
	}
	return findings
}

func securityFindings(cfg config.Config) []doctorFinding {
	findings := []doctorFinding{}
	addWarn := func(message string) {
		findings = append(findings, doctorFinding{Level: "warn", Area: "security", Message: message})
	}
	if !cfg.Security.Audit.Enabled {
		addWarn("audit logging is disabled")
	} else {
		if !cfg.Security.Audit.Strict {
			addWarn("audit logging is enabled but strict mode is off")
		}
		if !cfg.Security.Audit.VerifyOnStart {
			addWarn("audit logging is enabled but verifyOnStart is off")
		}
	}
	if !cfg.Security.SecretStore.Enabled {
		addWarn("secret store is disabled")
		if hasExternalIntegrations(cfg) {
			addWarn("secret store is disabled while external integrations are enabled")
		}
	} else if !cfg.Security.SecretStore.Required && hasExternalIntegrations(cfg) {
		addWarn("secret store failures are tolerated while channels, webhook, or MCP integrations are enabled")
	}
	if !cfg.Security.Profiles.Enabled {
		addWarn("access profiles are disabled")
	}
	return findings
}

func webhookFindings(cfg config.Config) []doctorFinding {
	findings := []doctorFinding{}
	addWarn := func(message string) {
		findings = append(findings, doctorFinding{Level: "warn", Area: "webhook", Message: message})
	}
	if !cfg.Triggers.Webhook.Enabled {
		return findings
	}
	if strings.TrimSpace(cfg.Triggers.Webhook.Secret) == "" {
		addWarn("webhook is enabled without a secret")
	}
	if !isLoopbackAddr(cfg.Triggers.Webhook.Addr) {
		addWarn("webhook bind address is not loopback-only")
	}
	profileName, profile, ok := resolveEffectiveProfile(cfg, "webhook", "webhook")
	if !ok {
		addWarn("webhook is enabled without an effective access profile")
		if cfg.Hardening.PrivilegedTools {
			addWarn("webhook can reach privileged tools because no access profile applies")
		}
		if cfg.Hardening.GuardedTools {
			addWarn("webhook can reach guarded tools because no access profile applies")
		}
		if cfg.Skills.EnableExec && cfg.Hardening.PrivilegedTools {
			addWarn("webhook can reach skill execution because no access profile applies")
		}
		return findings
	}
	if profileAllowsPrivileged(profile) {
		addWarn(fmt.Sprintf("webhook resolves to profile %q with privileged capability", profileName))
	}
	if profile.AllowSubagents {
		addWarn(fmt.Sprintf("webhook resolves to profile %q with subagents enabled", profileName))
	}
	if len(profile.WritablePaths) > 0 {
		addWarn(fmt.Sprintf("webhook resolves to profile %q with writable paths", profileName))
	}
	if hostListTooBroad(profile.AllowedHosts) {
		addWarn(fmt.Sprintf("webhook resolves to profile %q with broad allowedHosts", profileName))
	}
	if cfg.Hardening.EnableExecShell && cfg.Hardening.PrivilegedTools && profileCanReachExec(profile) {
		addWarn(fmt.Sprintf("webhook can reach exec shell mode via profile %q", profileName))
	}
	if cfg.Skills.EnableExec && profileAllowsPrivileged(profile) && profileAllowsTool(profile, "run_skill_script") {
		addWarn(fmt.Sprintf("webhook can reach skill execution via profile %q", profileName))
	}
	return findings
}

func serviceFindings(cfg config.Config) []doctorFinding {
	findings := []doctorFinding{}
	addWarn := func(message string) {
		findings = append(findings, doctorFinding{Level: "warn", Area: "service", Message: message})
	}
	if !cfg.Service.Enabled {
		return findings
	}
	if strings.TrimSpace(cfg.Service.Secret) == "" {
		addWarn("service mode is enabled without a shared secret")
	} else if len(strings.TrimSpace(cfg.Service.Secret)) < 24 {
		addWarn("service mode is enabled with a weak shared secret")
	}
	if !isLoopbackAddr(cfg.Service.Listen) {
		addWarn("service bind address is not loopback-only")
	}
	return findings
}

func mcpFindings(cfg config.Config) []doctorFinding {
	findings := []doctorFinding{}
	addWarn := func(message string) {
		findings = append(findings, doctorFinding{Level: "warn", Area: "mcp", Message: message})
	}
	if len(cfg.Tools.MCPServers) == 0 {
		return findings
	}
	for name, server := range cfg.Tools.MCPServers {
		if !server.Enabled {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(server.Transport)) {
		case "stdio":
			if len(server.ChildEnvAllowlist) == 0 {
				addWarn(fmt.Sprintf("server %q uses stdio without a server childEnvAllowlist", name))
				if len(cfg.Hardening.ChildEnvAllowlist) == 0 {
					addWarn(fmt.Sprintf("server %q uses stdio with no server or global child environment allowlist", name))
				}
			}
		case "sse", "streamablehttp":
			if server.AllowInsecureHTTP || isInsecureHTTPURL(server.URL) {
				addWarn(fmt.Sprintf("server %q uses insecure HTTP transport", name))
			}
			if !cfg.Security.Network.Enabled || !cfg.Security.Network.DefaultDeny {
				addWarn(fmt.Sprintf("server %q uses remote HTTP transport without deny-by-default network policy", name))
			}
			if hostListTooBroad(cfg.Security.Network.AllowedHosts) {
				addWarn(fmt.Sprintf("server %q relies on a broad network allowlist", name))
			}
		}
	}
	return findings
}

func networkFindings(cfg config.Config) []doctorFinding {
	findings := []doctorFinding{}
	addWarn := func(message string) {
		findings = append(findings, doctorFinding{Level: "warn", Area: "network", Message: message})
	}
	if hostListContainsLiteralStar(cfg.Security.Network.AllowedHosts) {
		addWarn("security.network.allowedHosts contains *")
	}
	if hostListTooBroad(cfg.Security.Network.AllowedHosts) {
		addWarn("security.network.allowedHosts is broad")
	}
	if hasRemoteHTTPMCP(cfg) && (!cfg.Security.Network.Enabled || !cfg.Security.Network.DefaultDeny) {
		addWarn("remote MCP transports are enabled without a meaningful deny-by-default network posture")
	}
	return findings
}

func profileFindings(cfg config.Config) []doctorFinding {
	findings := []doctorFinding{}
	addWarn := func(message string) {
		findings = append(findings, doctorFinding{Level: "warn", Area: "profiles", Message: message})
	}
	if !cfg.Security.Profiles.Enabled {
		if hasPublicIngress(cfg) {
			addWarn("public ingress is enabled while access profiles are disabled")
		}
		if cfg.Triggers.Webhook.Enabled {
			addWarn("webhook is enabled while access profiles are disabled")
		}
		return findings
	}
	if len(cfg.Security.Profiles.Profiles) == 0 {
		addWarn("access profiles are enabled but no profiles are defined")
		return findings
	}
	if strings.TrimSpace(cfg.Security.Profiles.Default) == "" && len(cfg.Security.Profiles.Channels) == 0 && len(cfg.Security.Profiles.Triggers) == 0 {
		addWarn("access profiles are enabled but no default, channel, or trigger mapping is configured")
	}
	if hasPublicIngress(cfg) && strings.TrimSpace(cfg.Security.Profiles.Default) == "" {
		missing := false
		for _, channel := range openAccessChannelNames(cfg) {
			if _, _, ok := resolveEffectiveProfile(cfg, "", channel); !ok {
				missing = true
				break
			}
		}
		if missing {
			addWarn("one or more open-access channels have no effective access profile")
		}
	}
	if cfg.Triggers.Webhook.Enabled {
		if _, _, ok := resolveEffectiveProfile(cfg, "webhook", "webhook"); !ok {
			addWarn("webhook has no effective access profile")
		}
	}
	profileNames := make([]string, 0, len(cfg.Security.Profiles.Profiles))
	for name := range cfg.Security.Profiles.Profiles {
		profileNames = append(profileNames, name)
	}
	sort.Strings(profileNames)
	for _, name := range profileNames {
		profile := cfg.Security.Profiles.Profiles[name]
		if hostListContainsLiteralStar(profile.AllowedHosts) {
			addWarn(fmt.Sprintf("profile %q allowedHosts contains *", name))
		}
		if hostListTooBroad(profile.AllowedHosts) {
			addWarn(fmt.Sprintf("profile %q has broad allowedHosts", name))
		}
		if profileAllowsPrivileged(profile) && len(profile.AllowedTools) == 0 {
			addWarn(fmt.Sprintf("profile %q permits privileged capability without an explicit tool allowlist", name))
		}
	}
	return findings
}

func execFindings(cfg config.Config) []doctorFinding {
	findings := []doctorFinding{}
	addWarn := func(message string) {
		findings = append(findings, doctorFinding{Level: "warn", Area: "exec", Message: message})
	}
	if !cfg.Hardening.PrivilegedTools && !cfg.Hardening.GuardedTools {
		return findings
	}
	if len(cfg.Hardening.ExecAllowedPrograms) == 0 {
		addWarn("exec is enabled without an exec allowlist")
	}
	if len(cfg.Hardening.ChildEnvAllowlist) == 0 {
		addWarn("exec-capable configuration has an empty child environment allowlist")
	}
	if cfg.Hardening.EnableExecShell {
		addWarn("exec shell command mode is enabled; prefer program + args and keep shell mode off unless strictly required")
	}
	if publicIngressCanReachExec(cfg) || webhookCanReachExec(cfg) {
		addWarn("public or webhook-facing ingress can reach privileged exec posture unless profiles deny it")
	}
	return findings
}

func skillFindings(cfg config.Config) []doctorFinding {
	findings := []doctorFinding{}
	addWarn := func(message string) {
		findings = append(findings, doctorFinding{Level: "warn", Area: "skills", Message: message})
	}
	if !cfg.Skills.EnableExec {
		return findings
	}
	if !cfg.Skills.Policy.QuarantineByDefault {
		addWarn("skill execution is enabled while quarantineByDefault is false")
	}
	if len(cfg.Skills.Policy.TrustedOwners) == 0 {
		addWarn("skill execution is enabled without a trustedOwners policy for managed skills")
	}
	if len(cfg.Skills.Policy.TrustedRegistries) == 0 {
		addWarn("skill execution is enabled without a trustedRegistries policy for managed skills")
	}
	if len(cfg.Hardening.ChildEnvAllowlist) == 0 {
		addWarn("skill execution is enabled with an empty child environment allowlist")
	}
	if hasPublicIngress(cfg) && publicIngressCanReachSkillExec(cfg) {
		addWarn("public ingress can reach skill execution through a permissive profile")
	}
	if cfg.Triggers.Webhook.Enabled {
		if _, profile, ok := resolveEffectiveProfile(cfg, "webhook", "webhook"); !ok || (profileAllowsPrivileged(profile) && profileAllowsTool(profile, "run_skill_script")) {
			addWarn("webhook can reach skill execution through a permissive profile")
		}
	}
	return findings
}

func channelExposureFindings(cfg config.Config) []doctorFinding {
	findings := []doctorFinding{}
	add := func(area, message string) {
		findings = append(findings, doctorFinding{Level: "warn", Area: area, Message: message})
	}
	if cfg.Channels.Telegram.Enabled && cfg.Channels.Telegram.OpenAccess {
		add("telegram", "channel is open to any sender")
		appendPublicChannelExposureWarnings(&findings, cfg, "telegram")
	}
	if cfg.Channels.Slack.Enabled && cfg.Channels.Slack.OpenAccess {
		add("slack", "channel is open to any sender")
		appendPublicChannelExposureWarnings(&findings, cfg, "slack")
	}
	if cfg.Channels.Discord.Enabled && cfg.Channels.Discord.OpenAccess {
		add("discord", "channel is open to any sender")
		appendPublicChannelExposureWarnings(&findings, cfg, "discord")
	}
	if cfg.Channels.WhatsApp.Enabled && cfg.Channels.WhatsApp.OpenAccess {
		add("whatsapp", "channel is open to any sender")
		appendPublicChannelExposureWarnings(&findings, cfg, "whatsapp")
	}
	if cfg.Channels.Email.Enabled && cfg.Channels.Email.OpenAccess {
		add("email", "channel is open to any sender")
		appendPublicChannelExposureWarnings(&findings, cfg, "email")
	}
	return findings
}

func appendPublicChannelExposureWarnings(findings *[]doctorFinding, cfg config.Config, channel string) {
	add := func(message string) {
		*findings = append(*findings, doctorFinding{Level: "warn", Area: channel, Message: message})
	}
	profileName, profile, ok := resolveEffectiveProfile(cfg, "", channel)
	if !ok {
		if cfg.Hardening.PrivilegedTools {
			add("open-access channel can reach privileged tools because no access profile applies")
		}
		if cfg.Hardening.GuardedTools {
			add("open-access channel can reach guarded tools because no access profile applies")
		}
		if cfg.Skills.EnableExec && cfg.Hardening.PrivilegedTools {
			add("open-access channel can reach skill execution because no access profile applies")
		}
		return
	}
	if profileAllowsPrivileged(profile) {
		add(fmt.Sprintf("open-access channel resolves to profile %q with privileged capability", profileName))
	}
	if cfg.Hardening.GuardedTools && !profileHasMeaningfulToolRestriction(profile) {
		add(fmt.Sprintf("open-access channel resolves to profile %q without a meaningful tool restriction", profileName))
	}
	if cfg.Hardening.EnableExecShell && cfg.Hardening.PrivilegedTools && profileCanReachExec(profile) {
		add(fmt.Sprintf("open-access channel can reach exec shell mode via profile %q", profileName))
	}
	if cfg.Skills.EnableExec && profileAllowsPrivileged(profile) && profileAllowsTool(profile, "run_skill_script") {
		add(fmt.Sprintf("open-access channel can reach skill execution via profile %q", profileName))
	}
}

func runtimeProfileFindings(cfg config.Config) []doctorFinding {
	findings := []doctorFinding{}
	addWarn := func(message string) {
		findings = append(findings, doctorFinding{Level: "warn", Area: "runtime-profile", Message: message})
	}

	p := cfg.RuntimeProfile
	if p == "" {
		addWarn("runtimeProfile is not set; consider setting it to one of: local-dev, single-user-hardened, hosted-service, hosted-no-exec, hosted-remote-sandbox-only")
		return findings
	}

	if config.IsHostedProfile(p) {
		if !cfg.Security.SecretStore.Enabled {
			addWarn("hosted profile requires security.secretStore.enabled")
		}
		if !cfg.Security.Audit.Enabled {
			addWarn("hosted profile requires security.audit.enabled")
		}
		if !cfg.Security.Network.Enabled && !cfg.Security.Network.DefaultDeny {
			addWarn("hosted profile should configure security.network outbound policy")
		}
	}

	if p == config.ProfileHostedNoExec {
		if cfg.Hardening.EnableExecShell {
			addWarn("hosted-no-exec profile: enableExecShell should be false")
		}
		if cfg.Hardening.PrivilegedTools {
			addWarn("hosted-no-exec profile: privilegedTools should be false")
		}
	}

	if p == config.ProfileHostedRemoteSandbox {
		if cfg.Hardening.EnableExecShell && !cfg.Hardening.Sandbox.Enabled {
			addWarn("hosted-remote-sandbox-only profile: exec requires sandbox to be enabled")
		}
	}

	return findings
}

func hasDoctorWarnings(findings []doctorFinding) bool {
	for _, finding := range findings {
		if strings.EqualFold(finding.Level, "warn") || strings.EqualFold(finding.Level, "error") {
			return true
		}
	}
	return false
}

func isLoopbackAddr(addr string) bool {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return true
	}
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}
	host = strings.Trim(host, "[]")
	if host == "" || strings.EqualFold(host, "localhost") {
		return true
	}
	parsed := net.ParseIP(host)
	return parsed != nil && parsed.IsLoopback()
}

func hasExternalIntegrations(cfg config.Config) bool {
	return cfg.Triggers.Webhook.Enabled || anyEnabledChannels(cfg) || anyEnabledMCPServers(cfg)
}

func anyEnabledChannels(cfg config.Config) bool {
	return cfg.Channels.Telegram.Enabled || cfg.Channels.Slack.Enabled || cfg.Channels.Discord.Enabled || cfg.Channels.WhatsApp.Enabled || cfg.Channels.Email.Enabled
}

func anyEnabledMCPServers(cfg config.Config) bool {
	for _, server := range cfg.Tools.MCPServers {
		if server.Enabled {
			return true
		}
	}
	return false
}

func hasPublicIngress(cfg config.Config) bool {
	return len(openAccessChannelNames(cfg)) > 0
}

func openAccessChannelNames(cfg config.Config) []string {
	channels := []string{}
	if cfg.Channels.Telegram.Enabled && cfg.Channels.Telegram.OpenAccess {
		channels = append(channels, "telegram")
	}
	if cfg.Channels.Slack.Enabled && cfg.Channels.Slack.OpenAccess {
		channels = append(channels, "slack")
	}
	if cfg.Channels.Discord.Enabled && cfg.Channels.Discord.OpenAccess {
		channels = append(channels, "discord")
	}
	if cfg.Channels.WhatsApp.Enabled && cfg.Channels.WhatsApp.OpenAccess {
		channels = append(channels, "whatsapp")
	}
	if cfg.Channels.Email.Enabled && cfg.Channels.Email.OpenAccess {
		channels = append(channels, "email")
	}
	return channels
}

func resolveEffectiveProfile(cfg config.Config, trigger, channel string) (string, config.AccessProfileConfig, bool) {
	if !cfg.Security.Profiles.Enabled {
		return "", config.AccessProfileConfig{}, false
	}
	if profileName := strings.TrimSpace(cfg.Security.Profiles.Triggers[strings.ToLower(strings.TrimSpace(trigger))]); profileName != "" {
		profile, ok := cfg.Security.Profiles.Profiles[profileName]
		return profileName, profile, ok
	}
	if profileName := strings.TrimSpace(cfg.Security.Profiles.Channels[strings.ToLower(strings.TrimSpace(channel))]); profileName != "" {
		profile, ok := cfg.Security.Profiles.Profiles[profileName]
		return profileName, profile, ok
	}
	profileName := strings.TrimSpace(cfg.Security.Profiles.Default)
	if profileName == "" {
		return "", config.AccessProfileConfig{}, false
	}
	profile, ok := cfg.Security.Profiles.Profiles[profileName]
	return profileName, profile, ok
}

func profileAllowsPrivileged(profile config.AccessProfileConfig) bool {
	maxCapability := strings.ToLower(strings.TrimSpace(profile.MaxCapability))
	return maxCapability == "" || maxCapability == "privileged"
}

func profileAllowsGuarded(profile config.AccessProfileConfig) bool {
	maxCapability := strings.ToLower(strings.TrimSpace(profile.MaxCapability))
	return maxCapability == "" || maxCapability == "privileged" || maxCapability == "guarded"
}

func profileAllowsTool(profile config.AccessProfileConfig, toolName string) bool {
	if len(profile.AllowedTools) == 0 {
		return true
	}
	toolName = strings.TrimSpace(toolName)
	for _, allowed := range profile.AllowedTools {
		if strings.TrimSpace(allowed) == toolName {
			return true
		}
	}
	return false
}

func profileHasMeaningfulToolRestriction(profile config.AccessProfileConfig) bool {
	return !profileAllowsGuarded(profile) || len(profile.AllowedTools) > 0
}

func profileCanReachExec(profile config.AccessProfileConfig) bool {
	return profileAllowsPrivileged(profile) && profileAllowsTool(profile, "exec")
}

func hostListContainsLiteralStar(hosts []string) bool {
	for _, host := range hosts {
		if strings.TrimSpace(host) == "*" {
			return true
		}
	}
	return false
}

func hostListTooBroad(hosts []string) bool {
	if len(hosts) > 10 {
		return true
	}
	for _, host := range hosts {
		host = strings.TrimSpace(strings.ToLower(host))
		if host == "*" {
			return true
		}
		if strings.Contains(host, "*") && !strings.HasPrefix(host, "*.") {
			return true
		}
		if strings.HasPrefix(host, "*.") {
			return true
		}
	}
	return false
}

func isInsecureHTTPURL(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return false
	}
	return strings.EqualFold(u.Scheme, "http")
}

func hasRemoteHTTPMCP(cfg config.Config) bool {
	for _, server := range cfg.Tools.MCPServers {
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
		if !isLoopbackAddr(u.Hostname()) {
			return true
		}
	}
	return false
}

func publicIngressCanReachSkillExec(cfg config.Config) bool {
	for _, channel := range openAccessChannelNames(cfg) {
		_, profile, ok := resolveEffectiveProfile(cfg, "", channel)
		if !ok {
			return cfg.Hardening.PrivilegedTools
		}
		if profileAllowsPrivileged(profile) && profileAllowsTool(profile, "run_skill_script") {
			return true
		}
	}
	return false
}

func publicIngressCanReachExec(cfg config.Config) bool {
	if !cfg.Hardening.EnableExecShell || !cfg.Hardening.PrivilegedTools {
		return false
	}
	for _, channel := range openAccessChannelNames(cfg) {
		_, profile, ok := resolveEffectiveProfile(cfg, "", channel)
		if !ok {
			return true
		}
		if profileCanReachExec(profile) {
			return true
		}
	}
	return false
}

func webhookCanReachExec(cfg config.Config) bool {
	if !cfg.Hardening.EnableExecShell || !cfg.Hardening.PrivilegedTools || !cfg.Triggers.Webhook.Enabled {
		return false
	}
	_, profile, ok := resolveEffectiveProfile(cfg, "webhook", "webhook")
	if !ok {
		return true
	}
	return profileCanReachExec(profile)
}
