package doctor

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"or3-intern/internal/config"
	"or3-intern/internal/db"
)

type Options struct {
	Mode            Mode
	ConfigPath      string
	ValidationError string
	Probe           bool
}

func Evaluate(cfg config.Config, opts Options) Report {
	if opts.Mode == "" {
		opts.Mode = ModeAdvisory
	}
	findings := make([]Finding, 0, 48)
	findings = append(findings, configValidationFindings(cfg, opts)...)
	findings = append(findings, filesystemFindings(cfg, opts)...)
	findings = append(findings, hardeningFindings(cfg, opts)...)
	findings = append(findings, securityFindings(cfg, opts)...)
	findings = append(findings, approvalFindings(cfg, opts)...)
	findings = append(findings, webhookFindings(cfg, opts)...)
	findings = append(findings, serviceFindings(cfg, opts)...)
	findings = append(findings, mcpFindings(cfg, opts)...)
	findings = append(findings, networkFindings(cfg, opts)...)
	findings = append(findings, profileFindings(cfg, opts)...)
	findings = append(findings, execFindings(cfg, opts)...)
	findings = append(findings, skillFindings(cfg, opts)...)
	findings = append(findings, channelExposureFindings(cfg, opts)...)
	findings = append(findings, channelIngressFindings(cfg, opts)...)
	findings = append(findings, runtimeProfileFindings(cfg, opts)...)
	if opts.Probe {
		findings = append(findings, probeFindings(cfg, opts)...)
	}
	return NewReport(opts.Mode, findings)
}

func severityFor(mode Mode, advisory Severity, blockOnStartup bool) Severity {
	if blockOnStartup && (mode == ModeStartupChat || mode == ModeStartupServe || mode == ModeStartupService) {
		return SeverityBlock
	}
	return advisory
}

func severityForConfigureOrStartup(mode Mode, advisory Severity) Severity {
	if mode == ModeConfigurePostSave || mode == ModeStartupChat || mode == ModeStartupServe || mode == ModeStartupService {
		return SeverityBlock
	}
	return advisory
}

func configValidationFindings(cfg config.Config, opts Options) []Finding {
	findings := []Finding{}
	if strings.TrimSpace(opts.ValidationError) != "" {
		findings = append(findings, Finding{
			ID:       "config.validation.load",
			Area:     "config",
			Severity: severityForConfigureOrStartup(opts.Mode, SeverityError),
			Summary:  opts.ValidationError,
			Detail:   "The config was loaded in repair mode because normal validation failed. Fix the reported fields before startup.",
			FixMode:  FixModeInteractive,
			FixHint:  "Run `or3-intern doctor --fix --interactive` or `or3-intern configure`.",
		})
	}
	if err := config.ValidateProfile(cfg); err != nil {
		findings = append(findings, Finding{
			ID:       "runtime-profile.validation",
			Area:     "runtime-profile",
			Severity: severityForConfigureOrStartup(opts.Mode, SeverityError),
			Summary:  err.Error(),
			Detail:   "The selected runtime profile contradicts the rest of the configuration.",
			FixMode:  FixModeManual,
			FixHint:  "Adjust the runtime profile or the dependent security settings.",
		})
	}
	if strings.TrimSpace(opts.ValidationError) == "" {
		if err := validateConfigSnapshot(cfg); err != nil {
			findings = append(findings, Finding{
				ID:       "config.validation.snapshot",
				Area:     "config",
				Severity: severityForConfigureOrStartup(opts.Mode, SeverityError),
				Summary:  err.Error(),
				Detail:   "The current in-memory config cannot pass full validation.",
				FixMode:  FixModeInteractive,
				FixHint:  "Repair the invalid config fields before startup.",
			})
		}
	}
	return findings
}

func filesystemFindings(cfg config.Config, opts Options) []Finding {
	findings := []Finding{}
	if !cfg.Tools.RestrictToWorkspace {
		findings = append(findings, Finding{
			ID:       "filesystem.workspace_restriction_disabled",
			Area:     "filesystem",
			Severity: SeverityWarn,
			Summary:  "workspace restriction is disabled",
			Detail:   "File tools are not bounded to a workspace directory.",
			FixMode:  FixModeManual,
			FixHint:  "Enable workspace restriction or explicitly scope writable paths.",
		})
	}
	if cfg.Tools.RestrictToWorkspace && strings.TrimSpace(cfg.WorkspaceDir) == "" {
		findings = append(findings, Finding{
			ID:       "filesystem.workspace_dir_empty",
			Area:     "filesystem",
			Severity: severityFor(opts.Mode, SeverityError, false),
			Summary:  "workspace restriction is enabled but workspaceDir is empty",
			Detail:   "Restricted file tools need a concrete workspace root.",
			FixMode:  FixModeManual,
			FixHint:  "Set tools.restrictToWorkspace=false or configure workspaceDir.",
		})
	}
	if cfg.Tools.RestrictToWorkspace && strings.TrimSpace(cfg.WorkspaceDir) != "" {
		if _, err := os.Stat(cfg.WorkspaceDir); err != nil {
			findings = append(findings, Finding{
				ID:       "filesystem.workspace_dir_missing",
				Area:     "filesystem",
				Severity: severityFor(opts.Mode, SeverityWarn, false),
				Summary:  "workspaceDir does not exist on disk",
				Detail:   cfg.WorkspaceDir,
				FixMode:  FixModeManual,
				FixHint:  "Create the workspace directory or point workspaceDir at an existing path.",
			})
		}
	}
	dbDir := filepath.Dir(strings.TrimSpace(cfg.DBPath))
	if dbDir != "" {
		if _, err := os.Stat(dbDir); err != nil {
			findings = append(findings, Finding{
				ID:       "filesystem.db_parent_missing",
				Area:     "filesystem",
				Severity: SeverityWarn,
				Summary:  "database parent directory does not exist",
				Detail:   dbDir,
				FixMode:  FixModeAutomatic,
				FixHint:  "Create the database directory.",
			})
		}
	}
	artifactsDir := strings.TrimSpace(cfg.ArtifactsDir)
	if artifactsDir == "" {
		findings = append(findings, Finding{
			ID:       "filesystem.artifacts_dir_empty",
			Area:     "filesystem",
			Severity: severityFor(opts.Mode, SeverityError, false),
			Summary:  "artifacts directory is empty",
			Detail:   "Artifacts are needed for channels, media, and runtime outputs.",
			FixMode:  FixModeManual,
			FixHint:  "Set artifactsDir to an existing or creatable directory.",
		})
	} else if _, err := os.Stat(artifactsDir); err != nil {
		findings = append(findings, Finding{
			ID:       "filesystem.artifacts_dir_missing",
			Area:     "filesystem",
			Severity: SeverityWarn,
			Summary:  "artifacts directory does not exist",
			Detail:   artifactsDir,
			FixMode:  FixModeAutomatic,
			FixHint:  "Create the artifacts directory.",
		})
	}
	return findings
}

