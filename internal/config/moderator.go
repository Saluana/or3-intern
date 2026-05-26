package config

import "strings"

const (
	defaultApprovalModeratorTimeoutSeconds = 8
	defaultApprovalModeratorMaxPromptChars = 12000
	defaultApprovalModeratorMaxSubjectChars = 4000
	maxApprovalModeratorReasonChars         = 500
)

func defaultApprovalModeratorConfig() ApprovalModeratorConfig {
	return ApprovalModeratorConfig{
		Enabled:         false,
		Preset:          ApprovalModeratorPresetBalanced,
		TimeoutSeconds:  defaultApprovalModeratorTimeoutSeconds,
		MaxPromptChars:  defaultApprovalModeratorMaxPromptChars,
		MaxSubjectChars: defaultApprovalModeratorMaxSubjectChars,
		FailureAction:   ApprovalModeratorActionEscalate,
		Actions:         actionsForApprovalModeratorPreset(ApprovalModeratorPresetBalanced),
	}
}

func actionsForApprovalModeratorPreset(preset ApprovalModeratorPreset) ApprovalModeratorActionMap {
	switch normalizeApprovalModeratorPreset(preset) {
	case ApprovalModeratorPresetCautious:
		return ApprovalModeratorActionMap{
			Low: ApprovalModeratorActionApprove, Medium: ApprovalModeratorActionEscalate,
			High: ApprovalModeratorActionEscalate, Extreme: ApprovalModeratorActionDeny,
		}
	case ApprovalModeratorPresetHandsOff:
		return ApprovalModeratorActionMap{
			Low: ApprovalModeratorActionApprove, Medium: ApprovalModeratorActionApprove,
			High: ApprovalModeratorActionApprove, Extreme: ApprovalModeratorActionEscalate,
		}
	case ApprovalModeratorPresetManual:
		return ApprovalModeratorActionMap{
			Low: ApprovalModeratorActionEscalate, Medium: ApprovalModeratorActionEscalate,
			High: ApprovalModeratorActionEscalate, Extreme: ApprovalModeratorActionEscalate,
		}
	default:
		return ApprovalModeratorActionMap{
			Low: ApprovalModeratorActionApprove, Medium: ApprovalModeratorActionApprove,
			High: ApprovalModeratorActionEscalate, Extreme: ApprovalModeratorActionDeny,
		}
	}
}

func (m ApprovalModeratorConfig) EffectiveActions() ApprovalModeratorActionMap {
	if m.actionsExplicitlySet() {
		return m.Actions
	}
	if preset := normalizeApprovalModeratorPreset(m.Preset); preset != "" {
		return actionsForApprovalModeratorPreset(preset)
	}
	return m.Actions
}

func (m ApprovalModeratorConfig) actionsExplicitlySet() bool {
	return m.Actions.Low != "" || m.Actions.Medium != "" || m.Actions.High != "" || m.Actions.Extreme != ""
}

// NormalizeApprovalModerator applies defaults and preset-derived action mappings.
func NormalizeApprovalModerator(cfg *ApprovalModeratorConfig) {
	normalizeApprovalModerator(cfg)
}

func normalizeApprovalModerator(cfg *ApprovalModeratorConfig) {
	if cfg == nil {
		return
	}
	def := defaultApprovalModeratorConfig()
	if cfg.TimeoutSeconds <= 0 {
		cfg.TimeoutSeconds = def.TimeoutSeconds
	}
	if cfg.MaxPromptChars <= 0 {
		cfg.MaxPromptChars = def.MaxPromptChars
	}
	if cfg.MaxSubjectChars <= 0 {
		cfg.MaxSubjectChars = def.MaxSubjectChars
	}
	if strings.TrimSpace(string(cfg.FailureAction)) == "" {
		cfg.FailureAction = def.FailureAction
	} else if normalized := normalizeApprovalModeratorAction(cfg.FailureAction); normalized != "" {
		cfg.FailureAction = normalized
	}
	if normalized := normalizeApprovalModeratorPreset(cfg.Preset); normalized != "" {
		cfg.Preset = normalized
	} else if strings.TrimSpace(string(cfg.Preset)) == "" {
		cfg.Preset = def.Preset
	}
	if !cfg.actionsExplicitlySet() {
		cfg.Actions = actionsForApprovalModeratorPreset(cfg.Preset)
	} else {
		fallback := actionsForApprovalModeratorPreset(cfg.Preset)
		cfg.Actions.Low = fillApprovalModeratorAction(cfg.Actions.Low, fallback.Low)
		cfg.Actions.Medium = fillApprovalModeratorAction(cfg.Actions.Medium, fallback.Medium)
		cfg.Actions.High = fillApprovalModeratorAction(cfg.Actions.High, fallback.High)
		cfg.Actions.Extreme = fillApprovalModeratorAction(cfg.Actions.Extreme, fallback.Extreme)
	}
	cfg.UserPolicy = strings.TrimSpace(cfg.UserPolicy)
	cfg.Provider = strings.TrimSpace(cfg.Provider)
	cfg.Model = strings.TrimSpace(cfg.Model)
}

func normalizeApprovalModeratorPreset(preset ApprovalModeratorPreset) ApprovalModeratorPreset {
	switch ApprovalModeratorPreset(strings.ToLower(strings.TrimSpace(string(preset)))) {
	case ApprovalModeratorPresetBalanced:
		return ApprovalModeratorPresetBalanced
	case ApprovalModeratorPresetCautious:
		return ApprovalModeratorPresetCautious
	case ApprovalModeratorPresetHandsOff:
		return ApprovalModeratorPresetHandsOff
	case ApprovalModeratorPresetManual:
		return ApprovalModeratorPresetManual
	default:
		return ""
	}
}

func fillApprovalModeratorAction(action, fallback ApprovalModeratorAction) ApprovalModeratorAction {
	if strings.TrimSpace(string(action)) == "" {
		return fallback
	}
	if normalized := normalizeApprovalModeratorAction(action); normalized != "" {
		return normalized
	}
	return action
}

func normalizeApprovalModeratorAction(action ApprovalModeratorAction) ApprovalModeratorAction {
	switch ApprovalModeratorAction(strings.ToLower(strings.TrimSpace(string(action)))) {
	case ApprovalModeratorActionApprove:
		return ApprovalModeratorActionApprove
	case ApprovalModeratorActionEscalate:
		return ApprovalModeratorActionEscalate
	case ApprovalModeratorActionDeny:
		return ApprovalModeratorActionDeny
	default:
		return ""
	}
}

func isValidApprovalModeratorAction(action ApprovalModeratorAction) bool {
	switch action {
	case ApprovalModeratorActionApprove, ApprovalModeratorActionEscalate, ApprovalModeratorActionDeny:
		return true
	default:
		return false
	}
}
