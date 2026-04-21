package doctor

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"or3-intern/internal/config"
	"or3-intern/internal/security"
)

func ApplyAutomaticFixes(cfgPath string, cfg *config.Config, report Report) ([]AppliedFix, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config required")
	}
	applied := []AppliedFix{}
	changedConfig := false
	defaults := config.Default()
	for _, finding := range report.Findings {
		switch finding.ID {
		case "filesystem.db_parent_missing":
			if err := os.MkdirAll(filepath.Dir(strings.TrimSpace(cfg.DBPath)), 0o755); err != nil {
				return applied, err
			}
			applied = append(applied, AppliedFix{ID: finding.ID, Summary: "created database parent directory"})
		case "filesystem.artifacts_dir_missing":
			if err := os.MkdirAll(strings.TrimSpace(cfg.ArtifactsDir), 0o755); err != nil {
				return applied, err
			}
			applied = append(applied, AppliedFix{ID: finding.ID, Summary: "created artifacts directory"})
		case "security.audit.key_missing":
			if _, err := security.LoadOrCreateKey(cfg.Security.Audit.KeyFile); err != nil {
				return applied, err
			}
			applied = append(applied, AppliedFix{ID: finding.ID, Summary: "generated audit key file"})
		case "security.secret_store.key_missing":
			if _, err := security.LoadOrCreateKey(cfg.Security.SecretStore.KeyFile); err != nil {
				return applied, err
			}
			applied = append(applied, AppliedFix{ID: finding.ID, Summary: "generated secret-store key file"})
		case "approvals.key_missing", "approvals.key_path_missing":
			if strings.TrimSpace(cfg.Security.Approvals.KeyFile) == "" {
				continue
			}
			if _, err := security.LoadOrCreateKey(cfg.Security.Approvals.KeyFile); err != nil {
				return applied, err
			}
			applied = append(applied, AppliedFix{ID: finding.ID, Summary: "generated approvals key file"})
		case "quotas.unset":
			cfg.Hardening.Quotas = defaults.Hardening.Quotas
			changedConfig = true
			applied = append(applied, AppliedFix{ID: finding.ID, Summary: "restored default hardening quotas"})
		case "privileged-exec.bubblewrap_path_empty":
			cfg.Hardening.Sandbox.BubblewrapPath = defaults.Hardening.Sandbox.BubblewrapPath
			changedConfig = true
			applied = append(applied, AppliedFix{ID: finding.ID, Summary: "set bubblewrapPath to the default bwrap value"})
		case "service.public_bind":
			if fixModeForBind(cfg.RuntimeProfile) == FixModeAutomatic {
				cfg.Service.Listen = "127.0.0.1:9100"
				changedConfig = true
				applied = append(applied, AppliedFix{ID: finding.ID, Summary: "bound service to loopback"})
			}
		case "webhook.public_bind":
			if fixModeForBind(cfg.RuntimeProfile) == FixModeAutomatic {
				cfg.Triggers.Webhook.Addr = "127.0.0.1:8765"
				changedConfig = true
				applied = append(applied, AppliedFix{ID: finding.ID, Summary: "bound webhook to loopback"})
			}
		case "channels.invalid_ingress":
			channel := finding.Metadata["channel"]
			if applyChannelIngressChoice(cfg, channel, "deny", nil) {
				changedConfig = true
				applied = append(applied, AppliedFix{ID: finding.ID, Summary: fmt.Sprintf("set %s inbound access to deny", channel)})
			}
		}
	}
	if changedConfig {
		if strings.TrimSpace(cfgPath) == "" {
			return applied, fmt.Errorf("config path required for config mutations")
		}
		if err := config.Save(cfgPath, *cfg); err != nil {
			return applied, err
		}
	}
	return applied, nil
}

