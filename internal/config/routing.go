// Package config defines the persisted runtime configuration and validation
// rules for or3-intern.
package config

import (
	"strings"
)

func normalizeModelRef(ref ModelRef, fallbackProvider, fallbackModel string) ModelRef {
	ref.Provider = normalizeProviderKey(firstNonEmpty(ref.Provider, fallbackProvider))
	ref.Model = strings.TrimSpace(firstNonEmpty(ref.Model, fallbackModel))
	return ref
}

func normalizeModelRole(role ModelRoleConfig, fallback ModelRef) ModelRoleConfig {
	role.Primary = normalizeModelRef(role.Primary, fallback.Provider, fallback.Model)
	seen := map[string]struct{}{}
	out := make([]ModelRef, 0, len(role.Fallbacks))
	for _, ref := range role.Fallbacks {
		ref = normalizeModelRef(ref, "", "")
		if ref.Provider == "" || ref.Model == "" {
			continue
		}
		key := ref.Provider + "\x00" + ref.Model
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, ref)
	}
	role.Fallbacks = out
	if role.EmbedDimensions < 0 {
		role.EmbedDimensions = 0
	}
	return role
}

func normalizeProviderRouting(cfg *Config) {
	if cfg == nil {
		return
	}
	if cfg.Providers == nil {
		cfg.Providers = ProviderProfiles{}
	}
	legacyProvider := inferProviderKey(cfg.Provider.APIBase)
	if strings.TrimSpace(cfg.Provider.APIBase) != "" || strings.TrimSpace(cfg.Provider.APIKey) != "" {
		updateProviderProfile(cfg, legacyProvider, func(profile *ProviderProfileConfig) {
			if strings.TrimSpace(cfg.Provider.APIBase) != "" {
				profile.APIBase = strings.TrimSpace(cfg.Provider.APIBase)
			}
			if strings.TrimSpace(cfg.Provider.APIKey) != "" {
				profile.APIKey = cfg.Provider.APIKey
			}
			if cfg.Provider.TimeoutSeconds > 0 {
				profile.TimeoutSeconds = cfg.Provider.TimeoutSeconds
			}
			profile.EnableVision = cfg.Provider.EnableVision
			if strings.TrimSpace(cfg.Provider.Model) != "" {
				profile.DefaultChatModel = strings.TrimSpace(cfg.Provider.Model)
			}
			if strings.TrimSpace(cfg.Provider.EmbedModel) != "" {
				profile.DefaultEmbedModel = strings.TrimSpace(cfg.Provider.EmbedModel)
			}
			if cfg.Provider.EmbedDimensions > 0 {
				profile.DefaultDimensions = cfg.Provider.EmbedDimensions
			}
		})
	}
	ensureProviderProfile(cfg, "openai")
	ensureProviderProfile(cfg, "openrouter")
	ensureProviderProfile(cfg, "custom")

	if strings.TrimSpace(cfg.Provider.Model) != "" && (strings.TrimSpace(cfg.ModelRouting.Chat.Primary.Model) == "" || cfg.ModelRouting.Chat.Primary.Model == defaultOpenAIChatModel) {
		cfg.ModelRouting.Chat.Primary.Model = strings.TrimSpace(cfg.Provider.Model)
		cfg.ModelRouting.Agents.Primary.Model = strings.TrimSpace(cfg.Provider.Model)
		cfg.ModelRouting.Subagents.Primary.Model = strings.TrimSpace(cfg.Provider.Model)
	}
	if strings.TrimSpace(cfg.Provider.EmbedModel) != "" && (strings.TrimSpace(cfg.ModelRouting.Embeddings.Primary.Model) == "" || cfg.ModelRouting.Embeddings.Primary.Model == defaultOpenAIEmbedModel) {
		cfg.ModelRouting.Embeddings.Primary.Model = strings.TrimSpace(cfg.Provider.EmbedModel)
	}
	if cfg.Provider.EmbedDimensions > 0 && cfg.ModelRouting.Embeddings.EmbedDimensions <= 0 {
		cfg.ModelRouting.Embeddings.EmbedDimensions = cfg.Provider.EmbedDimensions
	}
	if strings.TrimSpace(cfg.ConsolidationModel) != "" {
		cfg.ModelRouting.Summarization.Primary.Model = strings.TrimSpace(cfg.ConsolidationModel)
	}
	if strings.TrimSpace(cfg.ContextManager.Model) != "" {
		cfg.ModelRouting.ContextManager.Primary.Model = strings.TrimSpace(cfg.ContextManager.Model)
	}

	chatFallback := ModelRef{Provider: legacyProvider, Model: firstNonEmpty(cfg.Provider.Model, defaultOpenAIChatModel)}
	embedFallback := ModelRef{Provider: legacyProvider, Model: firstNonEmpty(cfg.Provider.EmbedModel, defaultOpenAIEmbedModel)}
	cfg.ModelRouting.Chat = normalizeModelRole(cfg.ModelRouting.Chat, chatFallback)
	cfg.ModelRouting.Agents = normalizeModelRole(cfg.ModelRouting.Agents, cfg.ModelRouting.Chat.Primary)
	cfg.ModelRouting.Subagents = normalizeModelRole(cfg.ModelRouting.Subagents, cfg.ModelRouting.Agents.Primary)
	cfg.ModelRouting.Summarization = normalizeModelRole(cfg.ModelRouting.Summarization, ModelRef{Provider: cfg.ModelRouting.Chat.Primary.Provider, Model: firstNonEmpty(cfg.ConsolidationModel, cfg.ModelRouting.Chat.Primary.Model)})
	cfg.ModelRouting.ContextManager = normalizeModelRole(cfg.ModelRouting.ContextManager, ModelRef{Provider: cfg.ModelRouting.Summarization.Primary.Provider, Model: firstNonEmpty(cfg.ContextManager.Model, cfg.ModelRouting.Summarization.Primary.Model)})
	cfg.ModelRouting.Embeddings = normalizeModelRole(cfg.ModelRouting.Embeddings, embedFallback)
	cfg.ModelRouting.Fallback = normalizeModelRole(cfg.ModelRouting.Fallback, cfg.ModelRouting.Chat.Primary)
	if cfg.ModelRouting.Embeddings.EmbedDimensions <= 0 && cfg.Provider.EmbedDimensions > 0 {
		cfg.ModelRouting.Embeddings.EmbedDimensions = cfg.Provider.EmbedDimensions
	}

	for key, profile := range cfg.Providers {
		normalized := normalizeProviderKey(key)
		if normalized == "" {
			delete(cfg.Providers, key)
			continue
		}
		profile.APIBase = strings.TrimRight(strings.TrimSpace(profile.APIBase), "/")
		profile.Label = strings.TrimSpace(profile.Label)
		if profile.Label == "" {
			profile.Label = normalized
		}
		if profile.TimeoutSeconds <= 0 {
			profile.TimeoutSeconds = 60
		}
		if profile.DefaultDimensions < 0 {
			profile.DefaultDimensions = 0
		}
		if normalized != key {
			delete(cfg.Providers, key)
		}
		cfg.Providers[normalized] = profile
	}
	if cfg.FavoriteModels == nil {
		cfg.FavoriteModels = FavoriteModelsConfig{}
	}
	for provider, favorites := range cfg.FavoriteModels {
		normalized := normalizeProviderKey(provider)
		seen := map[string]struct{}{}
		out := make([]FavoriteModelConfig, 0, len(favorites))
		for _, fav := range favorites {
			fav.Model = strings.TrimSpace(fav.Model)
			fav.Label = strings.TrimSpace(fav.Label)
			if fav.Model == "" {
				continue
			}
			if _, ok := seen[fav.Model]; ok {
				continue
			}
			seen[fav.Model] = struct{}{}
			out = append(out, fav)
		}
		if normalized != provider {
			delete(cfg.FavoriteModels, provider)
		}
		cfg.FavoriteModels[normalized] = out
	}
	syncLegacyProviderFromRouting(cfg)
}

