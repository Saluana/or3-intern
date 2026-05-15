package config

import (
	"net"
	"strings"
)

type IntegrationQuarantine struct {
	Name   string
	Reason string
}

// QuarantineInvalidOptionalIntegrations disables optional integrations that
// should not make core chat start in a runnable-broken state.
func QuarantineInvalidOptionalIntegrations(cfg *Config) []IntegrationQuarantine {
	if cfg == nil {
		return nil
	}
	var quarantined []IntegrationQuarantine
	for name, server := range cfg.Tools.MCPServers {
		if !server.Enabled {
			continue
		}
		if err := validateMCPServers(map[string]MCPServerConfig{name: server}); err != nil {
			server.Enabled = false
			cfg.Tools.MCPServers[name] = server
			quarantined = append(quarantined, IntegrationQuarantine{Name: "mcp:" + name, Reason: err.Error()})
		}
	}
	if cfg.Triggers.Webhook.Enabled && strings.TrimSpace(cfg.Triggers.Webhook.Secret) == "" && !integrationAddrIsLoopback(cfg.Triggers.Webhook.Addr) {
		cfg.Triggers.Webhook.Enabled = false
		quarantined = append(quarantined, IntegrationQuarantine{Name: "webhook", Reason: "public webhook requires a secret"})
	}
	if cfg.Channels.Telegram.Enabled && strings.TrimSpace(cfg.Channels.Telegram.Token) == "" {
		cfg.Channels.Telegram.Enabled = false
		quarantined = append(quarantined, IntegrationQuarantine{Name: "telegram", Reason: "missing bot token"})
	}
	if cfg.Channels.Slack.Enabled && (strings.TrimSpace(cfg.Channels.Slack.AppToken) == "" || strings.TrimSpace(cfg.Channels.Slack.BotToken) == "") {
		cfg.Channels.Slack.Enabled = false
		quarantined = append(quarantined, IntegrationQuarantine{Name: "slack", Reason: "missing app or bot token"})
	}
	if cfg.Channels.Discord.Enabled && strings.TrimSpace(cfg.Channels.Discord.Token) == "" {
		cfg.Channels.Discord.Enabled = false
		quarantined = append(quarantined, IntegrationQuarantine{Name: "discord", Reason: "missing bot token"})
	}
	if cfg.Channels.Email.Enabled && (strings.TrimSpace(cfg.Channels.Email.IMAPHost) == "" || strings.TrimSpace(cfg.Channels.Email.SMTPHost) == "") {
		cfg.Channels.Email.Enabled = false
		quarantined = append(quarantined, IntegrationQuarantine{Name: "email", Reason: "missing mail server settings"})
	}
	cfg.IntegrationWarnings = append([]IntegrationQuarantine{}, quarantined...)
	return quarantined
}

func integrationAddrIsLoopback(addr string) bool {
	host, _, err := net.SplitHostPort(strings.TrimSpace(addr))
	if err != nil {
		host = strings.TrimSpace(addr)
	}
	host = strings.Trim(host, "[]")
	return host == "" || strings.EqualFold(host, "localhost") || isLoopbackHost(host)
}
