package adminflow

import (
	"strings"

	"or3-intern/internal/configmeta"
)

type riskEscalationRule struct {
	reason  string
	minRisk configmeta.RiskLevel
	match   func(*SettingsChangePlan) bool
}

var riskEscalationRules = []riskEscalationRule{
	{
		reason:  "restart required",
		minRisk: configmeta.RiskNotice,
		match:   func(plan *SettingsChangePlan) bool { return plan != nil && plan.RestartRequired },
	},
	{
		reason:  "skill authentication change",
		minRisk: configmeta.RiskWarning,
		match:   func(plan *SettingsChangePlan) bool { return plan != nil && hasSkillAuthChange(plan.Changes) },
	},
	{
		reason:  "tool permission change",
		minRisk: configmeta.RiskWarning,
		match:   func(plan *SettingsChangePlan) bool { return plan != nil && hasToolPermissionChange(plan.Changes) },
	},
	{
		reason:  "file scope change",
		minRisk: configmeta.RiskWarning,
		match:   func(plan *SettingsChangePlan) bool { return plan != nil && hasFileScopeChange(plan.Changes) },
	},
	{
		reason:  "shell, network, or service exposure",
		minRisk: configmeta.RiskDanger,
		match:   func(plan *SettingsChangePlan) bool { return plan != nil && hasShellNetworkServiceExposure(plan.Changes) },
	},
	{
		reason:  "approval posture change",
		minRisk: configmeta.RiskDanger,
		match:   func(plan *SettingsChangePlan) bool { return plan != nil && hasApprovalPostureChange(plan.Changes) },
	},
	{
		reason:  "automation change",
		minRisk: configmeta.RiskWarning,
		match:   func(plan *SettingsChangePlan) bool { return plan != nil && hasAutomationChange(plan.Changes) },
	},
}

// ClassifyRisk computes the final risk level for a plan based on metadata,
// change content, affected areas, restart need, file scopes, command classes,
// skill auth changes, and security posture changes.
func ClassifyRisk(plan *SettingsChangePlan) RiskDecision {
	if plan == nil || len(plan.Changes) == 0 {
		return RiskDecision{
			Level:  configmeta.RiskSafe,
			Reason: "no changes",
		}
	}

	applyPlanMetadata(plan)

	decision := RiskDecision{
		Level:             configmeta.RiskSafe,
		RequiresApproval:  false,
		RequiresStepUp:    false,
		RequiresRestart:   plan.RestartRequired,
		EscalationReasons: []string{},
	}

	for _, change := range plan.Changes {
		if configmeta.RiskRank(change.MetadataRisk) > configmeta.RiskRank(decision.Level) {
			decision.Level = change.MetadataRisk
		}
	}

	for _, rule := range riskEscalationRules {
		if rule.match(plan) {
			escalateRisk(&decision, rule.reason, rule.minRisk)
		}
	}

	switch decision.Level {
	case configmeta.RiskWarning:
		decision.RequiresApproval = true
		decision.RequiresStepUp = true
		decision.Reason = "warning-level change requires explicit consent and identity verification"
	case configmeta.RiskDanger:
		decision.RequiresApproval = true
		decision.RequiresStepUp = true
		decision.Reason = "danger-level change requires admin approval with passkey or PIN"
	case configmeta.RiskNotice:
		decision.Reason = "notice-level change will be applied with confirmation"
	default:
		decision.Reason = "safe change can be applied automatically"
	}

	return decision
}

func applyPlanMetadata(plan *SettingsChangePlan) {
	if plan == nil {
		return
	}
	maxMetaRisk := configmeta.RiskSafe
	for i := range plan.Changes {
		change := &plan.Changes[i]
		meta, ok := lookupChangeMetadata(*change)
		if !ok {
			continue
		}
		if change.MetadataRisk == "" {
			change.MetadataRisk = meta.Risk
		}
		maxMetaRisk = configmeta.HigherRisk(maxMetaRisk, change.MetadataRisk)
		if meta.RestartRequired {
			plan.RestartRequired = true
		}
		if meta.RequiresApproval {
			plan.RequiresApproval = true
		}
		if meta.RequiresStepUp {
			plan.RequiresStepUpAuth = true
		}
	}
	if plan.RiskLevel == "" {
		plan.RiskLevel = maxMetaRisk
	}
}

func escalateRisk(decision *RiskDecision, reason string, minRisk configmeta.RiskLevel) {
	decision.EscalationReasons = append(decision.EscalationReasons, reason)
	if configmeta.RiskRank(decision.Level) < configmeta.RiskRank(minRisk) {
		decision.Level = minRisk
	}
}

// hasSkillAuthChange checks if any change affects skill authentication.
func hasSkillAuthChange(changes []SettingsPlanChange) bool {
	for _, change := range changes {
		if strings.Contains(change.ConfigPath, "skills.") &&
			(strings.Contains(change.Field, "api_key") ||
				strings.Contains(change.Field, "token") ||
				strings.Contains(change.Field, "credential")) {
			return true
		}
	}
	return false
}

// hasToolPermissionChange checks if any change affects tool permissions.
func hasToolPermissionChange(changes []SettingsPlanChange) bool {
	for _, change := range changes {
		if strings.Contains(change.ConfigPath, "tools.") &&
			(strings.Contains(change.Field, "enable") ||
				strings.Contains(change.Field, "allowed") ||
				strings.Contains(change.Field, "restrict")) {
			return true
		}
		if strings.Contains(change.ConfigPath, "hardening.") {
			return true
		}
	}
	return false
}

// hasFileScopeChange checks if any change affects file scope.
func hasFileScopeChange(changes []SettingsPlanChange) bool {
	for _, change := range changes {
		if strings.Contains(change.Field, "workspace") ||
			strings.Contains(change.Field, "allowed_dir") ||
			strings.Contains(change.Field, "writable_paths") {
			return true
		}
	}
	return false
}

// hasShellNetworkServiceExposure checks if any change exposes shell, network, or service.
func hasShellNetworkServiceExposure(changes []SettingsPlanChange) bool {
	for _, change := range changes {
		if strings.Contains(change.ConfigPath, "tools.enableExec") ||
			strings.Contains(change.ConfigPath, "hardening.enableExecShell") {
			if isTruthyValue(change.NewValue.Value) {
				return true
			}
		}
		if strings.Contains(change.ConfigPath, "service.listen") ||
			strings.Contains(change.ConfigPath, "sandbox.allowNetwork") {
			return true
		}
		if strings.Contains(change.ConfigPath, "service.enabled") {
			if isTruthyValue(change.NewValue.Value) {
				return true
			}
		}
	}
	return false
}

// hasApprovalPostureChange checks if any change affects approval posture.
func hasApprovalPostureChange(changes []SettingsPlanChange) bool {
	for _, change := range changes {
		if strings.Contains(change.ConfigPath, "security.approvals") ||
			strings.Contains(change.Field, "approval") ||
			strings.Contains(change.Field, "step_up") {
			return true
		}
	}
	return false
}

// hasAutomationChange checks if any change affects automation.
func hasAutomationChange(changes []SettingsPlanChange) bool {
	for _, change := range changes {
		if strings.Contains(change.ConfigPath, "cron.") ||
			strings.Contains(change.ConfigPath, "heartbeat.") ||
			strings.Contains(change.ConfigPath, "triggers.") {
			return true
		}
	}
	return false
}