func syncLegacyProviderFromRouting(cfg *Config) {
	if cfg == nil {
		return
	}
	chat := cfg.ModelRouting.Chat.Primary
	if profile, ok := cfg.Providers[chat.Provider]; ok {
		cfg.Provider.APIBase = profile.APIBase
		cfg.Provider.APIKey = profile.APIKey
		cfg.Provider.Model = firstNonEmpty(chat.Model, profile.DefaultChatModel, cfg.Provider.Model)
		cfg.Provider.Temperature = roleTemperature(cfg.ModelRouting.Chat, cfg.Provider.Temperature)
		cfg.Provider.TimeoutSeconds = profile.TimeoutSeconds
		cfg.Provider.EnableVision = profile.EnableVision
	}
	embed := cfg.ModelRouting.Embeddings.Primary
	if profile, ok := cfg.Providers[embed.Provider]; ok {
		cfg.Provider.EmbedModel = firstNonEmpty(embed.Model, profile.DefaultEmbedModel, cfg.Provider.EmbedModel)
		if cfg.ModelRouting.Embeddings.EmbedDimensions > 0 {
			cfg.Provider.EmbedDimensions = cfg.ModelRouting.Embeddings.EmbedDimensions
		} else {
			cfg.Provider.EmbedDimensions = profile.DefaultDimensions
		}
	}
	cfg.ConsolidationModel = strings.TrimSpace(cfg.ModelRouting.Summarization.Primary.Model)
	cfg.ContextManager.Provider = ""
	if profile, ok := cfg.Providers[cfg.ModelRouting.ContextManager.Primary.Provider]; ok {
		cfg.ContextManager.Provider = profile.APIBase
	}
	cfg.ContextManager.Model = strings.TrimSpace(cfg.ModelRouting.ContextManager.Primary.Model)
}

func roleTemperature(role ModelRoleConfig, fallback float64) float64 {
	if role.Temperature != nil {
		return *role.Temperature
	}
	return fallback
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