func hardeningFindings(cfg config.Config, opts Options) []Finding {
	findings := []Finding{}
	if len(cfg.Hardening.ChildEnvAllowlist) == 0 {
		findings = append(findings, Finding{
			ID:       "env.child_allowlist_empty",
			Area:     "env",
			Severity: SeverityWarn,
			Summary:  "child process environment allowlist is empty",
			Detail:   "Subprocesses may inherit less predictable environment state.",
			FixMode:  FixModeManual,
		})
	}
	if !cfg.Hardening.Quotas.Enabled {
		findings = append(findings, Finding{
			ID:       "quotas.disabled",
			Area:     "quotas",
			Severity: SeverityWarn,
			Summary:  "tool quotas are disabled",
			Detail:   "Per-turn safety limits are disabled.",
			FixMode:  FixModeManual,
		})
	}
	if cfg.Hardening.Quotas.MaxToolCalls <= 0 || cfg.Hardening.Quotas.MaxExecCalls <= 0 || cfg.Hardening.Quotas.MaxWebCalls <= 0 || cfg.Hardening.Quotas.MaxSubagentCalls <= 0 {
		findings = append(findings, Finding{
			ID:       "quotas.unset",
			Area:     "quotas",
			Severity: SeverityWarn,
			Summary:  "one or more quota limits are unset",
			Detail:   "Quota values should be positive when quotas are enabled.",
			FixMode:  FixModeAutomatic,
			FixHint:  "Restore the default hardening quotas.",
		})
	}
	if cfg.Hardening.PrivilegedTools && !cfg.Hardening.Sandbox.Enabled {
		findings = append(findings, Finding{
			ID:       "privileged-exec.sandbox_disabled",
			Area:     "privileged-exec",
			Severity: severityFor(opts.Mode, SeverityWarn, isHostedOrStartupMode(cfg, opts.Mode)),
			Summary:  "privileged tools are enabled without Bubblewrap sandboxing",
			Detail:   "Privileged exec-capable tools should run under Bubblewrap in hardened setups.",
			FixMode:  FixModeManual,
			FixHint:  "Enable hardening.sandbox or disable privileged tools.",
		})
	}
	if cfg.Hardening.Sandbox.Enabled && strings.TrimSpace(cfg.Hardening.Sandbox.BubblewrapPath) == "" {
		findings = append(findings, Finding{
			ID:       "privileged-exec.bubblewrap_path_empty",
			Area:     "privileged-exec",
			Severity: SeverityError,
			Summary:  "Bubblewrap sandbox is enabled without a bubblewrapPath",
			Detail:   "Sandboxing cannot start without a bubblewrap binary path.",
			FixMode:  FixModeAutomatic,
			FixHint:  "Set hardening.sandbox.bubblewrapPath to `bwrap`.",
		})
	}
	if cfg.Hardening.Sandbox.Enabled && strings.TrimSpace(cfg.Hardening.Sandbox.BubblewrapPath) != "" {
		if _, err := exec.LookPath(strings.TrimSpace(cfg.Hardening.Sandbox.BubblewrapPath)); err != nil {
			findings = append(findings, Finding{
				ID:       "privileged-exec.bubblewrap_missing",
				Area:     "privileged-exec",
				Severity: severityFor(opts.Mode, SeverityError, isHostedOrStartupMode(cfg, opts.Mode)),
				Summary:  "configured Bubblewrap binary is not available",
				Detail:   strings.TrimSpace(cfg.Hardening.Sandbox.BubblewrapPath),
				FixMode:  FixModeManual,
				FixHint:  "Install Bubblewrap or update hardening.sandbox.bubblewrapPath.",
			})
		}
	}
	return findings
}

