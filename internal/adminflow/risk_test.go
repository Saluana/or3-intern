package adminflow

import (
	"testing"

	"or3-intern/internal/configmeta"
)

func TestClassifyRisk(t *testing.T) {
	tests := []struct {
		name     string
		plan     *SettingsChangePlan
		expected RiskDecision
	}{
		{
			name: "nil plan",
			plan: nil,
			expected: RiskDecision{
				Level:  configmeta.RiskSafe,
				Reason: "no changes",
			},
		},
		{
			name: "empty changes",
			plan: &SettingsChangePlan{
				Changes: []SettingsPlanChange{},
			},
			expected: RiskDecision{
				Level:  configmeta.RiskSafe,
				Reason: "no changes",
			},
		},
		{
			name: "safe change - model selection",
			plan: &SettingsChangePlan{
				Changes: []SettingsPlanChange{
					{
						ConfigPath:   "provider.model",
						Field:        "model",
						MetadataRisk: configmeta.RiskSafe,
						NewValue:     RedactedValue{Value: "gpt-4"},
					},
				},
			},
			expected: RiskDecision{
				Level:            configmeta.RiskSafe,
				RequiresApproval: false,
				RequiresStepUp:   false,
				Reason:           "safe change can be applied automatically",
			},
		},
		{
			name: "notice change - restart required",
			plan: &SettingsChangePlan{
				RestartRequired: true,
				Changes: []SettingsPlanChange{
					{
						ConfigPath:   "service.enabled",
						Field:        "enabled",
						MetadataRisk: configmeta.RiskNotice,
						NewValue:     RedactedValue{Value: true},
					},
				},
			},
			expected: RiskDecision{
				Level:             configmeta.RiskDanger,
				RequiresApproval:  true,
				RequiresStepUp:    true,
				RequiresRestart:   true,
				Reason:            "danger-level change requires admin approval with passkey or PIN",
				EscalationReasons: []string{"restart required", "shell, network, or service exposure"},
			},
		},
		{
			name: "warning change - skill auth",
			plan: &SettingsChangePlan{
				Changes: []SettingsPlanChange{
					{
						ConfigPath:   "skills.github.api_key",
						Field:        "api_key",
						MetadataRisk: configmeta.RiskWarning,
						NewValue:     RedactedValue{Value: "ghp_xxx", Redacted: true},
					},
				},
			},
			expected: RiskDecision{
				Level:             configmeta.RiskWarning,
				RequiresApproval:  true,
				RequiresStepUp:    true,
				Reason:            "warning-level change requires explicit consent and identity verification",
				EscalationReasons: []string{"skill authentication change"},
			},
		},
		{
			name: "warning change - tool permissions",
			plan: &SettingsChangePlan{
				Changes: []SettingsPlanChange{
					{
						ConfigPath:   "tools.enableExec",
						Field:        "enableExec",
						MetadataRisk: configmeta.RiskWarning,
						NewValue:     RedactedValue{Value: true},
					},
				},
			},
			expected: RiskDecision{
				Level:             configmeta.RiskDanger,
				RequiresApproval:  true,
				RequiresStepUp:    true,
				Reason:            "danger-level change requires admin approval with passkey or PIN",
				EscalationReasons: []string{"tool permission change", "shell, network, or service exposure"},
			},
		},
		{
			name: "danger change - shell exposure",
			plan: &SettingsChangePlan{
				Changes: []SettingsPlanChange{
					{
						ConfigPath:   "tools.enableExec",
						Field:        "enableExec",
						MetadataRisk: configmeta.RiskDanger,
						NewValue:     RedactedValue{Value: true},
					},
				},
			},
			expected: RiskDecision{
				Level:             configmeta.RiskDanger,
				RequiresApproval:  true,
				RequiresStepUp:    true,
				Reason:            "danger-level change requires admin approval with passkey or PIN",
				EscalationReasons: []string{"tool permission change", "shell, network, or service exposure"},
			},
		},
		{
			name: "danger change - approval posture",
			plan: &SettingsChangePlan{
				Changes: []SettingsPlanChange{
					{
						ConfigPath:   "security.approvals.enabled",
						Field:        "enabled",
						MetadataRisk: configmeta.RiskWarning,
						NewValue:     RedactedValue{Value: false},
					},
				},
			},
			expected: RiskDecision{
				Level:             configmeta.RiskDanger,
				RequiresApproval:  true,
				RequiresStepUp:    true,
				Reason:            "danger-level change requires admin approval with passkey or PIN",
				EscalationReasons: []string{"approval posture change"},
			},
		},
		{
			name: "escalation - restart bumps safe to notice",
			plan: &SettingsChangePlan{
				RestartRequired: true,
				Changes: []SettingsPlanChange{
					{
						ConfigPath:   "provider.model",
						Field:        "model",
						MetadataRisk: configmeta.RiskSafe,
						NewValue:     RedactedValue{Value: "gpt-4"},
					},
				},
			},
			expected: RiskDecision{
				Level:             configmeta.RiskNotice,
				RequiresApproval:  false,
				RequiresStepUp:    false,
				RequiresRestart:   true,
				Reason:            "notice-level change will be applied with confirmation",
				EscalationReasons: []string{"restart required"},
			},
		},
		{
			name: "multiple changes - highest risk wins",
			plan: &SettingsChangePlan{
				Changes: []SettingsPlanChange{
					{
						ConfigPath:   "provider.model",
						Field:        "model",
						MetadataRisk: configmeta.RiskSafe,
						NewValue:     RedactedValue{Value: "gpt-4"},
					},
					{
						ConfigPath:   "provider.apiKey",
						Field:        "apiKey",
						MetadataRisk: configmeta.RiskWarning,
						NewValue:     RedactedValue{Value: "sk-xxx", Redacted: true},
					},
				},
			},
			expected: RiskDecision{
				Level:            configmeta.RiskWarning,
				RequiresApproval: true,
				RequiresStepUp:   true,
				Reason:           "warning-level change requires explicit consent and identity verification",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ClassifyRisk(tt.plan)

			if result.Level != tt.expected.Level {
				t.Errorf("Level = %v, want %v", result.Level, tt.expected.Level)
			}
			if result.RequiresApproval != tt.expected.RequiresApproval {
				t.Errorf("RequiresApproval = %v, want %v", result.RequiresApproval, tt.expected.RequiresApproval)
			}
			if result.RequiresStepUp != tt.expected.RequiresStepUp {
				t.Errorf("RequiresStepUp = %v, want %v", result.RequiresStepUp, tt.expected.RequiresStepUp)
			}
			if result.RequiresRestart != tt.expected.RequiresRestart {
				t.Errorf("RequiresRestart = %v, want %v", result.RequiresRestart, tt.expected.RequiresRestart)
			}
			if tt.expected.Reason != "" && result.Reason != tt.expected.Reason {
				t.Errorf("Reason = %v, want %v", result.Reason, tt.expected.Reason)
			}
			if len(tt.expected.EscalationReasons) > 0 {
				if len(result.EscalationReasons) != len(tt.expected.EscalationReasons) {
					t.Errorf("EscalationReasons length = %v, want %v", len(result.EscalationReasons), len(tt.expected.EscalationReasons))
				} else {
					for i := range tt.expected.EscalationReasons {
						if result.EscalationReasons[i] != tt.expected.EscalationReasons[i] {
							t.Errorf("EscalationReasons[%d] = %v, want %v", i, result.EscalationReasons[i], tt.expected.EscalationReasons[i])
						}
					}
				}
			}
		})
	}
}

func TestIsTruthyValue(t *testing.T) {
	tests := []struct {
		name     string
		value    any
		expected bool
	}{
		{"nil", nil, false},
		{"bool true", true, true},
		{"bool false", false, false},
		{"string true", "true", true},
		{"string TRUE", "TRUE", true},
		{"string on", "on", true},
		{"string yes", "yes", true},
		{"string 1", "1", true},
		{"string false", "false", false},
		{"string off", "off", false},
		{"string no", "no", false},
		{"string 0", "0", false},
		{"int 1", 1, true},
		{"int 0", 0, false},
		{"int64 1", int64(1), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isTruthyValue(tt.value)
			if result != tt.expected {
				t.Errorf("isTruthyValue(%v) = %v, want %v", tt.value, result, tt.expected)
			}
		})
	}
}
