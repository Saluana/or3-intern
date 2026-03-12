package main

import (
	"context"
	"fmt"
	"strings"

	"or3-intern/internal/config"
	"or3-intern/internal/db"
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