func securityFindings(cfg config.Config, opts Options) []Finding {
	findings := []Finding{}
	if !cfg.Security.Audit.Enabled {
		findings = append(findings, Finding{
			ID:       "security.audit_disabled",
			Area:     "security",
			Severity: SeverityWarn,
			Summary:  "audit logging is disabled",
			Detail:   "Sensitive operations are not written to the append-only audit chain.",
			FixMode:  FixModeManual,
		})
	} else {
		if !cfg.Security.Audit.Strict {
			findings = append(findings, Finding{
				ID:       "security.audit_not_strict",
				Area:     "security",
				Severity: SeverityWarn,
				Summary:  "audit logging is enabled but strict mode is off",
				Detail:   "Audit write failures will not fail closed.",
				FixMode:  FixModeManual,
			})
		}
		if !cfg.Security.Audit.VerifyOnStart {
			findings = append(findings, Finding{
				ID:       "security.audit_no_verify_on_start",
				Area:     "security",
				Severity: SeverityWarn,
				Summary:  "audit logging is enabled but verifyOnStart is off",
				Detail:   "The audit chain is not verified automatically at startup.",
				FixMode:  FixModeManual,
			})
		}
		if keyFinding := keyFileFinding("security.audit.key_missing", "security", cfg.Security.Audit.KeyFile, "audit key file is missing", FixModeAutomatic); keyFinding != nil {
			findings = append(findings, *keyFinding)
		}
	}
	if !cfg.Security.SecretStore.Enabled {
		findings = append(findings, Finding{
			ID:       "security.secret_store_disabled",
			Area:     "security",
			Severity: SeverityWarn,
			Summary:  "secret store is disabled",
			Detail:   "Provider and integration secrets cannot be stored in encrypted local secret storage.",
			FixMode:  FixModeManual,
		})
		if hasExternalIntegrations(cfg) {
			findings = append(findings, Finding{
				ID:       "security.secret_store_disabled_with_integrations",
				Area:     "security",
				Severity: SeverityWarn,
				Summary:  "secret store is disabled while external integrations are enabled",
				Detail:   "External integrations increase the value of local secret protection.",
				FixMode:  FixModeInteractive,
				FixHint:  "Enable the secret store and generate a key file.",
			})
		}
	} else {
		if !cfg.Security.SecretStore.Required && hasExternalIntegrations(cfg) {
			findings = append(findings, Finding{
				ID:       "security.secret_store_not_required",
				Area:     "security",
				Severity: SeverityWarn,
				Summary:  "secret store failures are tolerated while external integrations are enabled",
				Detail:   "The runtime may continue with weaker secret handling if the secret store becomes unavailable.",
				FixMode:  FixModeManual,
			})
		}
		if keyFinding := keyFileFinding("security.secret_store.key_missing", "security", cfg.Security.SecretStore.KeyFile, "secret-store key file is missing", FixModeAutomatic); keyFinding != nil {
			findings = append(findings, *keyFinding)
		}
	}
	if !cfg.Security.Profiles.Enabled {
		findings = append(findings, Finding{
			ID:       "security.profiles_disabled",
			Area:     "security",
			Severity: SeverityWarn,
			Summary:  "access profiles are disabled",
			Detail:   "External ingress and automation run without a profile boundary.",
			FixMode:  FixModeManual,
		})
	}
	return findings
}

func approvalFindings(cfg config.Config, opts Options) []Finding {
	findings := []Finding{}
	if !cfg.Security.Approvals.Enabled {
		return findings
	}
	if approvalBrokerRequired(cfg) && strings.TrimSpace(cfg.Security.Approvals.KeyFile) == "" {
		findings = append(findings, Finding{
			ID:       "approvals.key_missing",
			Area:     "approvals",
			Severity: severityFor(opts.Mode, SeverityWarn, opts.Mode == ModeStartupService || opts.Mode == ModeStartupServe),
			Summary:  "approval broker keyFile is required when approvals use ask or allowlist mode",
			Detail:   "Approval tokens and pairing codes need a signing key.",
			FixMode:  FixModeAutomatic,
			FixHint:  "Generate the configured approvals key file.",
		})
	}
	if keyFinding := keyFileFinding("approvals.key_path_missing", "approvals", cfg.Security.Approvals.KeyFile, "approval key file is missing", FixModeAutomatic); keyFinding != nil && cfg.Security.Approvals.Enabled {
		findings = append(findings, *keyFinding)
	}
	if cfg.Service.Enabled && !isLoopbackAddr(cfg.Service.Listen) && approvalBrokerRequired(cfg) && strings.TrimSpace(cfg.Security.Approvals.KeyFile) == "" {
		findings = append(findings, Finding{
			ID:       "approvals.public_service_without_key",
			Area:     "approvals",
			Severity: severityFor(opts.Mode, SeverityWarn, opts.Mode == ModeStartupService),
			Summary:  "service mode is exposed beyond loopback while approvals require a broker keyFile",
			Detail:   "Remote approval and pairing flows should not run without an approval signing key.",
			FixMode:  FixModeAutomatic,
		})
	}
	return findings
}

