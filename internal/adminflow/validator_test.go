package adminflow

import (
	"errors"
	"strings"
	"testing"

	"or3-intern/internal/config"
	"or3-intern/internal/configmeta"
)

func TestPlanValidatorStage_Success(t *testing.T) {
	configmeta.Clear()
	configmeta.RegisterFirstSliceFields()

	cfg := config.Default()
	plan := &SettingsChangePlan{
		ID: "scp_test",
		Changes: []SettingsPlanChange{
			{
				ConfigPath: "provider.model",
				Section:    "provider",
				Field:      "provider_model",
				Operation:  "set",
				OldValue:   RedactedValue{Value: cfg.Provider.Model, Present: cfg.Provider.Model != ""},
				NewValue:   RedactedValue{Value: "gpt-4.1-mini"},
			},
		},
	}

	state, err := (PlanValidator{}).Stage(cfg, plan, ValidationOptions{ApprovedAuthority: configmeta.RiskWarning})
	if err != nil {
		t.Fatalf("Stage() error = %v", err)
	}
	if state.StagedConfig.Provider.Model != "gpt-4.1-mini" {
		t.Fatalf("staged model = %q", state.StagedConfig.Provider.Model)
	}
	if state.RiskDecision.Level != configmeta.RiskSafe {
		t.Fatalf("risk level = %s", state.RiskDecision.Level)
	}
	if len(state.Validation) == 0 {
		t.Fatal("expected validation results")
	}
}

func TestPlanValidatorStage_StalePlan(t *testing.T) {
	configmeta.Clear()
	configmeta.RegisterFirstSliceFields()

	cfg := config.Default()
	plan := &SettingsChangePlan{
		Changes: []SettingsPlanChange{
			{
				ConfigPath: "provider.model",
				Section:    "provider",
				Field:      "provider_model",
				Operation:  "set",
				OldValue:   RedactedValue{Value: "different-model", Present: true},
				NewValue:   RedactedValue{Value: "gpt-4.1-mini"},
			},
		},
	}

	_, err := (PlanValidator{}).Stage(cfg, plan, ValidationOptions{})
	if !errors.Is(err, ErrStalePlan) {
		t.Fatalf("Stage() error = %v, want stale plan", err)
	}
}

func TestPlanValidatorStage_RejectsProviderModelDisplayName(t *testing.T) {
	configmeta.Clear()
	configmeta.RegisterFirstSliceFields()

	cfg := config.Default()
	plan := &SettingsChangePlan{
		Changes: []SettingsPlanChange{
			{
				ConfigPath: "provider.model",
				Section:    "provider",
				Field:      "provider_model",
				Operation:  "set",
				NewValue:   RedactedValue{Value: "deepseek v4 pro"},
			},
		},
	}

	_, err := (PlanValidator{}).Stage(cfg, plan, ValidationOptions{})
	if !errors.Is(err, ErrPlanValidation) {
		t.Fatalf("Stage() error = %v, want plan validation error", err)
	}
	if len(plan.ValidationResults) == 0 || !strings.Contains(plan.ValidationResults[len(plan.ValidationResults)-1].Message, "exact provider model ID") {
		t.Fatalf("expected exact model ID validation message, got %#v", plan.ValidationResults)
	}
}

func TestPlanValidatorStage_AppliesGuardedToolsToggle(t *testing.T) {
	configmeta.Clear()
	configmeta.RegisterFirstSliceFields()

	cfg := config.Default()
	cfg.Hardening.GuardedTools = false
	plan := &SettingsChangePlan{
		ID: "scp_guarded",
		Changes: []SettingsPlanChange{
			{
				ConfigPath: "hardening.guardedTools",
				Operation:  "set",
				NewValue:   RedactedValue{Value: true},
			},
		},
	}

	state, err := (PlanValidator{}).Stage(cfg, plan, ValidationOptions{ApprovedAuthority: configmeta.RiskWarning})
	if err != nil {
		t.Fatalf("Stage() error = %v", err)
	}
	if !state.StagedConfig.Hardening.GuardedTools {
		t.Fatalf("expected guarded tools enabled, got false")
	}
	if plan.Changes[0].Field != "hardening_guarded_tools" {
		t.Fatalf("normalized field = %q, want hardening_guarded_tools", plan.Changes[0].Field)
	}
}

