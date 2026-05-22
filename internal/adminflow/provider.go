package adminflow

import (
	"strings"

	"or3-intern/internal/agentcli"
	"or3-intern/internal/config"
)

type AdminBrainKind string

const (
	AdminBrainRunner         AdminBrainKind = "runner"
	AdminBrainAPIKeyProvider AdminBrainKind = "apiKeyProvider"
	AdminBrainUnavailable    AdminBrainKind = "unavailable"
)

type AdminBrainProvider struct {
	Kind        AdminBrainKind `json:"kind"`
	Available   bool           `json:"available"`
	DisplayName string         `json:"display_name,omitempty"`
	RunnerID    string         `json:"runner_id,omitempty"`
	ProviderKey string         `json:"provider_key,omitempty"`
	Reason      string         `json:"reason,omitempty"`
}

func DetectAdminBrainProvider(cfg config.Config, runners []agentcli.RunnerInfo) AdminBrainProvider {
	for _, runner := range runners {
		if runner.Status != agentcli.RunnerStatusAvailable {
			continue
		}
		if runner.AuthStatus != agentcli.AuthReady {
			continue
		}
		if !runner.Supports.Chat.ChatSelectable {
			continue
		}
		return AdminBrainProvider{
			Kind:        AdminBrainRunner,
			Available:   true,
			DisplayName: "Admin Brain",
			RunnerID:    strings.TrimSpace(runner.ID),
		}
	}
	if providerKey := configuredAdminBrainProviderKey(cfg); providerKey != "" {
		return AdminBrainProvider{
			Kind:        AdminBrainAPIKeyProvider,
			Available:   true,
			DisplayName: "Admin Brain",
			ProviderKey: providerKey,
		}
	}
	return AdminBrainProvider{
		Kind:        AdminBrainUnavailable,
		Available:   false,
		DisplayName: "Admin Brain",
		Reason:      "Basic Doctor is available. AI repair is not configured yet.",
	}
}

func configuredAdminBrainProviderKey(cfg config.Config) string {
	if strings.TrimSpace(cfg.Provider.APIKey) != "" {
		key := strings.TrimSpace(cfg.ModelRouting.Chat.Primary.Provider)
		if key == "" {
			key = inferProviderKeyFromBase(cfg.Provider.APIBase)
		}
		if key == "" {
			key = "provider"
		}
		return key
	}
	for key, profile := range cfg.Providers {
		if strings.TrimSpace(profile.APIKey) != "" {
			return strings.TrimSpace(key)
		}
	}
	return ""
}

func inferProviderKeyFromBase(apiBase string) string {
	base := strings.ToLower(strings.TrimSpace(apiBase))
	switch {
	case strings.Contains(base, "openrouter"):
		return "openrouter"
	case strings.Contains(base, "openai"):
		return "openai"
	case base != "":
		return "custom"
	default:
		return ""
	}
}