func webhookFindings(cfg config.Config, opts Options) []Finding {
	findings := []Finding{}
	if !cfg.Triggers.Webhook.Enabled {
		return findings
	}
	if strings.TrimSpace(cfg.Triggers.Webhook.Secret) == "" {
		findings = append(findings, Finding{
			ID:       "webhook.secret_missing",
			Area:     "webhook",
			Severity: severityFor(opts.Mode, SeverityWarn, opts.Mode == ModeStartupServe),
			Summary:  "webhook is enabled without a secret",
			Detail:   "Webhook ingress should be authenticated with a shared secret.",
			FixMode:  FixModeInteractive,
			FixHint:  "Generate a strong webhook secret or disable the webhook.",
		})
	}
	if !isLoopbackAddr(cfg.Triggers.Webhook.Addr) {
		findings = append(findings, Finding{
			ID:       "webhook.public_bind",
			Area:     "webhook",
			Severity: severityFor(opts.Mode, SeverityWarn, opts.Mode == ModeStartupServe && config.IsHostedProfile(cfg.RuntimeProfile)),
			Summary:  "webhook bind address is not loopback-only",
			Detail:   cfg.Triggers.Webhook.Addr,
			FixMode:  fixModeForBind(cfg.RuntimeProfile),
			FixHint:  "Bind the webhook listener to loopback unless you are intentionally exposing it.",
		})
	}
	profileName, profile, ok := resolveEffectiveProfile(cfg, "webhook", "webhook")
	if !ok {
		findings = append(findings, Finding{
			ID:       "webhook.profile_missing",
			Area:     "webhook",
			Severity: severityFor(opts.Mode, SeverityWarn, opts.Mode == ModeStartupServe),
			Summary:  "webhook is enabled without an effective access profile",
			Detail:   "Webhook turns should resolve to a bounded access profile.",
			FixMode:  FixModeInteractive,
			FixHint:  "Create or map an access profile for the webhook.",
		})
		if cfg.Hardening.PrivilegedTools {
			findings = append(findings, Finding{
				ID:       "webhook.privileged_without_profile",
				Area:     "webhook",
				Severity: severityFor(opts.Mode, SeverityWarn, opts.Mode == ModeStartupServe),
				Summary:  "webhook can reach privileged tools because no access profile applies",
			})
		}
		if cfg.Hardening.GuardedTools {
			findings = append(findings, Finding{
				ID:       "webhook.guarded_without_profile",
				Area:     "webhook",
				Severity: severityFor(opts.Mode, SeverityWarn, opts.Mode == ModeStartupServe),
				Summary:  "webhook can reach guarded tools because no access profile applies",
			})
		}
		if cfg.Skills.EnableExec && cfg.Hardening.PrivilegedTools {
			findings = append(findings, Finding{
				ID:       "webhook.skills_without_profile",
				Area:     "webhook",
				Severity: severityFor(opts.Mode, SeverityWarn, opts.Mode == ModeStartupServe),
				Summary:  "webhook can reach skill execution because no access profile applies",
			})
		}
		return findings
	}
	if profileAllowsPrivileged(profile) {
		findings = append(findings, Finding{
			ID:       "webhook.profile_privileged",
			Area:     "webhook",
			Severity: severityFor(opts.Mode, SeverityWarn, opts.Mode == ModeStartupServe),
			Summary:  fmt.Sprintf("webhook resolves to profile %q with privileged capability", profileName),
		})
	}
	if profile.AllowSubagents {
		findings = append(findings, Finding{
			ID:       "webhook.profile_subagents",
			Area:     "webhook",
			Severity: SeverityWarn,
			Summary:  fmt.Sprintf("webhook resolves to profile %q with subagents enabled", profileName),
		})
	}
	if len(profile.WritablePaths) > 0 {
		findings = append(findings, Finding{
			ID:       "webhook.profile_writable_paths",
			Area:     "webhook",
			Severity: SeverityWarn,
			Summary:  fmt.Sprintf("webhook resolves to profile %q with writable paths", profileName),
		})
	}
	if hostListTooBroad(profile.AllowedHosts) {
		findings = append(findings, Finding{
			ID:       "webhook.profile_broad_hosts",
			Area:     "webhook",
			Severity: SeverityWarn,
			Summary:  fmt.Sprintf("webhook resolves to profile %q with broad allowedHosts", profileName),
		})
	}
	if cfg.Hardening.EnableExecShell && cfg.Hardening.PrivilegedTools && profileCanReachExec(profile) {
		findings = append(findings, Finding{
			ID:       "webhook.exec_shell_exposure",
			Area:     "webhook",
			Severity: severityFor(opts.Mode, SeverityWarn, opts.Mode == ModeStartupServe),
			Summary:  fmt.Sprintf("webhook can reach exec shell mode via profile %q", profileName),
		})
	}
	if cfg.Skills.EnableExec && profileAllowsPrivileged(profile) && profileAllowsTool(profile, "run_skill_script") {
		findings = append(findings, Finding{
			ID:       "webhook.skill_exec_exposure",
			Area:     "webhook",
			Severity: severityFor(opts.Mode, SeverityWarn, opts.Mode == ModeStartupServe),
			Summary:  fmt.Sprintf("webhook can reach skill execution via profile %q", profileName),
		})
	}
	return findings
}

func serviceFindings(cfg config.Config, opts Options) []Finding {
	findings := []Finding{}
	if !cfg.Service.Enabled && opts.Mode != ModeStartupService {
		return findings
	}
	secret := strings.TrimSpace(cfg.Service.Secret)
	if secret == "" {
		findings = append(findings, Finding{
			ID:       "service.secret_missing",
			Area:     "service",
			Severity: severityFor(opts.Mode, SeverityWarn, opts.Mode == ModeStartupService && config.IsHostedProfile(cfg.RuntimeProfile)),
			Summary:  "service mode is enabled without a shared secret",
			Detail:   "The internal service API should not run without authentication.",
			FixMode:  FixModeInteractive,
			FixHint:  "Generate a strong service secret or disable service mode.",
		})
	} else if len(secret) < 24 {
		findings = append(findings, Finding{
			ID:       "service.secret_weak",
			Area:     "service",
			Severity: severityFor(opts.Mode, SeverityWarn, opts.Mode == ModeStartupService && config.IsHostedProfile(cfg.RuntimeProfile)),
			Summary:  "service mode is enabled with a weak shared secret",
			Detail:   "Use at least a 24-character random secret for service auth.",
			FixMode:  FixModeInteractive,
			FixHint:  "Generate a stronger service secret.",
		})
	}
	if !isLoopbackAddr(cfg.Service.Listen) {
		findings = append(findings, Finding{
			ID:       "service.public_bind",
			Area:     "service",
			Severity: severityFor(opts.Mode, SeverityWarn, opts.Mode == ModeStartupService && config.IsHostedProfile(cfg.RuntimeProfile)),
			Summary:  "service bind address is not loopback-only",
			Detail:   cfg.Service.Listen,
			FixMode:  fixModeForBind(cfg.RuntimeProfile),
			FixHint:  "Bind the service to loopback unless you intentionally expose it behind a hardened deployment.",
		})
	}
	return findings
}

func mcpFindings(cfg config.Config, opts Options) []Finding {
	findings := []Finding{}
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
				findings = append(findings, Finding{
					ID:       "mcp.stdio_child_env_missing",
					Area:     "mcp",
					Severity: SeverityWarn,
					Summary:  fmt.Sprintf("server %q uses stdio without a server childEnvAllowlist", name),
				})
				if len(cfg.Hardening.ChildEnvAllowlist) == 0 {
					findings = append(findings, Finding{
						ID:       "mcp.stdio_global_child_env_missing",
						Area:     "mcp",
						Severity: SeverityWarn,
						Summary:  fmt.Sprintf("server %q uses stdio with no server or global child environment allowlist", name),
					})
				}
			}
		case "sse", "streamablehttp":
			if server.AllowInsecureHTTP || isInsecureHTTPURL(server.URL) {
				findings = append(findings, Finding{
					ID:       "mcp.http_insecure",
					Area:     "mcp",
					Severity: SeverityWarn,
					Summary:  fmt.Sprintf("server %q uses insecure HTTP transport", name),
				})
			}
			if !cfg.Security.Network.Enabled || !cfg.Security.Network.DefaultDeny {
				findings = append(findings, Finding{
					ID:       "mcp.http_no_default_deny",
					Area:     "mcp",
					Severity: severityFor(opts.Mode, SeverityWarn, isHostedOrStartupMode(cfg, opts.Mode)),
					Summary:  fmt.Sprintf("server %q uses remote HTTP transport without deny-by-default network policy", name),
				})
			}
			if hostListTooBroad(cfg.Security.Network.AllowedHosts) {
				findings = append(findings, Finding{
					ID:       "mcp.http_broad_allowlist",
					Area:     "mcp",
					Severity: severityFor(opts.Mode, SeverityWarn, isHostedOrStartupMode(cfg, opts.Mode)),
					Summary:  fmt.Sprintf("server %q relies on a broad network allowlist", name),
				})
			}
		}
	}
	return findings
}

