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

// DetectAdminBrainProvider chooses how settings health chat runs AI turns.
// Admin Assistant turns rely on in-process doctor_* service tools, so only the
// service runtime can be advertised as tool-capable Admin Brain.
func DetectAdminBrainProvider(cfg config.Config, runners []agentcli.RunnerInfo) AdminBrainProvider {
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
		Reason:      "Basic Doctor is available. Configure an in-process model provider so Admin Assistant can use Doctor tools.",
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
