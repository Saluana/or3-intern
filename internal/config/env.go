// Package config defines the persisted runtime configuration and validation
// rules for or3-intern.
package config

import (
	"os"
	"strconv"
	"strings"
)

func ApplyEnvOverrides(cfg *Config) {
	if cfg == nil {
		return
	}
	applyEnvString("OR3_DB_PATH", &cfg.DBPath)
	applyEnvString("OR3_ARTIFACTS_DIR", &cfg.ArtifactsDir)
	if v := os.Getenv("OR3_API_BASE"); v != "" {
		providerKey := inferProviderKey(v)
		cfg.Provider.APIBase = v
		cfg.ModelRouting.Chat.Primary.Provider = providerKey
		cfg.ModelRouting.Agents.Primary.Provider = providerKey
		cfg.ModelRouting.Subagents.Primary.Provider = providerKey
		cfg.ModelRouting.Summarization.Primary.Provider = providerKey
		cfg.ModelRouting.ContextManager.Primary.Provider = providerKey
		cfg.ModelRouting.Embeddings.Primary.Provider = providerKey
		updateProviderProfile(cfg, providerKey, func(profile *ProviderProfileConfig) { profile.APIBase = v })
	}
	if v := os.Getenv("OR3_API_KEY"); v != "" {
		cfg.Provider.APIKey = v
		updateProviderProfile(cfg, inferProviderKey(cfg.Provider.APIBase), func(profile *ProviderProfileConfig) { profile.APIKey = v })
	}
	if v := os.Getenv("OR3_MODEL"); v != "" && shouldApplyEnvModelOverride(cfg) {
		cfg.Provider.Model = v
		cfg.ModelRouting.Chat.Primary.Model = v
		cfg.ModelRouting.Agents.Primary.Model = v
		cfg.ModelRouting.Subagents.Primary.Model = v
	}
	if v := os.Getenv("OR3_CONSOLIDATION_MODEL"); v != "" {
		cfg.ConsolidationModel = v
		cfg.ModelRouting.Summarization.Primary.Model = v
	}
	if v := os.Getenv("OR3_EMBED_MODEL"); v != "" {
		cfg.Provider.EmbedModel = v
		cfg.ModelRouting.Embeddings.Primary.Model = v
	}
	if v := os.Getenv("OR3_EMBED_DIMENSIONS"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil {
			cfg.Provider.EmbedDimensions = parsed
			cfg.ModelRouting.Embeddings.EmbedDimensions = parsed
		}
	}
	if v := os.Getenv("OPENAI_API_KEY"); v != "" {
		updateProviderProfile(cfg, "openai", func(profile *ProviderProfileConfig) { profile.APIKey = v })
	}
	if v := os.Getenv("OPENROUTER_API_KEY"); v != "" {
		updateProviderProfile(cfg, "openrouter", func(profile *ProviderProfileConfig) { profile.APIKey = v })
	}
	applyEnvString("OR3_TELEGRAM_TOKEN", &cfg.Channels.Telegram.Token)
	applyEnvString("OR3_SLACK_APP_TOKEN", &cfg.Channels.Slack.AppToken)
	applyEnvString("OR3_SLACK_BOT_TOKEN", &cfg.Channels.Slack.BotToken)
	applyEnvString("OR3_DISCORD_TOKEN", &cfg.Channels.Discord.Token)
	applyEnvString("OR3_WHATSAPP_BRIDGE_URL", &cfg.Channels.WhatsApp.BridgeURL)
	applyEnvString("OR3_WHATSAPP_BRIDGE_TOKEN", &cfg.Channels.WhatsApp.BridgeToken)
	applyEnvString("OR3_EMAIL_IMAP_HOST", &cfg.Channels.Email.IMAPHost)
	applyEnvInt("OR3_EMAIL_IMAP_PORT", &cfg.Channels.Email.IMAPPort)
	applyEnvString("OR3_EMAIL_IMAP_USERNAME", &cfg.Channels.Email.IMAPUsername)
	applyEnvString("OR3_EMAIL_IMAP_PASSWORD", &cfg.Channels.Email.IMAPPassword)
	applyEnvString("OR3_EMAIL_SMTP_HOST", &cfg.Channels.Email.SMTPHost)
	applyEnvInt("OR3_EMAIL_SMTP_PORT", &cfg.Channels.Email.SMTPPort)
	applyEnvString("OR3_EMAIL_SMTP_USERNAME", &cfg.Channels.Email.SMTPUsername)
	applyEnvString("OR3_EMAIL_SMTP_PASSWORD", &cfg.Channels.Email.SMTPPassword)
	applyEnvString("OR3_EMAIL_FROM_ADDRESS", &cfg.Channels.Email.FromAddress)
	applyEnvBool("OR3_SUBAGENTS_ENABLED", &cfg.Subagents.Enabled)
	applyEnvInt("OR3_SUBAGENTS_MAX_CONCURRENT", &cfg.Subagents.MaxConcurrent)
	applyEnvInt("OR3_SUBAGENTS_MAX_QUEUED", &cfg.Subagents.MaxQueued)
	applyEnvInt("OR3_SUBAGENTS_TASK_TIMEOUT_SECONDS", &cfg.Subagents.TaskTimeoutSeconds)
	applyEnvBool("OR3_AGENT_CLI_ENABLED", &cfg.AgentCLI.Enabled)
	if v := os.Getenv("OR3_AGENT_CLI_DISABLED_RUNNERS"); v != "" {
		cfg.AgentCLI.DisabledRunners = compactStrings(strings.Split(v, ","))
	}
	applyEnvInt("OR3_AGENT_CLI_MAX_CONCURRENT", &cfg.AgentCLI.MaxConcurrent)
	applyEnvInt("OR3_AGENT_CLI_MAX_QUEUED", &cfg.AgentCLI.MaxQueued)
	applyEnvInt("OR3_AGENT_CLI_DEFAULT_TIMEOUT_SECONDS", &cfg.AgentCLI.DefaultTimeoutSeconds)
	applyEnvInt("OR3_AGENT_CLI_MAX_TIMEOUT_SECONDS", &cfg.AgentCLI.MaxTimeoutSeconds)
	applyEnvBool("OR3_AGENT_CLI_ALLOW_SANDBOX_AUTO", &cfg.AgentCLI.AllowSandboxAuto)
	applyEnvString("OR3_AGENT_CLI_DEFAULT_MODE", &cfg.AgentCLI.DefaultMode)
	applyEnvString("OR3_AGENT_CLI_DEFAULT_ISOLATION", &cfg.AgentCLI.DefaultIsolation)
	applyEnvBool("OR3_SERVICE_ENABLED", &cfg.Service.Enabled)
	applyEnvString("OR3_SERVICE_LISTEN", &cfg.Service.Listen)
	applyEnvString("OR3_SERVICE_SECRET", &cfg.Service.Secret)
	applyEnvString("OR3_SERVICE_SHARED_SECRET_ROLE", &cfg.Service.SharedSecretRole)
	applyEnvBool("OR3_SERVICE_ALLOW_UNAUTHENTICATED_PAIRING", &cfg.Service.AllowUnauthenticatedPairing)
	applyEnvBool("OR3_SERVICE_ALLOW_REMOTE_UNAUTHENTICATED_PAIRING", &cfg.Service.AllowRemoteUnauthenticatedPairing)
	applyEnvList("OR3_SERVICE_TRUSTED_BROWSER_ORIGINS", &cfg.Service.TrustedBrowserOrigins)
	applyEnvList("OR3_SERVICE_TRUSTED_BROWSER_CIDRS", &cfg.Service.TrustedBrowserCIDRs)
	applyEnvList("OR3_SERVICE_TRUSTED_PAIRING_ORIGINS", &cfg.Service.TrustedPairingOrigins)
	applyEnvList("OR3_SERVICE_TRUSTED_PAIRING_CIDRS", &cfg.Service.TrustedPairingCIDRs)
	applyEnvBool("OR3_AUTH_ENABLED", &cfg.Auth.Enabled)
	applyEnvString("OR3_AUTH_RP_ID", &cfg.Auth.RPID)
	applyEnvString("OR3_AUTH_RP_DISPLAY_NAME", &cfg.Auth.RPDisplayName)
	applyEnvList("OR3_AUTH_ALLOWED_ORIGINS", &cfg.Auth.AllowedOrigins)
	applyEnvList("OR3_AUTH_RELATED_ORIGINS", &cfg.Auth.RelatedOrigins)
	applyEnvInt("OR3_AUTH_SESSION_IDLE_TTL_SECONDS", &cfg.Auth.SessionIdleTTLSeconds)
	applyEnvInt("OR3_AUTH_SESSION_ABSOLUTE_TTL_SECONDS", &cfg.Auth.SessionAbsoluteTTLSeconds)
	applyEnvInt("OR3_AUTH_STEP_UP_TTL_SECONDS", &cfg.Auth.StepUpTTLSeconds)
	applyEnvString("OR3_AUTH_FALLBACK_POLICY", &cfg.Auth.FallbackPolicy)
	if v := os.Getenv("OR3_AUTH_ENFORCEMENT_MODE"); v != "" {
		cfg.Auth.EnforcementMode = AuthEnforcementMode(v)
	}
	applyEnvBool("OR3_AUTH_ALLOW_PAIRED_TOKEN_FALLBACK", &cfg.Auth.AllowPairedTokenFallback)
	applyEnvBool("OR3_AUTH_REQUIRE_PASSKEY_FOR_SENSITIVE", &cfg.Auth.RequirePasskeyForSensitive)
	if v := os.Getenv("OR3_RUNTIME_PROFILE"); v != "" {
		cfg.RuntimeProfile = RuntimeProfile(strings.ToLower(strings.TrimSpace(v)))
	}
}