func TestPlanValidatorStage_AppliesRestrictToWorkspaceToggle(t *testing.T) {
	configmeta.Clear()
	configmeta.RegisterFirstSliceFields()

	cfg := config.Default()
	cfg.Tools.RestrictToWorkspace = false
	plan := &SettingsChangePlan{
		ID: "scp_workspace",
		Changes: []SettingsPlanChange{
			{
				ConfigPath: "tools.restrictToWorkspace",
				Operation:  "toggle",
				NewValue:   RedactedValue{Value: true},
			},
		},
	}

	state, err := (PlanValidator{}).Stage(cfg, plan, ValidationOptions{ApprovedAuthority: configmeta.RiskWarning})
	if err != nil {
		t.Fatalf("Stage() error = %v", err)
	}
	if !state.StagedConfig.Tools.RestrictToWorkspace {
		t.Fatalf("expected workspace restriction enabled, got false")
	}
	if plan.Changes[0].Field != "workspace_restrict" {
		t.Fatalf("normalized field = %q, want workspace_restrict", plan.Changes[0].Field)
	}
}

func TestPlanValidatorStage_NormalizesProviderModelFieldAlias(t *testing.T) {
	configmeta.Clear()
	configmeta.RegisterFirstSliceFields()

	cfg := config.Default()
	plan := &SettingsChangePlan{
		ID: "scp_alias",
		Changes: []SettingsPlanChange{
			{
				ConfigPath: "provider.model",
				Section:    "provider",
				Field:      "model",
				Operation:  "set",
				NewValue:   RedactedValue{Value: "deepseek/deepseek-v4-flash"},
			},
		},
	}

	state, err := (PlanValidator{}).Stage(cfg, plan, ValidationOptions{ApprovedAuthority: configmeta.RiskWarning})
	if err != nil {
		t.Fatalf("Stage() error = %v", err)
	}
	if plan.Changes[0].Field != "provider_model" {
		t.Fatalf("normalized field = %q, want provider_model", plan.Changes[0].Field)
	}
	if state.StagedConfig.Provider.Model != "deepseek/deepseek-v4-flash" {
		t.Fatalf("staged model = %q", state.StagedConfig.Provider.Model)
	}
}

func TestNormalizePlanChange_ConfigPathOnly(t *testing.T) {
	configmeta.Clear()
	configmeta.RegisterFirstSliceFields()

	change := SettingsPlanChange{
		ConfigPath: "provider.model",
		Operation:  "set",
		NewValue:   RedactedValue{Value: "gpt-4.1-mini"},
	}
	NormalizePlanChange(&change)
	if change.Section != "provider" || change.Field != "provider_model" {
		t.Fatalf("normalized change = %#v", change)
	}
}

func TestNormalizePlanChange_SkillEntryAPIKey(t *testing.T) {
	configmeta.Clear()
	configmeta.RegisterFirstSliceFields()

	change := SettingsPlanChange{
		ConfigPath: "skills.entries.demo.apiKey",
		Section:    "skills_entry",
		Channel:    "demo",
		Field:      "api_key",
		Operation:  "set",
		NewValue:   RedactedValue{Value: "new-secret"},
	}
	NormalizePlanChange(&change)
	if change.ConfigPath != "skills.entries.demo.apiKey" {
		t.Fatalf("config path = %q, want concrete demo path", change.ConfigPath)
	}
	if change.Field != "api_key" {
		t.Fatalf("field = %q, want api_key", change.Field)
	}
	if change.Channel != "demo" {
		t.Fatalf("channel = %q, want demo", change.Channel)
	}
}

func TestPlanValidatorStage_MutationFailure(t *testing.T) {
	configmeta.Clear()
	configmeta.RegisterFirstSliceFields()

	cfg := config.Default()
	plan := &SettingsChangePlan{
		Changes: []SettingsPlanChange{
			{
				ConfigPath: "runtime.historyMaxMessages",
				Section:    "runtime",
				Field:      "runtime_history_max",
				Operation:  "set",
				OldValue:   RedactedValue{Value: cfg.HistoryMax},
				NewValue:   RedactedValue{Value: "not-an-int"},
			},
		},
	}

	_, err := (PlanValidator{}).Stage(cfg, plan, ValidationOptions{})
	if !errors.Is(err, ErrPlanValidation) {
		t.Fatalf("Stage() error = %v, want plan validation error", err)
	}
}

func TestPlanValidatorStage_ConfigValidationFailure(t *testing.T) {
	configmeta.Clear()
	configmeta.RegisterFirstSliceFields()

	cfg := config.Default()
	plan := &SettingsChangePlan{
		Changes: []SettingsPlanChange{
			{
				ConfigPath: "agentCLI.defaultMode",
				Section:    "agentCLI",
				Field:      "agentCLI_default_mode",
				Operation:  "choose",
				OldValue:   RedactedValue{Value: cfg.AgentCLI.DefaultMode, Present: cfg.AgentCLI.DefaultMode != ""},
				NewValue:   RedactedValue{Value: "sandbox_auto"},
			},
		},
	}

	_, err := (PlanValidator{}).Stage(cfg, plan, ValidationOptions{})
	if !errors.Is(err, ErrPlanValidation) {
		t.Fatalf("Stage() error = %v, want plan validation error", err)
	}
}