func networkFindings(cfg config.Config, opts Options) []Finding {
	findings := []Finding{}
	if hostListContainsLiteralStar(cfg.Security.Network.AllowedHosts) {
		findings = append(findings, Finding{
			ID:       "network.literal_star",
			Area:     "network",
			Severity: severityFor(opts.Mode, SeverityWarn, isHostedOrStartupMode(cfg, opts.Mode)),
			Summary:  "security.network.allowedHosts contains *",
		})
	}
	if hostListTooBroad(cfg.Security.Network.AllowedHosts) {
		findings = append(findings, Finding{
			ID:       "network.broad_allowlist",
			Area:     "network",
			Severity: severityFor(opts.Mode, SeverityWarn, isHostedOrStartupMode(cfg, opts.Mode)),
			Summary:  "security.network.allowedHosts is broad",
		})
	}
	if hasRemoteHTTPMCP(cfg) && (!cfg.Security.Network.Enabled || !cfg.Security.Network.DefaultDeny) {
		findings = append(findings, Finding{
			ID:       "network.remote_mcp_without_default_deny",
			Area:     "network",
			Severity: severityFor(opts.Mode, SeverityWarn, isHostedOrStartupMode(cfg, opts.Mode)),
			Summary:  "remote MCP transports are enabled without a meaningful deny-by-default network posture",
		})
	}
	return findings
}

func profileFindings(cfg config.Config, opts Options) []Finding {
	findings := []Finding{}
	if !cfg.Security.Profiles.Enabled {
		if hasPublicIngress(cfg) {
			findings = append(findings, Finding{
				ID:       "profiles.public_ingress_without_profiles",
				Area:     "profiles",
				Severity: severityFor(opts.Mode, SeverityWarn, opts.Mode == ModeStartupServe),
				Summary:  "public ingress is enabled while access profiles are disabled",
				FixMode:  FixModeInteractive,
			})
		}
		if cfg.Triggers.Webhook.Enabled {
			findings = append(findings, Finding{
				ID:       "profiles.webhook_without_profiles",
				Area:     "profiles",
				Severity: severityFor(opts.Mode, SeverityWarn, opts.Mode == ModeStartupServe),
				Summary:  "webhook is enabled while access profiles are disabled",
				FixMode:  FixModeInteractive,
			})
		}
		return findings
	}
	if len(cfg.Security.Profiles.Profiles) == 0 {
		findings = append(findings, Finding{
			ID:       "profiles.empty",
			Area:     "profiles",
			Severity: SeverityWarn,
			Summary:  "access profiles are enabled but no profiles are defined",
			FixMode:  FixModeInteractive,
		})
		return findings
	}
	if strings.TrimSpace(cfg.Security.Profiles.Default) == "" && len(cfg.Security.Profiles.Channels) == 0 && len(cfg.Security.Profiles.Triggers) == 0 {
		findings = append(findings, Finding{
			ID:       "profiles.no_mapping",
			Area:     "profiles",
			Severity: SeverityWarn,
			Summary:  "access profiles are enabled but no default, channel, or trigger mapping is configured",
			FixMode:  FixModeInteractive,
		})
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
			findings = append(findings, Finding{
				ID:       "profiles.open_ingress_profile_missing",
				Area:     "profiles",
				Severity: severityFor(opts.Mode, SeverityWarn, opts.Mode == ModeStartupServe),
				Summary:  "one or more open-access channels have no effective access profile",
				FixMode:  FixModeInteractive,
			})
		}
	}
	if cfg.Triggers.Webhook.Enabled {
		if _, _, ok := resolveEffectiveProfile(cfg, "webhook", "webhook"); !ok {
			findings = append(findings, Finding{
				ID:       "profiles.webhook_effective_missing",
				Area:     "profiles",
				Severity: severityFor(opts.Mode, SeverityWarn, opts.Mode == ModeStartupServe),
				Summary:  "webhook has no effective access profile",
				FixMode:  FixModeInteractive,
			})
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
			findings = append(findings, Finding{
				ID:       "profiles.profile_literal_star",
				Area:     "profiles",
				Severity: SeverityWarn,
				Summary:  fmt.Sprintf("profile %q allowedHosts contains *", name),
			})
		}
		if hostListTooBroad(profile.AllowedHosts) {
			findings = append(findings, Finding{
				ID:       "profiles.profile_broad_hosts",
				Area:     "profiles",
				Severity: SeverityWarn,
				Summary:  fmt.Sprintf("profile %q has broad allowedHosts", name),
			})
		}
		if profileAllowsPrivileged(profile) && len(profile.AllowedTools) == 0 {
			findings = append(findings, Finding{
				ID:       "profiles.privileged_without_tools",
				Area:     "profiles",
				Severity: severityFor(opts.Mode, SeverityWarn, isHostedOrStartupMode(cfg, opts.Mode)),
				Summary:  fmt.Sprintf("profile %q permits privileged capability without an explicit tool allowlist", name),
			})
		}
	}
	return findings
}

