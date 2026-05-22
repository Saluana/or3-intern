package adminflow

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"

	"or3-intern/internal/config"
	"or3-intern/internal/configedit"
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

type PlanValidator struct{}

func (PlanValidator) Stage(current config.Config, plan *SettingsChangePlan, opts ValidationOptions) (ValidationState, error) {
	if plan == nil {
		return ValidationState{}, fmt.Errorf("%w: plan is required", ErrPlanValidation)
	}

	next := cloneConfig(current)
	results := make([]PlanValidationResult, 0, len(plan.Changes)+4)

	for i := range plan.Changes {
		change := plan.Changes[i]
		if err := validateExpectedOldValue(current, change); err != nil {
			results = append(results, PlanValidationResult{Check: staleCheckName(change), Status: "fail", Message: err.Error()})
			plan.ValidationResults = results
			return ValidationState{Validation: results}, ErrStalePlan
		}
		results = append(results, PlanValidationResult{Check: staleCheckName(change), Status: "pass"})

		changed, err := applyPlanChange(&next, change)
		if err != nil {
			results = append(results, PlanValidationResult{Check: applyCheckName(change), Status: "fail", Message: err.Error()})
			plan.ValidationResults = results
			return ValidationState{Validation: results}, fmt.Errorf("%w: %s", ErrPlanValidation, err)
		}
		if !changed {
			message := fmt.Sprintf("unsupported field update: %s.%s", change.Section, change.Field)
			results = append(results, PlanValidationResult{Check: applyCheckName(change), Status: "fail", Message: message})
			plan.ValidationResults = results
			return ValidationState{Validation: results}, fmt.Errorf("%w: %s", ErrPlanValidation, message)
		}
		results = append(results, PlanValidationResult{Check: applyCheckName(change), Status: "pass"})
	}

	if err := config.ValidateSnapshot(next); err != nil {
		results = append(results, PlanValidationResult{Check: "config.validate", Status: "fail", Message: err.Error()})
		plan.ValidationResults = results
		return ValidationState{StagedConfig: next, Validation: results}, fmt.Errorf("%w: %s", ErrPlanValidation, err)
	}
	results = append(results, PlanValidationResult{Check: "config.validate", Status: "pass"})

	report := doctor.Evaluate(next, doctor.Options{Mode: doctor.ModeConfigurePostSave})
	if report.Summary.BlockCount > 0 {
		results = append(results, PlanValidationResult{Check: "doctor.configure_post_save", Status: "fail", Message: summarizeDoctorBlocks(report)})
		plan.ValidationResults = results
		return ValidationState{StagedConfig: next, DoctorReport: report, Validation: results}, fmt.Errorf("%w: doctor reported blocking findings", ErrPlanValidation)
	}
	results = append(results, PlanValidationResult{Check: "doctor.configure_post_save", Status: "pass"})

	decoratePlanFromMetadata(plan)
	decision := ClassifyRisk(plan)
	if exceedsApprovedAuthority(decision.Level, opts.ApprovedAuthority) {
		results = append(results, PlanValidationResult{Check: "risk.authority", Status: "fail", Message: fmt.Sprintf("computed risk %s exceeds approved authority %s", decision.Level, opts.ApprovedAuthority)})
		plan.ValidationResults = results
		return ValidationState{StagedConfig: next, DoctorReport: report, RiskDecision: decision, Validation: results}, ErrPlanRiskExceeded
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

func applyPlanChange(cfg *config.Config, change SettingsPlanChange) (bool, error) {
	op := strings.ToLower(strings.TrimSpace(change.Operation))
	switch op {
	case "", "set":
		return configedit.ApplyFieldValue(cfg, change.Section, change.Channel, change.Field, stringifyPlanValue(change.NewValue.Value))
	case "toggle":
		value, ok := boolPlanValue(change.NewValue.Value)
		if !ok {
			return false, fmt.Errorf("toggle operation requires boolean new value")
		}
		return configedit.SetToggleFieldValue(cfg, change.Section, change.Channel, change.Field, value), nil
	case "choose":
		return configedit.ApplyChoiceSelection(cfg, change.Section, change.Channel, change.Field, stringifyPlanValue(change.NewValue.Value))
	default:
		return false, fmt.Errorf("unsupported operation %q", change.Operation)
	}
}

func validateExpectedOldValue(current config.Config, change SettingsPlanChange) error {
	if strings.TrimSpace(change.ConfigPath) == "" {
		return nil
	}
	currentValue, ok := resolveConfigPathValue(current, change.ConfigPath)
	if !ok {
		return nil
	}
	if change.OldValue.Redacted {
		present := valuePresent(currentValue)
		if present != change.OldValue.Present {
			return fmt.Errorf("expected previous secret presence %t for %s", change.OldValue.Present, change.ConfigPath)
		}
		return nil
	}
	if !planValuesEqual(currentValue, change.OldValue.Value) {
		return fmt.Errorf("expected previous value %v for %s", change.OldValue.Value, change.ConfigPath)
	}
	return nil
}

func resolveConfigPathValue(current config.Config, path string) (any, bool) {
	segments := strings.Split(strings.TrimSpace(path), ".")
	if len(segments) == 0 {
		return nil, false
	}
	value := reflect.ValueOf(current)
	for _, segment := range segments {
		for value.Kind() == reflect.Pointer {
			if value.IsNil() {
				return nil, false
			}
			value = value.Elem()
		}
		switch value.Kind() {
		case reflect.Struct:
			found := false
			typ := value.Type()
			for i := 0; i < value.NumField(); i++ {
				field := typ.Field(i)
				tag := strings.Split(field.Tag.Get("json"), ",")[0]
				if tag == segment {
					value = value.Field(i)
					found = true
					break
				}
			}
			if !found {
				return nil, false
			}
		case reflect.Map:
			key := reflect.ValueOf(segment)
			item := value.MapIndex(key)
			if !item.IsValid() {
				return nil, false
			}
			value = item
		default:
			return nil, false
		}
	}
	for value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return nil, true
		}
		value = value.Elem()
	}
	return value.Interface(), true
}

