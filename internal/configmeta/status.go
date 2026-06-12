package configmeta

import "or3-intern/internal/config"

// FieldStatus describes how configure/doctor/settings UIs should treat a field.
type FieldStatus string

const (
	FieldStatusActive        FieldStatus = "active"
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
	"provider.model":                         FieldStatusHidden,
	"provider.timeoutSeconds":                FieldStatusCompatibility,
	"provider.enableVision":                  FieldStatusHidden,
	"modelRouting.chat.primary.model":        FieldStatusHidden,
	"modelRouting.chat.primary.provider":     FieldStatusHidden,
	"modelRouting.agents.primary.model":      FieldStatusHidden,
	"modelRouting.agents.primary.provider":   FieldStatusHidden,
	"tools.enableExec":                       FieldStatusHidden,
	"tools.execTimeoutSeconds":               FieldStatusHidden,
	"tools.braveAPIKey":                      FieldStatusHidden,
	"tools.execAllowedPrograms":              FieldStatusHidden,
	"tools.pathAppend":                       FieldStatusHidden,
	"skills.enableExec":                      FieldStatusHidden,
	"security.approvals.skillExecution.mode": FieldStatusHidden,
	"security.approvals.exec.mode":           FieldStatusHidden,
	"context.maxInputTokens":                 FieldStatusHidden,
	"context.outputReserveTokens":            FieldStatusHidden,
	"context.safetyMarginTokens":             FieldStatusHidden,
	"maxToolLoops":                           FieldStatusHidden,
	"hardening.guardedTools":                 FieldStatusHidden,
	"hardening.privilegedTools":              FieldStatusHidden,
	"hardening.enableExecShell":              FieldStatusHidden,
	"hardening.execAllowedPrograms":          FieldStatusHidden,
	"service.maxCapability":                  FieldStatusHidden,
	"runners.default":                        FieldStatusHidden,
	"runners.maxConcurrent":                  FieldStatusHidden,
	"runners.maxQueued":                      FieldStatusHidden,
	"runners.defaultTimeoutSeconds":          FieldStatusHidden,
	"runners.maxTimeoutSeconds":              FieldStatusHidden,
	"runners.allowSandboxAuto":               FieldStatusHidden,
	"runners.defaultMode":                    FieldStatusHidden,
	"runners.defaultIsolation":               FieldStatusHidden,
	"runners.disabledRunners":                FieldStatusHidden,
}