// shouldApplyEnvModelOverride reports whether OR3_MODEL may replace persisted
// model settings. Env is used to seed first-run defaults; once provider or
// role routing differs from factory defaults (for example via or3-app settings),
// the on-disk config wins across restarts.
func shouldApplyEnvModelOverride(cfg *Config) bool {
	if cfg == nil {
		return false
	}
	defaults := Default()
	if strings.TrimSpace(cfg.Provider.Model) != strings.TrimSpace(defaults.Provider.Model) {
		return false
	}
	for _, pair := range []struct{ got, want string }{
		{cfg.ModelRouting.Chat.Primary.Model, defaults.ModelRouting.Chat.Primary.Model},
		{cfg.ModelRouting.Agents.Primary.Model, defaults.ModelRouting.Agents.Primary.Model},
		{cfg.ModelRouting.Subagents.Primary.Model, defaults.ModelRouting.Subagents.Primary.Model},
	} {
		if strings.TrimSpace(pair.got) != strings.TrimSpace(pair.want) {
			return false
		}
	}
	return true
}

func applyEnvString(key string, target *string) {
	if target == nil {
		return
	}
	if value := os.Getenv(key); value != "" {
		*target = value
	}
}

func applyEnvInt(key string, target *int) {
	if target == nil {
		return
	}
	if value := os.Getenv(key); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil {
			*target = parsed
		}
	}
}

