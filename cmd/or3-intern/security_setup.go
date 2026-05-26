package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"or3-intern/internal/approval"
	"or3-intern/internal/config"
	"or3-intern/internal/db"
	"or3-intern/internal/providers"
	"or3-intern/internal/security"
)

func buildHostPolicy(cfg config.Config) security.HostPolicy {
	return security.HostPolicy{
		Enabled:       cfg.Security.Network.Enabled,
		DefaultDeny:   cfg.Security.Network.DefaultDeny,
		AllowedHosts:  append([]string{}, cfg.Security.Network.AllowedHosts...),
		AllowLoopback: cfg.Security.Network.AllowLoopback,
		AllowPrivate:  cfg.Security.Network.AllowPrivate,
	}
}

func setupSecurity(ctx context.Context, cfg config.Config, d *db.DB) (config.Config, *security.SecretManager, *security.AuditLogger, error) {
	hostedProfile := config.IsHostedProfile(cfg.RuntimeProfile)
	var secretManager *security.SecretManager
	if cfg.Security.SecretStore.Enabled {
		// Check key file permissions before loading
		if err := checkKeyFilePermissions(cfg.Security.SecretStore.KeyFile, "secret store"); err != nil {
			if hostedProfile || cfg.Security.SecretStore.Required {
				return cfg, nil, nil, err
			}
			// For non-hosted profiles, warn but continue
			fmt.Fprintf(os.Stderr, "Warning: %v\n", err)
		}
		key, err := security.LoadExistingKey(cfg.Security.SecretStore.KeyFile)
		if err != nil {
			if hostedProfile || cfg.Security.SecretStore.Required {
				return cfg, nil, nil, fmt.Errorf("secret store unavailable: %w", err)
			}
		} else {
			secretManager = &security.SecretManager{DB: d, Key: key}
		}
	}
	var auditLogger *security.AuditLogger
	if cfg.Security.Audit.Enabled {
		auditKey, err := security.LoadOrCreateKey(cfg.Security.Audit.KeyFile)
		if err != nil {
			if hostedProfile || cfg.Security.Audit.Strict {
				return cfg, secretManager, nil, fmt.Errorf("audit logger unavailable: %w", err)
			}
		} else {
			auditLogger = &security.AuditLogger{DB: d, Key: auditKey, Strict: cfg.Security.Audit.Strict}
			if cfg.Security.Audit.VerifyOnStart {
				if err := auditLogger.Verify(ctx); err != nil {
					return cfg, secretManager, nil, err
				}
			}
		}
	}
	resolved, err := security.ResolveConfigSecrets(ctx, cfg, secretManager)
	if err != nil {
		return cfg, secretManager, auditLogger, err
	}
	if err := security.ValidateNoSecretRefs(resolved); err != nil {
		return cfg, secretManager, auditLogger, err
	}
	if err := validateConfiguredOutboundEndpoints(ctx, resolved, buildHostPolicy(resolved)); err != nil {
		return cfg, secretManager, auditLogger, err
	}
	return resolved, secretManager, auditLogger, nil
}

func validateConfiguredOutboundEndpoints(ctx context.Context, cfg config.Config, policy security.HostPolicy) error {
	if !policy.EnabledPolicy() {
		return nil
	}
	for _, endpoint := range []string{cfg.Provider.APIBase} {
		if err := policy.ValidateEndpoint(ctx, endpoint); err != nil {
			return err
		}
	}
	if cfg.Channels.Telegram.Enabled {
		if err := policy.ValidateEndpoint(ctx, cfg.Channels.Telegram.APIBase); err != nil {
			return err
		}
	}
	if cfg.Channels.Slack.Enabled {
		for _, endpoint := range []string{cfg.Channels.Slack.APIBase, cfg.Channels.Slack.SocketModeURL} {
			endpoint = strings.TrimSpace(endpoint)
			if endpoint == "" {
				continue
			}
			if err := policy.ValidateEndpoint(ctx, endpoint); err != nil {
				return err
			}
		}
	}
	if cfg.Channels.Discord.Enabled {
		for _, endpoint := range []string{cfg.Channels.Discord.APIBase, cfg.Channels.Discord.GatewayURL} {
			endpoint = strings.TrimSpace(endpoint)
			if endpoint == "" {
				continue
			}
			if err := policy.ValidateEndpoint(ctx, endpoint); err != nil {
				return err
			}
		}
	}
	if cfg.Channels.WhatsApp.Enabled {
		if err := policy.ValidateEndpoint(ctx, cfg.Channels.WhatsApp.BridgeURL); err != nil {
			return err
		}
	}
	if cfg.Channels.Email.Enabled {
		for _, host := range []string{cfg.Channels.Email.IMAPHost, cfg.Channels.Email.SMTPHost} {
			host = strings.TrimSpace(host)
			if host == "" {
				continue
			}
			if err := policy.ValidateEndpoint(ctx, host); err != nil {
				return err
			}
		}
	}
	for name, server := range cfg.Tools.MCPServers {
		if !server.Enabled || (server.Transport != "sse" && server.Transport != "streamablehttp") {
			continue
		}
		if err := policy.ValidateEndpoint(ctx, server.URL); err != nil {
			return fmt.Errorf("mcp server %s denied by network policy: %w", name, err)
		}
	}
	return nil
}