func execFindings(cfg config.Config, opts Options) []Finding {
	findings := []Finding{}
	if !cfg.Hardening.PrivilegedTools && !cfg.Hardening.GuardedTools {
		return findings
	}
	if len(cfg.Hardening.ExecAllowedPrograms) == 0 {
		findings = append(findings, Finding{
			ID:       "exec.allowlist_empty",
			Area:     "exec",
			Severity: severityFor(opts.Mode, SeverityWarn, isHostedOrStartupMode(cfg, opts.Mode)),
			Summary:  "exec is enabled without an exec allowlist",
			FixMode:  FixModeManual,
		})
	}
	if len(cfg.Hardening.ChildEnvAllowlist) == 0 {
		findings = append(findings, Finding{
			ID:       "exec.child_env_empty",
			Area:     "exec",
			Severity: SeverityWarn,
			Summary:  "exec-capable configuration has an empty child environment allowlist",
		})
	}
	if cfg.Hardening.EnableExecShell {
		findings = append(findings, Finding{
			ID:       "exec.shell_mode_enabled",
			Area:     "exec",
			Severity: SeverityWarn,
			Summary:  "exec shell command mode is enabled; prefer program + args and keep shell mode off unless strictly required",
		})
	}
	if publicIngressCanReachExec(cfg) || webhookCanReachExec(cfg) {
		findings = append(findings, Finding{
			ID:       "exec.public_ingress_reachable",
			Area:     "exec",
			Severity: severityFor(opts.Mode, SeverityWarn, opts.Mode == ModeStartupServe),
			Summary:  "public or webhook-facing ingress can reach privileged exec posture unless profiles deny it",
		})
	}
	return findings
}

func skillFindings(cfg config.Config, opts Options) []Finding {
	findings := []Finding{}
	if !cfg.Skills.EnableExec {
		return findings
	}
	if !cfg.Skills.Policy.QuarantineByDefault {
		findings = append(findings, Finding{
			ID:       "skills.quarantine_disabled",
			Area:     "skills",
			Severity: severityFor(opts.Mode, SeverityWarn, isHostedOrStartupMode(cfg, opts.Mode)),
			Summary:  "skill execution is enabled while quarantineByDefault is false",
		})
	}
	if len(cfg.Skills.Policy.TrustedOwners) == 0 {
		findings = append(findings, Finding{
			ID:       "skills.trusted_owners_empty",
			Area:     "skills",
			Severity: severityFor(opts.Mode, SeverityWarn, isHostedOrStartupMode(cfg, opts.Mode)),
			Summary:  "skill execution is enabled without a trustedOwners policy for managed skills",
		})
	}
	if len(cfg.Skills.Policy.TrustedRegistries) == 0 {
		findings = append(findings, Finding{
			ID:       "skills.trusted_registries_empty",
			Area:     "skills",
			Severity: severityFor(opts.Mode, SeverityWarn, isHostedOrStartupMode(cfg, opts.Mode)),
			Summary:  "skill execution is enabled without a trustedRegistries policy for managed skills",
		})
	}
	if len(cfg.Hardening.ChildEnvAllowlist) == 0 {
		findings = append(findings, Finding{
			ID:       "skills.child_env_empty",
			Area:     "skills",
			Severity: SeverityWarn,
			Summary:  "skill execution is enabled with an empty child environment allowlist",
		})
	}
	if hasPublicIngress(cfg) && publicIngressCanReachSkillExec(cfg) {
		findings = append(findings, Finding{
			ID:       "skills.public_ingress_reachable",
			Area:     "skills",
			Severity: severityFor(opts.Mode, SeverityWarn, opts.Mode == ModeStartupServe),
			Summary:  "public ingress can reach skill execution through a permissive profile",
		})
	}
	if cfg.Triggers.Webhook.Enabled {
		if _, profile, ok := resolveEffectiveProfile(cfg, "webhook", "webhook"); !ok || (profileAllowsPrivileged(profile) && profileAllowsTool(profile, "run_skill_script")) {
			findings = append(findings, Finding{
				ID:       "skills.webhook_reachable",
				Area:     "skills",
				Severity: severityFor(opts.Mode, SeverityWarn, opts.Mode == ModeStartupServe),
				Summary:  "webhook can reach skill execution through a permissive profile",
			})
		}
	}
	return findings
}

func channelExposureFindings(cfg config.Config, opts Options) []Finding {
	findings := []Finding{}
	if cfg.Channels.Telegram.Enabled && cfg.Channels.Telegram.OpenAccess {
		findings = append(findings, publicChannelExposureFindings(cfg, opts, "telegram")...)
	}
	if cfg.Channels.Slack.Enabled && cfg.Channels.Slack.OpenAccess {
		findings = append(findings, publicChannelExposureFindings(cfg, opts, "slack")...)
	}
	if cfg.Channels.Discord.Enabled && cfg.Channels.Discord.OpenAccess {
		findings = append(findings, publicChannelExposureFindings(cfg, opts, "discord")...)
	}
	if cfg.Channels.WhatsApp.Enabled && cfg.Channels.WhatsApp.OpenAccess {
		findings = append(findings, publicChannelExposureFindings(cfg, opts, "whatsapp")...)
	}
	if cfg.Channels.Email.Enabled && cfg.Channels.Email.OpenAccess {
		findings = append(findings, publicChannelExposureFindings(cfg, opts, "email")...)
	}
	return findings
}

