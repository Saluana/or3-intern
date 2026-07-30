package controlplane

import (
	"testing"

	"or3-intern/internal/runners"
)

func TestBuildChatRunnerIncludesRuntimeModelsAndDefault(t *testing.T) {
	spec := runners.RunnerSpec{
		ID:          runners.RunnerOpenCode,
		DisplayName: "OpenCode",
		Supports:    runners.RunnerSupports{Chat: runners.RunnerChatCapabilities{ChatSelectable: true, ChatReplay: true}},
	}
	info := runners.RunnerInfo{
		ID:          string(runners.RunnerOpenCode),
		DisplayName: "OpenCode",
		Status:      runners.RunnerStatusAvailable,
		AuthStatus:  runners.AuthReady,
		Runtime: runners.RunnerRuntimeInfo{
			Kind:         runners.RuntimeNative,
			Mode:         runners.RuntimeModeAuto,
			State:        runners.RuntimeStateReady,
			Ownership:    runners.RuntimeOwnershipManaged,
			DefaultModel: "anthropic/claude-sonnet-4-5",
			Models:       []runners.RunnerModelInfo{{ID: "anthropic/claude-sonnet-4-5", DisplayName: "Claude Sonnet 4.5", Default: true}},
		},
	}
	out := BuildChatRunner(spec, info, "", "", "", "")
	if out["default_model"] != "anthropic/claude-sonnet-4-5" {
		t.Fatalf("default_model = %v", out["default_model"])
	}
	if _, ok := out["runtime"].(runners.RunnerRuntimeInfo); !ok {
		t.Fatalf("runtime missing or wrong type: %#v", out["runtime"])
	}
	models, ok := out["models"].([]runners.RunnerModelInfo)
	if !ok || len(models) != 1 || models[0].ID != "anthropic/claude-sonnet-4-5" {
		t.Fatalf("models = %#v", out["models"])
	}
}