func applyEnvBool(key string, target *bool) {
	if target == nil {
		return
	}
	if value := os.Getenv(key); value != "" {
		if parsed, err := strconv.ParseBool(value); err == nil {
			*target = parsed
		}
	}
}

func applyEnvList(key string, target *[]string) {
	if target == nil {
		return
	}
	if value := os.Getenv(key); value != "" {
		*target = compactStrings(strings.Split(value, ","))
	}
}

func updateProviderProfile(cfg *Config, key string, update func(*ProviderProfileConfig)) {
	profile := ensureProviderProfile(cfg, key)
	if update != nil {
		update(&profile)
	}
	cfg.Providers[normalizeProviderKey(key)] = profile
}

func ensureProviderProfile(cfg *Config, key string) ProviderProfileConfig {
	if cfg.Providers == nil {
		cfg.Providers = ProviderProfiles{}
	}
	key = normalizeProviderKey(key)
	if key == "" {
		key = "openai"
	}
	profile := cfg.Providers[key]
	if strings.TrimSpace(profile.Label) == "" {
		switch key {
		case "openai":
			profile.Label = "OpenAI"
			if strings.TrimSpace(profile.APIBase) == "" {
				profile.APIBase = "https://api.openai.com/v1"
			}
			if strings.TrimSpace(profile.DefaultChatModel) == "" {
				profile.DefaultChatModel = "gpt-4.1-mini"
			}
			if strings.TrimSpace(profile.DefaultEmbedModel) == "" {
				profile.DefaultEmbedModel = "text-embedding-3-small"
			}
		case "openrouter":
			profile.Label = "OpenRouter"
			if strings.TrimSpace(profile.APIBase) == "" {
				profile.APIBase = "https://openrouter.ai/api/v1"
			}
			if strings.TrimSpace(profile.DefaultChatModel) == "" {
				profile.DefaultChatModel = "openai/gpt-4o-mini"
			}
		default:
			profile.Label = strings.ReplaceAll(key, "-", " ")
		}
	}
	if profile.TimeoutSeconds <= 0 {
		profile.TimeoutSeconds = 60
	}
	cfg.Providers[key] = profile
	return profile
}

func normalizeProviderKey(key string) string {
	key = strings.ToLower(strings.TrimSpace(key))
	key = strings.ReplaceAll(key, " ", "-")
	key = strings.Trim(key, "_-/")
	return key
}

func inferProviderKey(apiBase string) string {
	base := strings.ToLower(strings.TrimSpace(apiBase))
	switch {
	case strings.Contains(base, "openrouter.ai"):
		return "openrouter"
	case strings.Contains(base, "api.openai.com"):
		return "openai"
	default:
		return "custom"
	}
}
