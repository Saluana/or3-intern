package adminflow

import (
	"fmt"
	"strings"

	"or3-intern/internal/config"
	"or3-intern/internal/configedit"
)

func applyPlanChange(cfg *config.Config, change SettingsPlanChange) (bool, error) {
	op := strings.ToLower(strings.TrimSpace(change.Operation))
	switch op {
	case "", "set":
		changed, err := configedit.ApplyFieldValue(cfg, change.Section, change.Channel, change.Field, stringifyPlanValue(change.NewValue.Value))
		if err != nil {
			return false, err
		}
		return changed, nil
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

func validatePlanChangeValue(change SettingsPlanChange) error {
	if !isProviderModelChange(change) {
		return nil
	}
	model := strings.TrimSpace(stringifyPlanValue(change.NewValue.Value))
	if model == "" {
		return fmt.Errorf("provider model must not be empty")
	}
	if strings.ContainsAny(model, " \t\n\r") {
		return fmt.Errorf("provider model %q looks like a display name; use the exact provider model ID, for example openrouter/deepseek/deepseek-chat-v3-0324", model)
	}
	return nil
}

func isProviderModelChange(change SettingsPlanChange) bool {
	if change.ConfigPath == "provider.model" {
		return true
	}
	if change.Section != "provider" {
		return false
	}
	switch change.Field {
	case "provider_model", "model":
		return true
	default:
		return false
	}
}

func changeDisplayPath(change SettingsPlanChange) string {
	if strings.TrimSpace(change.ConfigPath) != "" {
		return strings.TrimSpace(change.ConfigPath)
	}
	section := strings.TrimSpace(change.Section)
	field := strings.TrimSpace(change.Field)
	if section != "" && field != "" {
		return section + "." + field
	}
	if field != "" {
		return field
	}
	if section != "" {
		return section
	}
	return "unknown setting"
}

func validateExpectedOldValue(current config.Config, change SettingsPlanChange) error {
	if strings.TrimSpace(change.ConfigPath) == "" {
		return nil
	}
	if change.OldValue.Value == nil && !change.OldValue.Redacted && !change.OldValue.Present && strings.TrimSpace(change.OldValue.Summary) == "" {
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

func checkName(prefix string, change SettingsPlanChange) string {
	if strings.TrimSpace(change.ConfigPath) != "" {
		return prefix + change.ConfigPath
	}
	if strings.TrimSpace(change.Channel) != "" {
		return prefix + change.Section + "." + change.Channel + "." + change.Field
	}
	return prefix + change.Section + "." + change.Field
}

func staleCheckName(change SettingsPlanChange) string {
	return checkName("stale.", change)
}

func applyCheckName(change SettingsPlanChange) string {
	return checkName("apply.", change)
}
