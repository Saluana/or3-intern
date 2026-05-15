package doctor

import (
	"strings"

	"or3-intern/internal/config"
)

func providerFindings(cfg config.Config, opts Options) []Finding {
	blockOnStartup := opts.Mode == ModeStartupChat || opts.Mode == ModeStartupServe || opts.Mode == ModeConfigurePostSave
	findings := []Finding{}
	if strings.TrimSpace(cfg.Provider.APIBase) == "" {
		findings = append(findings, Finding{
			ID:       "provider.endpoint_missing",
			Area:     "provider",
			Severity: severityFor(opts.Mode, SeverityWarn, blockOnStartup),
			Summary:  "AI provider endpoint is missing",
			Detail:   "OR3 needs an OpenAI-compatible endpoint before chat can start.",
			FixMode:  FixModeInteractive,
			FixHint:  "Run `or3-intern setup` or `or3-intern settings --section provider` and choose your provider.",
		})
	}
	if strings.TrimSpace(cfg.Provider.APIKey) == "" {
		findings = append(findings, Finding{
			ID:       "provider.api_key_missing",
			Area:     "provider",
			Severity: severityFor(opts.Mode, SeverityWarn, blockOnStartup),
			Summary:  "AI provider key is missing",
			Detail:   "The provider key is required to verify billing and access with the AI service.",
			FixMode:  FixModeInteractive,
			FixHint:  "Set the provider key in the environment or run `or3-intern setup` to save it locally.",
		})
	}
	if strings.TrimSpace(cfg.Provider.Model) == "" {
		findings = append(findings, Finding{
			ID:       "provider.chat_model_missing",
			Area:     "provider",
			Severity: severityFor(opts.Mode, SeverityWarn, blockOnStartup),
			Summary:  "Chat model is missing",
			Detail:   "OR3 needs a chat model name before it can send messages.",
			FixMode:  FixModeInteractive,
			FixHint:  "Run `or3-intern setup` or `or3-intern settings --section provider` and choose a chat model.",
		})
	}
	if strings.TrimSpace(cfg.Provider.EmbedModel) == "" {
		findings = append(findings, Finding{
			ID:       "provider.embedding_model_missing",
			Area:     "provider",
			Severity: SeverityWarn,
			Summary:  "Embedding model is missing",
			Detail:   "Document search and memory retrieval work best with an embedding model configured.",
			FixMode:  FixModeInteractive,
			FixHint:  "Run `or3-intern setup` or `or3-intern settings --section provider` and choose an embedding model.",
		})
	}
	return findings
}
