package main

import (
	"context"
	"fmt"
	"log"
	"strings"

	"or3-intern/internal/adminflow"
	"or3-intern/internal/configmeta"
)

const doctorDiagnosticLogMaxLimit = 200

func serviceDoctorApprovedAuthority(ctx context.Context) configmeta.RiskLevel {
	identity := serviceAuthIdentityFromContext(ctx)
	if identity.StepUpOK {
		return configmeta.RiskDanger
	}
	return configmeta.RiskNotice
}

func serviceDoctorPlanStatusAllowsApply(status string) bool {
	switch strings.TrimSpace(status) {
	case "", "validated":
		return true
	case "apply_state_unknown":
		return false
	default:
		return false
	}
}

func serviceDoctorPlanPersistRequiresStepUp(plan adminflow.SettingsChangePlan) bool {
	if plan.RequiresStepUpAuth || plan.RequiresApproval {
		return true
	}
	switch plan.RiskLevel {
	case configmeta.RiskWarning, configmeta.RiskDanger:
		return true
	default:
		return false
	}
}

func serviceDoctorPlanPersistAllowed(ctx context.Context, plan adminflow.SettingsChangePlan) error {
	if !serviceDoctorPlanPersistRequiresStepUp(plan) {
		return nil
	}
	if serviceAuthIdentityFromContext(ctx).StepUpOK {
		return nil
	}
	return fmt.Errorf("recent passkey verification is required before saving this plan")
}

func serviceDoctorPlanStatusAllowsRollback(status string) bool {
	switch strings.TrimSpace(status) {
	case "applied", "restart_pending", "restart_approval_required", "restart_start_failed", "post_checked", "post_check_failed":
		return true
	default:
		return false
	}
}

func isStrongDoctorSessionKey(key string) bool {
	key = strings.TrimSpace(key)
	if key == "" {
		return false
	}
	if strings.HasPrefix(key, "doctor-session_") {
		return len(key) >= len("doctor-session_")+8
	}
	if strings.HasPrefix(key, "doctor-app-") {
		return len(key) >= len("doctor-app-")+16
	}
	return len(key) >= 24
}

func clampDoctorDiagnosticLogLimit(requested int) (effective int) {
	effective = requested
	if effective <= 0 {
		effective = 100
	}
	if effective > doctorDiagnosticLogMaxLimit {
		effective = doctorDiagnosticLogMaxLimit
	}
	return effective
}

func validateDoctorApprovalForPlan(ctx context.Context, plan adminflow.SettingsChangePlan, approval adminflow.ApprovalContext) error {
	if strings.TrimSpace(approval.PlanID) != "" && strings.TrimSpace(approval.PlanID) != strings.TrimSpace(plan.ID) {
		return fmt.Errorf("approval plan_id does not match plan")
	}
	if plan.RequiresApproval && !approval.Approved {
		return fmt.Errorf("approval is required before apply")
	}
	if plan.RequiresStepUpAuth {
		identity := serviceAuthIdentityFromContext(ctx)
		if !identity.StepUpOK {
			return fmt.Errorf("recent passkey verification is required before applying this plan")
		}
	}
	return nil
}

func serviceDoctorRedactedRollbackChanges(plan adminflow.SettingsChangePlan) []adminflow.SettingsPlanChange {
	redacted := serviceDoctorRedactedPlanForAudit(plan)
	return redacted.Changes
}

func doctorLogWeakSessionKeyWarning(key string) {
	if strings.HasPrefix(key, "doctor-app-") && !isStrongDoctorSessionKey(key) {
		log.Printf("doctor: weak client session key format accepted for resume: %q", key)
	}
}