func planValuesEqual(left, right any) bool {
	leftJSON, leftErr := json.Marshal(left)
	rightJSON, rightErr := json.Marshal(right)
	if leftErr == nil && rightErr == nil {
		return string(leftJSON) == string(rightJSON)
	}
	return fmt.Sprint(left) == fmt.Sprint(right)
}

func valuePresent(value any) bool {
	if value == nil {
		return false
	}
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed) != ""
	case []string:
		return len(typed) > 0
	case bool:
		return typed
	default:
		zero := reflect.Zero(reflect.TypeOf(value)).Interface()
		return !reflect.DeepEqual(value, zero)
	}
}

func decoratePlanFromMetadata(plan *SettingsChangePlan) {
	if plan == nil {
		return
	}
	decision := RiskDecision{Level: configmeta.RiskSafe}
	for i := range plan.Changes {
		change := &plan.Changes[i]
		meta, ok := configmeta.GetByPath(change.ConfigPath)
		if !ok && strings.TrimSpace(change.Section) != "" && strings.TrimSpace(change.Field) != "" {
			meta, ok = configmeta.Get(change.Section, change.Field)
		}
		if !ok {
			continue
		}
		change.MetadataRisk = meta.Risk
		decision.Level = configmeta.HigherRisk(decision.Level, meta.Risk)
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
		plan.RiskLevel = decision.Level
	}
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

func staleCheckName(change SettingsPlanChange) string {
	if strings.TrimSpace(change.ConfigPath) != "" {
		return "stale." + change.ConfigPath
	}
	if strings.TrimSpace(change.Channel) != "" {
		return "stale." + change.Section + "." + change.Channel + "." + change.Field
	}
	return "stale." + change.Section + "." + change.Field
}

func applyCheckName(change SettingsPlanChange) string {
	if strings.TrimSpace(change.ConfigPath) != "" {
		return "apply." + change.ConfigPath
	}
	if strings.TrimSpace(change.Channel) != "" {
		return "apply." + change.Section + "." + change.Channel + "." + change.Field
	}
	return "apply." + change.Section + "." + change.Field
}

func cloneConfig(current config.Config) config.Config {
	data, err := json.Marshal(current)
	if err != nil {
		return current
	}
	var next config.Config
	if err := json.Unmarshal(data, &next); err != nil {
		return current
	}
	return next
}

func boolPlanValue(value any) (bool, bool) {
	switch typed := value.(type) {
	case bool:
		return typed, true
	case string:
		lower := strings.ToLower(strings.TrimSpace(typed))
		switch lower {
		case "1", "true", "yes", "on":
			return true, true
		case "0", "false", "no", "off":
			return false, true
		}
	}
	return false, false
}

func stringifyPlanValue(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case bool:
		if typed {
			return "true"
		}
		return "false"
	case int:
		return strconv.Itoa(typed)
	case int64:
		return strconv.FormatInt(typed, 10)
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	default:
		return fmt.Sprint(value)
	}
}

func planLiveReloadKeys(plan SettingsChangePlan) []string {
	keys := make([]string, 0, 1)
	for _, change := range plan.Changes {
		if strings.HasPrefix(change.ConfigPath, "modelRouting.") || strings.HasPrefix(change.Field, "routing_") {
			keys = append(keys, "model_routing")
			break
		}
	}
	return keys
}
