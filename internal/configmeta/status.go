package configmeta

import "or3-intern/internal/config"

// FieldStatus describes how configure/doctor/settings UIs should treat a field.
type FieldStatus string

const (
	FieldStatusActive        FieldStatus = "active"
	FieldStatusDeprecated    FieldStatus = "deprecated"
	FieldStatusHidden        FieldStatus = "hidden"
	FieldStatusCompatibility FieldStatus = "compatibility"
)

// StatusForConfigureKey returns the UI status for a configure TUI field key when runner-first is enabled.
func StatusForConfigureKey(cfg config.Config, key string) FieldStatus {
	if !cfg.RunnerFirst() {
		return FieldStatusActive
	}
	if meta, ok := configureFieldRunnerFirst[key]; ok {
		return meta.Status
	}
	return FieldStatusActive
}

// Annotate applies runner-first status to metadata entries for API consumers.
func Annotate(cfg config.Config, fields []ConfigFieldMetadata) []ConfigFieldMetadata {
	if !cfg.RunnerFirst() {
		return fields
	}
	out := make([]ConfigFieldMetadata, 0, len(fields))
	for _, field := range fields {
		status := StatusForPath(cfg, field.Path, field.Section, field.Key)
		if status == FieldStatusHidden {
			continue
		}
		field.Status = string(status)
		out = append(out, field)
	}
	return out
}

// StatusForPath resolves metadata status from a config path or section/key pair.
func StatusForPath(cfg config.Config, path, section, key string) FieldStatus {
	if !cfg.RunnerFirst() {
		return FieldStatusActive
	}
	if path != "" {
		if status, ok := runnerFirstPathStatus[path]; ok {
			return status
		}
	}
	if key != "" {
		if meta, ok := configureFieldRunnerFirst[key]; ok {
			return meta.Status
		}
	}
	if section != "" && key != "" {
		if meta, ok := configureFieldRunnerFirst[section+"_"+key]; ok {
			return meta.Status
		}
	}
	return FieldStatusActive
}

// ListForConfig returns registered metadata filtered and annotated for the active config mode.
func ListForConfig(cfg config.Config) []ConfigFieldMetadata {
	return Annotate(cfg, List())
}

var runnerFirstPathStatus = map[string]FieldStatus{
	"provider.model":                               FieldStatusCompatibility,
	"provider.timeoutSeconds":                      FieldStatusCompatibility,
	"provider.enableVision":                        FieldStatusDeprecated,
	"modelRouting.chat.primary.model":              FieldStatusCompatibility,
	"modelRouting.chat.primary.provider":           FieldStatusCompatibility,
	"modelRouting.agents.primary.model":            FieldStatusDeprecated,
	"modelRouting.agents.primary.provider":         FieldStatusDeprecated,
	"modelRouting.contextManager.primary.model":    FieldStatusDeprecated,
	"modelRouting.contextManager.primary.provider": FieldStatusDeprecated,
	"tools.enableExec":                             FieldStatusDeprecated,
	"tools.execTimeoutSeconds":                     FieldStatusDeprecated,
	"tools.braveAPIKey":                            FieldStatusDeprecated,
	"skills.enableExec":                            FieldStatusDeprecated,
	"security.approvals.skillExecution.mode":       FieldStatusDeprecated,
	"context.maxInputTokens":                       FieldStatusDeprecated,
	"context.outputReserveTokens":                  FieldStatusDeprecated,
	"context.safetyMarginTokens":                   FieldStatusDeprecated,
	"context.tools.dynamicExpose":                  FieldStatusDeprecated,
	"context.taskCard.enforcePlan":                 FieldStatusDeprecated,
	"contextManager.enabled":                       FieldStatusDeprecated,
	"contextManager.provider":                      FieldStatusDeprecated,
	"contextManager.model":                         FieldStatusDeprecated,
	"contextManager.timeoutSeconds":                FieldStatusDeprecated,
	"contextManager.idlePruneSeconds":              FieldStatusDeprecated,
	"contextManager.maxInputTokens":                FieldStatusDeprecated,
	"contextManager.maxOutputTokens":               FieldStatusDeprecated,
	"contextManager.allowTaskUpdates":              FieldStatusDeprecated,
	"contextManager.allowStalePropose":             FieldStatusDeprecated,
	"maxToolLoops":                                 FieldStatusDeprecated,
	"agentCLI.enabled":                             FieldStatusActive,
	"agentCLI.defaultRunner":                       FieldStatusActive,
}