func publicChannelExposureFindings(cfg config.Config, opts Options, channel string) []Finding {
	findings := []Finding{{
		ID:       "channels.open_access",
		Area:     channel,
		Severity: SeverityWarn,
		Summary:  "channel is open to any sender",
	}}
	profileName, profile, ok := resolveEffectiveProfile(cfg, "", channel)
	if !ok {
		if cfg.Hardening.PrivilegedTools {
			findings = append(findings, Finding{
				ID:       "channels.open_access_privileged_without_profile",
				Area:     channel,
				Severity: severityFor(opts.Mode, SeverityWarn, opts.Mode == ModeStartupServe),
				Summary:  "open-access channel can reach privileged tools because no access profile applies",
			})
		}
		if cfg.Hardening.GuardedTools {
			findings = append(findings, Finding{
				ID:       "channels.open_access_guarded_without_profile",
				Area:     channel,
				Severity: severityFor(opts.Mode, SeverityWarn, opts.Mode == ModeStartupServe),
				Summary:  "open-access channel can reach guarded tools because no access profile applies",
			})
		}
		if cfg.Skills.EnableExec && cfg.Hardening.PrivilegedTools {
			findings = append(findings, Finding{
				ID:       "channels.open_access_skills_without_profile",
				Area:     channel,
				Severity: severityFor(opts.Mode, SeverityWarn, opts.Mode == ModeStartupServe),
				Summary:  "open-access channel can reach skill execution because no access profile applies",
			})
		}
		return findings
	}
	if profileAllowsPrivileged(profile) {
		findings = append(findings, Finding{
			ID:       "channels.open_access_privileged_profile",
			Area:     channel,
			Severity: severityFor(opts.Mode, SeverityWarn, opts.Mode == ModeStartupServe),
			Summary:  fmt.Sprintf("open-access channel resolves to profile %q with privileged capability", profileName),
		})
	}
	if cfg.Hardening.GuardedTools && !profileHasMeaningfulToolRestriction(profile) {
		findings = append(findings, Finding{
			ID:       "channels.open_access_no_tool_boundary",
			Area:     channel,
			Severity: SeverityWarn,
			Summary:  fmt.Sprintf("open-access channel resolves to profile %q without a meaningful tool restriction", profileName),
		})
	}
	if cfg.Hardening.EnableExecShell && cfg.Hardening.PrivilegedTools && profileCanReachExec(profile) {
		findings = append(findings, Finding{
			ID:       "channels.open_access_exec_shell",
			Area:     channel,
			Severity: severityFor(opts.Mode, SeverityWarn, opts.Mode == ModeStartupServe),
			Summary:  fmt.Sprintf("open-access channel can reach exec shell mode via profile %q", profileName),
		})
	}
	if cfg.Skills.EnableExec && profileAllowsPrivileged(profile) && profileAllowsTool(profile, "run_skill_script") {
		findings = append(findings, Finding{
			ID:       "channels.open_access_skill_exec",
			Area:     channel,
			Severity: severityFor(opts.Mode, SeverityWarn, opts.Mode == ModeStartupServe),
			Summary:  fmt.Sprintf("open-access channel can reach skill execution via profile %q", profileName),
		})
	}
	return findings
}

func channelIngressFindings(cfg config.Config, opts Options) []Finding {
	findings := []Finding{}
	add := func(channel, area string, enabled bool, policy config.InboundPolicy, openAccess bool, hasAllowlist bool) {
		if !enabled {
			return
		}
		if !requiresChannelAllowlist(policy, openAccess, hasAllowlist) {
			return
		}
		findings = append(findings, Finding{
			ID:       "channels.invalid_ingress",
			Area:     area,
			Severity: severityFor(opts.Mode, SeverityError, opts.Mode == ModeStartupServe || opts.Mode == ModeConfigurePostSave),
			Summary:  fmt.Sprintf("%s is enabled without pairing, allowlist, or open access policy", channel),
			Detail:   "Enabled channels must choose an inbound authorization model.",
			FixMode:  FixModeInteractive,
			FixHint:  "Choose pairing, allowlist, open access, or deny inbound.",
			Metadata: map[string]string{"channel": area},
		})
	}
	add("Telegram", "telegram", cfg.Channels.Telegram.Enabled, cfg.Channels.Telegram.InboundPolicy, cfg.Channels.Telegram.OpenAccess, hasNonEmpty(cfg.Channels.Telegram.AllowedChatIDs))
	add("Slack", "slack", cfg.Channels.Slack.Enabled, cfg.Channels.Slack.InboundPolicy, cfg.Channels.Slack.OpenAccess, hasNonEmpty(cfg.Channels.Slack.AllowedUserIDs))
	add("Discord", "discord", cfg.Channels.Discord.Enabled, cfg.Channels.Discord.InboundPolicy, cfg.Channels.Discord.OpenAccess, hasNonEmpty(cfg.Channels.Discord.AllowedUserIDs))
	add("WhatsApp", "whatsapp", cfg.Channels.WhatsApp.Enabled, cfg.Channels.WhatsApp.InboundPolicy, cfg.Channels.WhatsApp.OpenAccess, hasNonEmpty(cfg.Channels.WhatsApp.AllowedFrom))
	add("Email", "email", cfg.Channels.Email.Enabled, cfg.Channels.Email.InboundPolicy, cfg.Channels.Email.OpenAccess, hasNonEmpty(cfg.Channels.Email.AllowedSenders))
	if cfg.Channels.Email.Enabled && !cfg.Channels.Email.ConsentGranted {
		findings = append(findings, Finding{
			ID:       "email.consent_missing",
			Area:     "email",
			Severity: severityFor(opts.Mode, SeverityError, opts.Mode == ModeStartupServe),
			Summary:  "email is enabled without explicit consentGranted=true",
			FixMode:  FixModeManual,
		})
	}
	return findings
}

