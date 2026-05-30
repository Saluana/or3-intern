package adminflow

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"or3-intern/internal/config"
	"or3-intern/internal/configmeta"
	"or3-intern/internal/doctor"
)

var (
	ErrStalePlan        = errors.New("settings plan is stale")
	ErrPlanValidation   = errors.New("settings plan validation failed")
	ErrPlanRiskExceeded = errors.New("settings plan risk exceeds approved authority")
)

type ValidationOptions struct {
	ApprovedAuthority configmeta.RiskLevel
}

type ValidationState struct {
	StagedConfig   config.Config
	DoctorReport   doctor.Report
	RiskDecision   RiskDecision
	Validation     []PlanValidationResult
	LiveReloadKeys []string
}

// StagePlan validates and stages a settings change plan against an in-memory config copy.
func StagePlan(current config.Config, plan *SettingsChangePlan, opts ValidationOptions) (ValidationState, error) {
	if plan == nil {
		return ValidationState{}, fmt.Errorf("%w: plan is required", ErrPlanValidation)
	}

	NormalizePlanChanges(plan.Changes)

	next, err := cloneConfig(current)
	if err != nil {
		return ValidationState{}, fmt.Errorf("%w: %s", ErrPlanValidation, err)
	}
	results := make([]PlanValidationResult, 0, len(plan.Changes)+4)

	for i := range plan.Changes {
		change := plan.Changes[i]
		if err := validateExpectedOldValue(current, change); err != nil {
			return abortStage(plan, ValidationState{}, results, staleCheckName(change), err.Error(), ErrStalePlan)
		}
		results = append(results, PlanValidationResult{Check: staleCheckName(change), Status: "pass"})

		changed, err := applyPlanChange(&next, change)
		if err != nil {
			return abortStage(plan, ValidationState{}, results, applyCheckName(change), err.Error(), fmt.Errorf("%w: %s", ErrPlanValidation, err))
		}
		if !changed {
			message := fmt.Sprintf("unsupported field update: %s", changeDisplayPath(change))
			return abortStage(plan, ValidationState{}, results, applyCheckName(change), message, fmt.Errorf("%w: %s", ErrPlanValidation, message))
		}
		if err := validatePlanChangeValue(change); err != nil {
			return abortStage(plan, ValidationState{}, results, applyCheckName(change), err.Error(), fmt.Errorf("%w: %s", ErrPlanValidation, err))
		}
		results = append(results, PlanValidationResult{Check: applyCheckName(change), Status: "pass"})
	}

	if err := config.ValidateSnapshot(next); err != nil {
		return abortStage(plan, ValidationState{StagedConfig: next}, results, "config.validate", err.Error(), fmt.Errorf("%w: %s", ErrPlanValidation, err))
	}
	results = append(results, PlanValidationResult{Check: "config.validate", Status: "pass"})

	report := doctor.Evaluate(next, doctor.Options{Mode: doctor.ModeConfigurePostSave})
	if report.Summary.BlockCount > 0 {
		message := summarizeDoctorBlocks(report)
		return abortStage(plan, ValidationState{StagedConfig: next, DoctorReport: report}, results, "doctor.configure_post_save", message, fmt.Errorf("%w: doctor reported blocking findings", ErrPlanValidation))
	}
	results = append(results, PlanValidationResult{Check: "doctor.configure_post_save", Status: "pass"})

	decision := ClassifyRisk(plan)
	if exceedsApprovedAuthority(decision.Level, opts.ApprovedAuthority) {
		message := fmt.Sprintf("computed risk %s exceeds approved authority %s", decision.Level, opts.ApprovedAuthority)
		state := ValidationState{StagedConfig: next, DoctorReport: report, RiskDecision: decision}
		return abortStage(plan, state, results, "risk.authority", message, ErrPlanRiskExceeded)
	}
	results = append(results, PlanValidationResult{Check: "risk.authority", Status: "pass"})

	plan.RiskLevel = decision.Level
	plan.RequiresApproval = decision.RequiresApproval
	plan.RequiresStepUpAuth = decision.RequiresStepUp
	plan.RestartRequired = decision.RequiresRestart
	plan.ValidationResults = results

	return ValidationState{
		StagedConfig:   next,
		DoctorReport:   report,
		RiskDecision:   decision,
		Validation:     results,
		LiveReloadKeys: planLiveReloadKeys(*plan),
	}, nil
}

func abortStage(plan *SettingsChangePlan, state ValidationState, results []PlanValidationResult, check, message string, err error) (ValidationState, error) {
	results = append(results, PlanValidationResult{Check: check, Status: "fail", Message: message})
	plan.ValidationResults = results
	state.Validation = results
	return state, err
}

func cloneConfig(current config.Config) (config.Config, error) {
	data, err := json.Marshal(current)
	if err != nil {
		return config.Config{}, fmt.Errorf("clone config: marshal: %w", err)
	}
	var next config.Config
	if err := json.Unmarshal(data, &next); err != nil {
		return config.Config{}, fmt.Errorf("clone config: unmarshal: %w", err)
	}
	return next, nil
}

func summarizeDoctorBlocks(report doctor.Report) string {
	blocks := make([]string, 0, report.Summary.BlockCount)
	for _, finding := range report.Findings {
		if finding.Severity == doctor.SeverityBlock {
			blocks = append(blocks, finding.Summary)
		}
	}
	if len(blocks) == 0 {
		return "doctor reported blocking findings"
	}
	return strings.Join(blocks, "; ")
}

func exceedsApprovedAuthority(level, approved configmeta.RiskLevel) bool {
	if approved == "" {
		return false
	}
	return configmeta.RiskRank(level) > configmeta.RiskRank(approved)
}

func planLiveReloadKeys(plan SettingsChangePlan) []string {
	keys := make([]string, 0, 1)
	for _, change := range plan.Changes {
		if isModelRoutingChange(change) {
			keys = append(keys, "model_routing")
			break
		}
	}
	return keys
}

func isModelRoutingChange(change SettingsPlanChange) bool {
	path := strings.TrimSpace(change.ConfigPath)
	field := strings.TrimSpace(change.Field)
	return strings.HasPrefix(path, "modelRouting.") || strings.HasPrefix(field, "routing_")
}
