package agentcli

import (
	"testing"

	"or3-intern/internal/config"
)

func TestOpenCodeCLIModelFlagMapsVendorPrefixedSlug(t *testing.T) {
	catalog := []RunnerModelInfo{
		{ID: "mimo-v2.5", Provider: "openrouter", ProviderName: "OpenRouter"},
		{ID: "xiaomi/mimo-v2.5", Provider: "openrouter", ProviderName: "OpenRouter"},
	}
	got := openCodeCLIModelFlagForCatalog(catalog, "xiaomi/mimo-v2.5")
	if got != "openrouter/mimo-v2.5" {
		t.Fatalf("cli flag = %q, want openrouter/mimo-v2.5", got)
	}
}

func TestOpenCodeCLIModelFlagHeuristicWithoutCatalog(t *testing.T) {
	got := OpenCodeCLIModelFlag(t.Context(), config.AgentCLIConfig{}, []string{"PATH="}, "xiaomi/mimo-v2.5")
	if got != "openrouter/mimo-v2.5" {
		t.Fatalf("cli flag = %q, want openrouter/mimo-v2.5", got)
	}
}

func TestNormalizeOpenCodeModelIDWithoutCatalog(t *testing.T) {
	got := NormalizeOpenCodeModelID(t.Context(), config.AgentCLIConfig{}, []string{"PATH="}, "xiaomi/mimo-v2.5")
	if got != "mimo-v2.5" {
		t.Fatalf("model id = %q, want mimo-v2.5", got)
	}
}
