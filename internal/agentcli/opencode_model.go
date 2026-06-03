package agentcli

import (
	"context"
	"strings"

	"or3-intern/internal/config"
)

// OpenCodeCatalog returns model metadata from a running OpenCode server or CLI discovery.
func OpenCodeCatalog(ctx context.Context, cfg config.AgentCLIConfig, env []string) []RunnerModelInfo {
	runtime := NewOpenCodeNativeRuntime()
	info := runtime.Info(ctx, cfg, env)
	if len(info.Models) > 0 {
		return info.Models
	}
	return runtime.modelsFromCLI(ctx, env)
}

// NormalizeOpenCodeModelID maps UI/session values (e.g. xiaomi/mimo-v2.5) onto the
// catalog model id OpenCode expects (e.g. mimo-v2.5).
func NormalizeOpenCodeModelID(ctx context.Context, cfg config.AgentCLIConfig, env []string, requested string) string {
	requested = strings.TrimSpace(requested)
	if requested == "" {
		return ""
	}
	catalog := OpenCodeCatalog(ctx, cfg, env)
	if resolved := resolveOpenCodeModel(catalog, requested); resolved != nil {
		return resolved.ID
	}
	if _, modelPart, ok := strings.Cut(requested, "/"); ok && modelPart != "" && !strings.Contains(modelPart, "/") {
		if resolved := resolveOpenCodeModel(catalog, modelPart); resolved != nil {
			return resolved.ID
		}
		return modelPart
	}
	return requested
}

func openCodeCLIModelFlagForCatalog(catalog []RunnerModelInfo, requested string) string {
	requested = strings.TrimSpace(requested)
	if requested == "" {
		return ""
	}
	if resolved := resolveOpenCodeModel(catalog, requested); resolved != nil {
		if provider := strings.TrimSpace(resolved.Provider); provider != "" {
			return provider + "/" + resolved.ID
		}
		return resolved.ID
	}
	providerPart, modelPart, split := strings.Cut(requested, "/")
	if !split || modelPart == "" {
		return requested
	}
	if isKnownOpenCodeProvider(providerPart) {
		return requested
	}
	if !strings.Contains(modelPart, "/") {
		return "openrouter/" + modelPart
	}
	return requested
}

// OpenCodeCLIModelFlag returns the provider/model value for `opencode run --model`.
func OpenCodeCLIModelFlag(ctx context.Context, cfg config.AgentCLIConfig, env []string, requested string) string {
	return openCodeCLIModelFlagForCatalog(OpenCodeCatalog(ctx, cfg, env), requested)
}

func openCodeCatalogWithFallback(ctx context.Context, endpoint string, env []string, runtime *OpenCodeNativeRuntime) []RunnerModelInfo {
	catalog := runtime.models(ctx, endpoint)
	if len(catalog) == 0 {
		catalog = runtime.modelsFromCLI(ctx, env)
	}
	return catalog
}

func openCodeModelRequestForCatalog(catalog []RunnerModelInfo, requested, thinking string) any {
	requested = strings.TrimSpace(requested)
	if requested == "" {
		return requested
	}
	if resolved := resolveOpenCodeModel(catalog, requested); resolved != nil {
		return openCodeModelBody(*resolved, thinking)
	}
	providerID, modelID := splitProviderModel(requested)
	for _, info := range catalog {
		if info.ID != modelID && info.ID != requested {
			continue
		}
		if thinking != "" && !stringInSlice(thinking, info.Reasoning) {
			continue
		}
		provider := firstNonEmpty(providerID, info.Provider)
		if provider == "" {
			return requested
		}
		body := openCodeModelBody(RunnerModelInfo{Provider: provider, ID: info.ID, Reasoning: info.Reasoning}, thinking)
		if body != nil {
			return body
		}
		return requested
	}
	return openCodeModelBodyHeuristic(requested, thinking)
}

func openCodeModelBodyHeuristic(requested, thinking string) any {
	providerPart, modelPart, split := strings.Cut(requested, "/")
	if !split || modelPart == "" {
		return requested
	}
	if isKnownOpenCodeProvider(providerPart) {
		return openCodeModelBody(RunnerModelInfo{Provider: providerPart, ID: modelPart}, thinking)
	}
	if !strings.Contains(modelPart, "/") {
		return openCodeModelBody(RunnerModelInfo{Provider: "openrouter", ID: modelPart}, thinking)
	}
	return requested
}

func mergeOpenCodeModelIntoBody(body map[string]any, model any) {
	switch value := model.(type) {
	case map[string]any:
		if providerID := strings.TrimSpace(asString(value["providerID"])); providerID != "" {
			body["providerID"] = providerID
		}
		if modelID := strings.TrimSpace(asString(value["modelID"])); modelID != "" {
			body["modelID"] = modelID
		}
		if variant := strings.TrimSpace(asString(value["variant"])); variant != "" {
			body["variant"] = variant
		}
	case string:
		if value != "" {
			body["model"] = value
		}
	}
}

func isKnownOpenCodeProvider(provider string) bool {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "openrouter", "openai", "opencode", "anthropic", "google", "github-copilot", "github":
		return true
	default:
		return false
	}
}

func extractOpenCodeErrorMessage(value any) string {
	switch v := value.(type) {
	case map[string]any:
		if strings.TrimSpace(asString(v["type"])) == "error" {
			if errObj, ok := v["error"].(map[string]any); ok {
				if data, ok := errObj["data"].(map[string]any); ok {
					if msg := strings.TrimSpace(asString(data["message"])); msg != "" {
						return msg
					}
				}
				if msg := strings.TrimSpace(asString(errObj["message"])); msg != "" {
					return msg
				}
			}
			if msg := strings.TrimSpace(asString(v["message"])); msg != "" {
				return msg
			}
		}
		for _, item := range v {
			if msg := extractOpenCodeErrorMessage(item); msg != "" {
				return msg
			}
		}
	case []any:
		for _, item := range v {
			if msg := extractOpenCodeErrorMessage(item); msg != "" {
				return msg
			}
		}
	}
	return ""
}
