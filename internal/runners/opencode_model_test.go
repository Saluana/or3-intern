package runners

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
	got := OpenCodeCLIModelFlag(t.Context(), config.RunnersConfig{}, []string{"PATH="}, "xiaomi/mimo-v2.5")
	if got != "openrouter/mimo-v2.5" {
		t.Fatalf("cli flag = %q, want openrouter/mimo-v2.5", got)
	}
}

func TestNormalizeOpenCodeModelIDWithoutCatalog(t *testing.T) {
	got := NormalizeOpenCodeModelID(t.Context(), config.RunnersConfig{}, []string{"PATH="}, "xiaomi/mimo-v2.5")
	if got != "mimo-v2.5" {
		t.Fatalf("model id = %q, want mimo-v2.5", got)
	}
}

func TestExtractOpenCodeErrorMessageFromAPIErrorEnvelope(t *testing.T) {
	value := map[string]any{
		"info": map[string]any{
			"error": map[string]any{
				"name": "APIError",
				"data": map[string]any{
					"message":    "Not Enough Credits",
					"statusCode": 401,
				},
			},
		},
	}
	if got := extractOpenCodeErrorMessage(value); got != "Not Enough Credits" {
		t.Fatalf("error message = %q, want Not Enough Credits", got)
	}
}