func setupApprovalBroker(cfg config.Config, d *db.DB, audit *security.AuditLogger) (*approval.Broker, error) {
	keyFile := strings.TrimSpace(cfg.Security.Approvals.KeyFile)
	if keyFile == "" {
		if !cfg.Security.Approvals.Enabled {
			return nil, nil
		}
		broker := &approval.Broker{DB: d, Audit: audit, Config: cfg.Security.Approvals, HostID: cfg.Security.Approvals.HostID, Workspace: cfg.WorkspaceDir}
		attachApprovalModerator(broker, cfg)
		return broker, nil
	}
	key, err := security.LoadOrCreateKey(keyFile)
	if err != nil {
		if cfg.Security.Approvals.Enabled && approvalBrokerRequired(cfg) {
			return nil, fmt.Errorf("approval broker unavailable: %w", err)
		}
		if cfg.Security.Approvals.Enabled {
			broker := &approval.Broker{DB: d, Audit: audit, Config: cfg.Security.Approvals, HostID: cfg.Security.Approvals.HostID, Workspace: cfg.WorkspaceDir}
			attachApprovalModerator(broker, cfg)
			return broker, nil
		}
		return nil, nil
	}
	broker := &approval.Broker{DB: d, Audit: audit, Config: cfg.Security.Approvals, HostID: cfg.Security.Approvals.HostID, SignKey: key, Workspace: cfg.WorkspaceDir}
	attachApprovalModerator(broker, cfg)
	return broker, nil
}

func attachApprovalModerator(broker *approval.Broker, cfg config.Config) {
	if broker == nil || !cfg.Security.Approvals.Enabled || !cfg.Security.Approvals.Moderator.Enabled {
		return
	}
	client, model := newApprovalModeratorProviderClient(cfg)
	if client == nil {
		return
	}
	broker.Moderator = approval.NewProviderModerator(client, model, cfg.Security.Approvals.Moderator)
}

func newApprovalModeratorProviderClient(cfg config.Config) (*providers.Client, string) {
	moderatorCfg := cfg.Security.Approvals.Moderator
	providerKey := strings.TrimSpace(moderatorCfg.Provider)
	model := strings.TrimSpace(moderatorCfg.Model)
	if providerKey == "" && model == "" {
		role := cfg.ModelRole(config.ModelRoleChat)
		providerKey = strings.TrimSpace(role.Primary.Provider)
		model = strings.TrimSpace(role.Primary.Model)
	}
	if providerKey == "" {
		providerKey = "openai"
	}
	if model == "" {
		if profile, ok := cfg.ProviderProfile(providerKey); ok && strings.TrimSpace(profile.DefaultChatModel) != "" {
			model = profile.DefaultChatModel
		} else {
			model = cfg.Provider.Model
		}
	}
	timeout := time.Duration(moderatorCfg.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 8 * time.Second
	}
	profile, ok := cfg.ProviderProfile(providerKey)
	if !ok || strings.TrimSpace(profile.APIBase) == "" || strings.TrimSpace(profile.APIKey) == "" {
		return nil, model
	}
	client := providers.New(strings.TrimRight(profile.APIBase, "/"), profile.APIKey, timeout)
	client.ProviderName = providerKey
	client.HostPolicy = buildHostPolicy(cfg)
	return client, model
}

func checkKeyFilePermissions(path, purpose string) error {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // File doesn't exist yet, will be created
		}
		return fmt.Errorf("cannot access %s key file: %w", purpose, err)
	}

	// Check if it's a regular file
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%s key file is not a regular file (may be symlink or special file): %s", purpose, path)
	}

	// Check permissions (on Unix systems)
	perm := info.Mode().Perm()
	if perm&0077 != 0 {
		return fmt.Errorf("%s key file has insecure permissions %04o (should be 0600): %s", purpose, perm, path)
	}

	return nil
}
