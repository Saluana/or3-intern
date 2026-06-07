package adminflow

import (
	"strings"

	"or3-intern/internal/agentcli"
	"or3-intern/internal/config"
)

type AdminBrainKind string

const (
	AdminBrainRunner      AdminBrainKind = "runner"
	AdminBrainUnavailable AdminBrainKind = "unavailable"
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
func DetectAdminBrainProvider(cfg config.Config, runners []agentcli.RunnerInfo) AdminBrainProvider {
	for _, runner := range runners {
		id := strings.TrimSpace(runner.ID)
		if id == "" || strings.EqualFold(id, string(agentcli.RunnerOR3)) {
			continue
		}
		return AdminBrainProvider{
			Kind:        AdminBrainRunner,
			Available:   true,
			DisplayName: runner.DisplayName,
			RunnerID:    id,
		}
	}
	if cfg.AgentCLI.Enabled {
		runnerID := string(agentcli.ResolveDefaultRunner(cfg))
		if strings.TrimSpace(runnerID) != "" && !strings.EqualFold(runnerID, string(agentcli.RunnerOR3)) {
			return AdminBrainProvider{
				Kind:        AdminBrainRunner,
				Available:   true,
				DisplayName: runnerID,
				RunnerID:    runnerID,
			}
		}
	}
	return AdminBrainProvider{
		Kind:        AdminBrainUnavailable,
		Available:   false,
		DisplayName: "Admin Brain",
		Reason:      "Basic Doctor is available. Configure an external runner so Admin Assistant can answer through runner chat.",
	}
}