func GenerateSecret() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func ApplyInteractiveChoice(cfg *config.Config, finding Finding, choice string, allowlist []string) (bool, error) {
	if cfg == nil {
		return false, fmt.Errorf("config required")
	}
	switch finding.ID {
	case "channels.invalid_ingress":
		return applyChannelIngressChoice(cfg, finding.Metadata["channel"], choice, allowlist), nil
	case "service.secret_missing", "service.secret_weak":
		switch strings.ToLower(strings.TrimSpace(choice)) {
		case "generate":
			secret, err := GenerateSecret()
			if err != nil {
				return false, err
			}
			cfg.Service.Secret = secret
			return true, nil
		case "disable":
			cfg.Service.Enabled = false
			return true, nil
		}
	case "webhook.secret_missing":
		switch strings.ToLower(strings.TrimSpace(choice)) {
		case "generate":
			secret, err := GenerateSecret()
			if err != nil {
				return false, err
			}
			cfg.Triggers.Webhook.Secret = secret
			return true, nil
		case "disable":
			cfg.Triggers.Webhook.Enabled = false
			return true, nil
		}
	case "service.public_bind":
		if strings.EqualFold(strings.TrimSpace(choice), "loopback") {
			cfg.Service.Listen = "127.0.0.1:9100"
			return true, nil
		}
	case "webhook.public_bind":
		if strings.EqualFold(strings.TrimSpace(choice), "loopback") {
			cfg.Triggers.Webhook.Addr = "127.0.0.1:8765"
			return true, nil
		}
	case "security.secret_store_disabled_with_integrations":
		switch strings.ToLower(strings.TrimSpace(choice)) {
		case "enable":
			cfg.Security.SecretStore.Enabled = true
			cfg.Security.SecretStore.Required = true
			if strings.TrimSpace(cfg.Security.SecretStore.KeyFile) == "" {
				cfg.Security.SecretStore.KeyFile = config.Default().Security.SecretStore.KeyFile
			}
			if _, err := security.LoadOrCreateKey(cfg.Security.SecretStore.KeyFile); err != nil {
				return false, err
			}
			return true, nil
		case "skip":
			return false, nil
		}
	case "privileged-exec.sandbox_disabled":
		switch strings.ToLower(strings.TrimSpace(choice)) {
		case "disable_privileged":
			cfg.Hardening.PrivilegedTools = false
			return true, nil
		case "enable_sandbox":
			cfg.Hardening.Sandbox.Enabled = true
			if strings.TrimSpace(cfg.Hardening.Sandbox.BubblewrapPath) == "" {
				cfg.Hardening.Sandbox.BubblewrapPath = config.Default().Hardening.Sandbox.BubblewrapPath
			}
			return true, nil
		case "skip":
			return false, nil
		}
	case "privileged-exec.bubblewrap_missing":
		switch strings.ToLower(strings.TrimSpace(choice)) {
		case "disable_privileged":
			cfg.Hardening.PrivilegedTools = false
			cfg.Hardening.Sandbox.Enabled = false
			return true, nil
		case "set_path":
			if len(allowlist) == 0 || strings.TrimSpace(allowlist[0]) == "" {
				return false, nil
			}
			cfg.Hardening.Sandbox.Enabled = true
			cfg.Hardening.Sandbox.BubblewrapPath = strings.TrimSpace(allowlist[0])
			return true, nil
		case "skip":
			return false, nil
		}
	}
	return false, nil
}

func applyChannelIngressChoice(cfg *config.Config, channel, choice string, allowlist []string) bool {
	choice = strings.ToLower(strings.TrimSpace(choice))
	switch strings.ToLower(strings.TrimSpace(channel)) {
	case "telegram":
		return applyChannelIngress(&cfg.Channels.Telegram.InboundPolicy, &cfg.Channels.Telegram.OpenAccess, &cfg.Channels.Telegram.AllowedChatIDs, &cfg.Channels.Telegram.Enabled, choice, allowlist)
	case "slack":
		return applyChannelIngress(&cfg.Channels.Slack.InboundPolicy, &cfg.Channels.Slack.OpenAccess, &cfg.Channels.Slack.AllowedUserIDs, &cfg.Channels.Slack.Enabled, choice, allowlist)
	case "discord":
		return applyChannelIngress(&cfg.Channels.Discord.InboundPolicy, &cfg.Channels.Discord.OpenAccess, &cfg.Channels.Discord.AllowedUserIDs, &cfg.Channels.Discord.Enabled, choice, allowlist)
	case "whatsapp":
		return applyChannelIngress(&cfg.Channels.WhatsApp.InboundPolicy, &cfg.Channels.WhatsApp.OpenAccess, &cfg.Channels.WhatsApp.AllowedFrom, &cfg.Channels.WhatsApp.Enabled, choice, allowlist)
	case "email":
		return applyChannelIngress(&cfg.Channels.Email.InboundPolicy, &cfg.Channels.Email.OpenAccess, &cfg.Channels.Email.AllowedSenders, &cfg.Channels.Email.Enabled, choice, allowlist)
	default:
		return false
	}
}

func applyChannelIngress(policy *config.InboundPolicy, openAccess *bool, allowlist *[]string, enabled *bool, choice string, values []string) bool {
	switch choice {
	case "pairing":
		*policy = config.InboundPolicyPairing
		*openAccess = false
		*allowlist = nil
		return true
	case "allowlist":
		items := compactStrings(values)
		if len(items) == 0 {
			return false
		}
		*policy = config.InboundPolicyAllowlist
		*openAccess = false
		*allowlist = items
		return true
	case "open":
		*policy = ""
		*openAccess = true
		*allowlist = nil
		return true
	case "deny":
		*policy = config.InboundPolicyDeny
		*openAccess = false
		*allowlist = nil
		return true
	case "disable":
		*enabled = false
		*policy = config.InboundPolicyDeny
		*openAccess = false
		*allowlist = nil
		return true
	default:
		return false
	}
}

func compactStrings(items []string) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item != "" {
			out = append(out, item)
		}
	}
	return out
}