func TestPlanValidatorStage_RiskAuthorityFailure(t *testing.T) {
	configmeta.Clear()
	configmeta.RegisterFirstSliceFields()

	cfg := config.Default()
	plan := &SettingsChangePlan{
		Changes: []SettingsPlanChange{
			{
				ConfigPath: "tools.enableExec",
				Section:    "tools",
				Field:      "tools_enable_exec",
				Operation:  "toggle",
				OldValue:   RedactedValue{Value: cfg.Tools.EnableExec},
				NewValue:   RedactedValue{Value: true},
			},
		},
	}

	_, err := (PlanValidator{}).Stage(cfg, plan, ValidationOptions{ApprovedAuthority: configmeta.RiskNotice})
	if !errors.Is(err, ErrPlanRiskExceeded) {
		t.Fatalf("Stage() error = %v, want risk exceeded", err)
	}
}

func TestPlanValidatorStage_RedactedOldValuePresenceMismatch(t *testing.T) {
	configmeta.Clear()
	configmeta.RegisterFirstSliceFields()

	cfg := config.Default()
	cfg.Skills.Entries = map[string]config.SkillEntryConfig{
		"demo": {},
	}
	plan := &SettingsChangePlan{
		Changes: []SettingsPlanChange{{
			ConfigPath: "skills.entries.demo.apiKey",
			Section:    "skills_entry",
			Channel:    "demo",
			Field:      "api_key",
			Operation:  "set",
			OldValue:   RedactedValue{Redacted: true, Present: true, Summary: "configured"},
			NewValue:   RedactedValue{Value: "clear"},
		}},
	}

	_, err := (PlanValidator{}).Stage(cfg, plan, ValidationOptions{ApprovedAuthority: configmeta.RiskWarning})
	if !errors.Is(err, ErrStalePlan) {
		t.Fatalf("Stage() error = %v, want stale plan", err)
	}
}

func TestPlanValidatorStage_RestartRequiredChange(t *testing.T) {
	configmeta.Clear()
	configmeta.RegisterFirstSliceFields()

	cfg := config.Default()
	plan := &SettingsChangePlan{
		Changes: []SettingsPlanChange{{
			ConfigPath: "skills.load.disableGlobalDir",
			Section:    "skills",
			Field:      "skills_global_disabled",
			Operation:  "toggle",
			OldValue:   RedactedValue{Value: cfg.Skills.Load.DisableGlobalDir},
			NewValue:   RedactedValue{Value: !cfg.Skills.Load.DisableGlobalDir},
		}},
	}

	state, err := (PlanValidator{}).Stage(cfg, plan, ValidationOptions{ApprovedAuthority: configmeta.RiskNotice})
	if err != nil {
		t.Fatalf("Stage() error = %v", err)
	}
	if !plan.RestartRequired || !state.RiskDecision.RequiresRestart {
		t.Fatalf("expected restart-required plan, got plan=%#v decision=%#v", plan, state.RiskDecision)
	}
}

func TestPlanValidatorStage_SkillEntryConfigChange(t *testing.T) {
	configmeta.Clear()
	configmeta.RegisterFirstSliceFields()

	cfg := config.Default()
	cfg.Skills.Entries = map[string]config.SkillEntryConfig{
		"demo": {
			Config: map[string]any{"managed_reference": "managed://cred-1"},
		},
	}
	plan := &SettingsChangePlan{
		Changes: []SettingsPlanChange{
			{
				ConfigPath: "skills.entries.demo.config.managed_reference",
				Section:    "skills_entry",
				Channel:    "demo",
				Field:      "config.managed_reference",
				Operation:  "set",
				OldValue:   RedactedValue{Value: "managed://cred-1"},
				NewValue:   RedactedValue{Value: "clear"},
			},
		},
	}

	state, err := (PlanValidator{}).Stage(cfg, plan, ValidationOptions{ApprovedAuthority: configmeta.RiskWarning})
	if err != nil {
		t.Fatalf("Stage() error = %v", err)
	}
	if _, ok := state.StagedConfig.Skills.Entries["demo"].Config["managed_reference"]; ok {
		t.Fatalf("expected managed_reference to be cleared, got %#v", state.StagedConfig.Skills.Entries["demo"].Config)
	}
}

func TestPlanLiveReloadKeys(t *testing.T) {
	plan := SettingsChangePlan{
		Changes: []SettingsPlanChange{
			{ConfigPath: "modelRouting.chat.primary.model", Field: "routing_chat_model"},
		},
	}
	keys := planLiveReloadKeys(plan)
	if len(keys) != 1 || keys[0] != "model_routing" {
		t.Fatalf("live reload keys = %#v", keys)
	}
}
