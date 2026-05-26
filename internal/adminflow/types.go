// Package adminflow owns SettingsChangePlan, risk classification, validation,
// apply, checkpoints, rollback, post-check orchestration, and redaction contracts.
package adminflow

import (
	"or3-intern/internal/configmeta"
)

// SettingsChangePlan represents a validated plan for config changes.
type SettingsChangePlan struct {
	ID                    string                 `json:"id"`
	Title                 string                 `json:"title"`
	Summary               string                 `json:"summary"`
	CreatedBy             string                 `json:"created_by"`
	CreatedAtUnixMs       int64                  `json:"created_at"`
	RiskLevel             configmeta.RiskLevel   `json:"risk_level"`
	RestartRequired       bool                   `json:"restart_required"`
	RequiresApproval      bool                   `json:"requires_approval"`
	RequiresStepUpAuth    bool                   `json:"requires_step_up_auth"`
	AffectedAreas         []string               `json:"affected_areas"`
	Changes               []SettingsPlanChange   `json:"changes"`
	ValidationResults     []PlanValidationResult `json:"validation_results"`
	EstimatedImpact       string                 `json:"estimated_impact"`
	RollbackPlan          RollbackPlan           `json:"rollback_plan"`
	PostApplyChecks       []PostApplyCheck       `json:"post_apply_checks"`
	UserFacingExplanation string                 `json:"user_facing_explanation"`
	ExactConfigDiff       []ConfigDiffLine       `json:"exact_config_diff,omitempty"`
	Advanced              map[string]any         `json:"advanced,omitempty"`
}

// SettingsPlanChange represents a single config change within a plan.
type SettingsPlanChange struct {
	ConfigPath       string               `json:"config_path"`
	Section          string               `json:"section"`
	Channel          string               `json:"channel,omitempty"`
	Field            string               `json:"field"`
	Operation        string               `json:"operation"`
	OldValue         RedactedValue        `json:"old_value"`
	NewValue         RedactedValue        `json:"new_value"`
	Impact           string               `json:"impact"`
	RiskReason       string               `json:"risk_reason"`
	ValidationStatus string               `json:"validation_status"`
	MetadataRisk     configmeta.RiskLevel `json:"metadata_risk"`
}

// RedactedValue represents a config value with redaction for secrets.
type RedactedValue struct {
	Value    any    `json:"value,omitempty"`
	Redacted bool   `json:"redacted,omitempty"`
	Present  bool   `json:"present,omitempty"`
	Summary  string `json:"summary,omitempty"`
}

// PlanValidationResult represents the result of validating a plan.
type PlanValidationResult struct {
	Check   string `json:"check"`
	Status  string `json:"status"` // "pass", "fail", "warning"
	Message string `json:"message,omitempty"`
}

// RollbackPlan describes how to rollback the plan if needed.
type RollbackPlan struct {
	Available       bool   `json:"available"`
	Safe            bool   `json:"safe"`
	ManualOnly      bool   `json:"manual_only,omitempty"`
	Instructions    string `json:"instructions,omitempty"`
	RestartRequired bool   `json:"restart_required,omitempty"`
}

// PostApplyCheck represents a check to run after applying the plan.
type PostApplyCheck struct {
	ID          string `json:"id"`
	Description string `json:"description"`
	Timeout     int    `json:"timeout_seconds,omitempty"`
}

// ConfigDiffLine represents a line in the exact config diff.
type ConfigDiffLine struct {
	Path     string `json:"path"`
	OldValue string `json:"old_value,omitempty"`
	NewValue string `json:"new_value,omitempty"`
	Added    bool   `json:"added,omitempty"`
	Removed  bool   `json:"removed,omitempty"`
}

// RiskDecision represents the result of risk classification for a plan.
type RiskDecision struct {
	Level             configmeta.RiskLevel `json:"level"`
	Reason            string               `json:"reason"`
	RequiresApproval  bool                 `json:"requires_approval"`
	RequiresStepUp    bool                 `json:"requires_step_up"`
	RequiresRestart   bool                 `json:"requires_restart"`
	EscalationReasons []string             `json:"escalation_reasons,omitempty"`
}

// ApprovalContext represents the approval state for a plan.
type ApprovalContext struct {
	PlanID             string `json:"plan_id"`
	Approved           bool   `json:"approved"`
	Approver           string `json:"approver,omitempty"`
	AuthMethod         string `json:"auth_method,omitempty"`
	ApprovedAtUnixMs   int64  `json:"approved_at,omitempty"`
	RememberForMinutes int    `json:"remember_for_minutes,omitempty"`
}

// ApplyResult represents the result of applying a plan.
type ApplyResult struct {
	Success          bool     `json:"success"`
	PlanID           string   `json:"plan_id"`
	RollbackID       string   `json:"rollback_id,omitempty"`
	RestartRequired  bool     `json:"restart_required"`
	RestartRequested bool     `json:"restart_requested,omitempty"`
	PostCheckPending bool     `json:"post_check_pending,omitempty"`
	PostCheckIDs     []string `json:"post_check_ids,omitempty"`
	Error            string   `json:"error,omitempty"`
}

// RollbackResult represents the result of rolling back a plan.
type RollbackResult struct {
	Success          bool   `json:"success"`
	RollbackID       string `json:"rollback_id"`
	PlanID           string `json:"plan_id"`
	RestartRequired  bool   `json:"restart_required"`
	RestartRequested bool   `json:"restart_requested,omitempty"`
	Error            string `json:"error,omitempty"`
}
