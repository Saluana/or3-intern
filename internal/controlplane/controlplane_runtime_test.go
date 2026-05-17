package controlplane

import (
	"testing"

	"or3-intern/internal/agentcli"
)

func TestBuildChatRunnerIncludesRuntimeModelsAndDefault(t *testing.T) {
	spec := agentcli.RunnerSpec{
		ID:          agentcli.RunnerOpenCode,
		DisplayName: "OpenCode",
		Supports:    agentcli.RunnerSupports{Chat: agentcli.RunnerChatCapabilities{ChatSelectable: true, ChatReplay: true}},
	}
	info := agentcli.RunnerInfo{
		ID:          string(agentcli.RunnerOpenCode),
		DisplayName: "OpenCode",
		Status:      agentcli.RunnerStatusAvailable,
		AuthStatus:  agentcli.AuthReady,
		Runtime: agentcli.RunnerRuntimeInfo{
			Kind:         agentcli.RuntimeNative,
			Mode:         agentcli.RuntimeModeAuto,
			State:        agentcli.RuntimeStateReady,
			Ownership:    agentcli.RuntimeOwnershipManaged,
			DefaultModel: "anthropic/claude-sonnet-4-5",
			Models:       []agentcli.RunnerModelInfo{{ID: "anthropic/claude-sonnet-4-5", DisplayName: "Claude Sonnet 4.5", Default: true}},
		},
	}
	out := BuildChatRunner(spec, info, "", "", "", "")
	if out["default_model"] != "anthropic/claude-sonnet-4-5" {
		t.Fatalf("default_model = %v", out["default_model"])
	}
	if _, ok := out["runtime"].(agentcli.RunnerRuntimeInfo); !ok {
		t.Fatalf("runtime missing or wrong type: %#v", out["runtime"])
	}
	models, ok := out["models"].([]agentcli.RunnerModelInfo)
	if !ok || len(models) != 1 || models[0].ID != "anthropic/claude-sonnet-4-5" {
		t.Fatalf("models = %#v", out["models"])
	}
}