func runtimeProfileFindings(cfg config.Config, opts Options) []Finding {
	findings := []Finding{}
	p := cfg.RuntimeProfile
	if p == "" {
		findings = append(findings, Finding{
			ID:       "runtime-profile.unset",
			Area:     "runtime-profile",
			Severity: SeverityWarn,
			Summary:  "runtimeProfile is not set; consider setting it to one of: local-dev, single-user-hardened, hosted-service, hosted-no-exec, hosted-remote-sandbox-only",
		})
		return findings
	}
	if config.IsHostedProfile(p) {
		if !cfg.Security.SecretStore.Enabled {
			findings = append(findings, Finding{
				ID:       "runtime-profile.secret_store_disabled",
				Area:     "runtime-profile",
				Severity: severityFor(opts.Mode, SeverityWarn, isHostedOrStartupMode(cfg, opts.Mode)),
				Summary:  "hosted profile requires security.secretStore.enabled",
			})
		}
		if !cfg.Security.SecretStore.Required {
			findings = append(findings, Finding{
				ID:       "runtime-profile.secret_store_not_required",
				Area:     "runtime-profile",
				Severity: severityFor(opts.Mode, SeverityWarn, isHostedOrStartupMode(cfg, opts.Mode)),
				Summary:  "hosted profile requires security.secretStore.required",
			})
		}
		if !cfg.Security.Audit.Enabled {
			findings = append(findings, Finding{
				ID:       "runtime-profile.audit_disabled",
				Area:     "runtime-profile",
				Severity: severityFor(opts.Mode, SeverityWarn, isHostedOrStartupMode(cfg, opts.Mode)),
				Summary:  "hosted profile requires security.audit.enabled",
			})
		}
		if !cfg.Security.Audit.Strict {
			findings = append(findings, Finding{
				ID:       "runtime-profile.audit_not_strict",
				Area:     "runtime-profile",
				Severity: severityFor(opts.Mode, SeverityWarn, isHostedOrStartupMode(cfg, opts.Mode)),
				Summary:  "hosted profile requires security.audit.strict",
			})
		}
		if !cfg.Security.Audit.VerifyOnStart {
			findings = append(findings, Finding{
				ID:       "runtime-profile.audit_no_verify_on_start",
				Area:     "runtime-profile",
				Severity: severityFor(opts.Mode, SeverityWarn, isHostedOrStartupMode(cfg, opts.Mode)),
				Summary:  "hosted profile requires security.audit.verifyOnStart",
			})
		}
		if !cfg.Security.Network.Enabled && !cfg.Security.Network.DefaultDeny {
			findings = append(findings, Finding{
				ID:       "runtime-profile.network_missing",
				Area:     "runtime-profile",
				Severity: severityFor(opts.Mode, SeverityWarn, isHostedOrStartupMode(cfg, opts.Mode)),
				Summary:  "hosted profile should configure security.network outbound policy",
			})
		}
	}
	if p == config.ProfileHostedNoExec {
		if cfg.Hardening.EnableExecShell {
			findings = append(findings, Finding{
				ID:       "runtime-profile.hosted_no_exec_shell",
				Area:     "runtime-profile",
				Severity: severityFor(opts.Mode, SeverityWarn, isHostedOrStartupMode(cfg, opts.Mode)),
				Summary:  "hosted-no-exec profile: enableExecShell should be false",
			})
		}
		if cfg.Hardening.PrivilegedTools {
			findings = append(findings, Finding{
				ID:       "runtime-profile.hosted_no_exec_privileged",
				Area:     "runtime-profile",
				Severity: severityFor(opts.Mode, SeverityWarn, isHostedOrStartupMode(cfg, opts.Mode)),
				Summary:  "hosted-no-exec profile: privilegedTools should be false",
			})
		}
	}
	if p == config.ProfileHostedRemoteSandbox {
		if cfg.Hardening.EnableExecShell && !cfg.Hardening.Sandbox.Enabled {
			findings = append(findings, Finding{
				ID:       "runtime-profile.remote_sandbox_missing",
				Area:     "runtime-profile",
				Severity: severityFor(opts.Mode, SeverityWarn, isHostedOrStartupMode(cfg, opts.Mode)),
				Summary:  "hosted-remote-sandbox-only profile: exec requires sandbox to be enabled",
			})
		}
	}
	return findings
}

func probeFindings(cfg config.Config, opts Options) []Finding {
	findings := []Finding{}
	if strings.TrimSpace(cfg.DBPath) != "" {
		database, err := db.Open(cfg.DBPath)
		if err != nil {
			findings = append(findings, Finding{
				ID:       "probe.sqlite_open_failed",
				Area:     "runtime",
				Severity: SeverityError,
				Summary:  "SQLite database could not be opened",
				Detail:   err.Error(),
				FixMode:  FixModeManual,
			})
		} else {
			_ = database.Close()
		}
	}
	return findings
}

func validateConfigSnapshot(cfg config.Config) error {
	file, err := os.CreateTemp("", "or3-intern-doctor-*.json")
	if err != nil {
		return err
	}
	path := file.Name()
	if closeErr := file.Close(); closeErr != nil {
		_ = os.Remove(path)
		return closeErr
	}
	defer os.Remove(path)
	if err := config.Save(path, cfg); err != nil {
		return err
	}
	_, err = config.Load(path)
	return err
}

func keyFileFinding(id, area, path, summary string, fixMode FixMode) *Finding {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	return &Finding{
		ID:       id,
		Area:     area,
		Severity: SeverityWarn,
		Summary:  summary,
		Detail:   path,
		FixMode:  fixMode,
		FixHint:  "Generate the missing key file.",
	}
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

func approvalBrokerRequired(cfg config.Config) bool {
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

func hasNonEmpty(items []string) bool {
	for _, item := range items {
		if strings.TrimSpace(item) != "" {
			return true
		}
	}
	return false
}

func requiresChannelAllowlist(policy config.InboundPolicy, openAccess bool, hasAllowlist bool) bool {
	switch strings.ToLower(strings.TrimSpace(string(policy))) {
	case string(config.InboundPolicyAllowlist):
		return !hasAllowlist
	case string(config.InboundPolicyPairing), string(config.InboundPolicyDeny):
		return false
	default:
		return !openAccess && !hasAllowlist
	}
}

func isHostedOrStartupMode(cfg config.Config, mode Mode) bool {
	if config.IsHostedProfile(cfg.RuntimeProfile) {
		return true
	}
	return mode == ModeStartupChat || mode == ModeStartupServe || mode == ModeStartupService
}

func fixModeForBind(profile config.RuntimeProfile) FixMode {
	if config.IsHostedProfile(profile) {
		return FixModeInteractive
	}
	return FixModeAutomatic
}

func TopFindings(findings []Finding, limit int) []Finding {
	if limit <= 0 || len(findings) <= limit {
		return append([]Finding{}, findings...)
	}
	return append([]Finding{}, findings[:limit]...)
}

func ValidateEndpoints(ctx context.Context, cfg config.Config) error {
	_ = ctx
	_ = cfg
	return nil
}
