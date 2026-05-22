package adminflow

import (
	"strings"

	"or3-intern/internal/configmeta"
)

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

	decision := RiskDecision{
		Level:             configmeta.RiskSafe,
		RequiresApproval:  false,
		RequiresStepUp:    false,
		RequiresRestart:   plan.RestartRequired,
		EscalationReasons: []string{},
	}

	// Start with the highest risk from individual changes
	for _, change := range plan.Changes {
		if configmeta.RiskRank(change.MetadataRisk) > configmeta.RiskRank(decision.Level) {
			decision.Level = change.MetadataRisk
		}
	}

	// Escalation rules - add reasons when conditions are met, escalate level if needed

	// Restart requirement escalates to at least notice
	if plan.RestartRequired {
		decision.EscalationReasons = append(decision.EscalationReasons, "restart required")
		if configmeta.RiskRank(decision.Level) < configmeta.RiskRank(configmeta.RiskNotice) {
			decision.Level = configmeta.RiskNotice
		}
	}

	// Skill auth changes escalate to warning
	if hasSkillAuthChange(plan.Changes) {
		decision.EscalationReasons = append(decision.EscalationReasons, "skill authentication change")
		if configmeta.RiskRank(decision.Level) < configmeta.RiskRank(configmeta.RiskWarning) {
			decision.Level = configmeta.RiskWarning
		}
	}

	// Tool permission changes escalate to warning
	if hasToolPermissionChange(plan.Changes) {
		decision.EscalationReasons = append(decision.EscalationReasons, "tool permission change")
		if configmeta.RiskRank(decision.Level) < configmeta.RiskRank(configmeta.RiskWarning) {
			decision.Level = configmeta.RiskWarning
		}
	}

	// File scope changes escalate to warning
	if hasFileScopeChange(plan.Changes) {
		decision.EscalationReasons = append(decision.EscalationReasons, "file scope change")
		if configmeta.RiskRank(decision.Level) < configmeta.RiskRank(configmeta.RiskWarning) {
			decision.Level = configmeta.RiskWarning
		}
	}

	// Shell/network/service exposure escalates to danger
	if hasShellNetworkServiceExposure(plan.Changes) {
		decision.EscalationReasons = append(decision.EscalationReasons, "shell, network, or service exposure")
		if configmeta.RiskRank(decision.Level) < configmeta.RiskRank(configmeta.RiskDanger) {
			decision.Level = configmeta.RiskDanger
		}
	}

	// Approval posture changes escalate to danger
	if hasApprovalPostureChange(plan.Changes) {
		decision.EscalationReasons = append(decision.EscalationReasons, "approval posture change")
		if configmeta.RiskRank(decision.Level) < configmeta.RiskRank(configmeta.RiskDanger) {
			decision.Level = configmeta.RiskDanger
		}
	}

	// Automation changes escalate to warning
	if hasAutomationChange(plan.Changes) {
		decision.EscalationReasons = append(decision.EscalationReasons, "automation change")
		if configmeta.RiskRank(decision.Level) < configmeta.RiskRank(configmeta.RiskWarning) {
			decision.Level = configmeta.RiskWarning
		}
	}

	// Set approval requirements based on final risk level
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
		// Shell exposure
		if strings.Contains(change.ConfigPath, "tools.enableExec") ||
			strings.Contains(change.ConfigPath, "hardening.enableExecShell") {
			if isTruthyValue(change.NewValue.Value) {
				return true
			}
		}
		// Network exposure
		if strings.Contains(change.ConfigPath, "service.listen") ||
			strings.Contains(change.ConfigPath, "sandbox.allowNetwork") {
			return true
		}
		// Service exposure
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

// isTruthyValue checks if a value represents a truthy state.
func isTruthyValue(v any) bool {
	if v == nil {
		return false
	}
	switch val := v.(type) {
	case bool:
		return val
	case string:
		lower := strings.ToLower(strings.TrimSpace(val))
		return lower == "true" || lower == "on" || lower == "yes" || lower == "1"
	case int, int32, int64:
		return val != 0
	}
	return false
}
