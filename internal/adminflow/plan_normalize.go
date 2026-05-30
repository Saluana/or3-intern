package adminflow

import (
	"strings"

	"or3-intern/internal/configedit"
	"or3-intern/internal/configmeta"
)

// NormalizePlanChanges resolves config paths and configure field keys so agent
// plans that use JSON paths (provider.model) or short keys (model) still apply.
func NormalizePlanChanges(changes []SettingsPlanChange) {
	configmeta.EnsureFirstSliceFieldsRegistered()
	for i := range changes {
		NormalizePlanChange(&changes[i])
	}
}

// NormalizePlanChange updates a single plan change in place.
func NormalizePlanChange(change *SettingsPlanChange) {
	if change == nil {
		return
	}
	configmeta.EnsureFirstSliceFieldsRegistered()

	path := strings.TrimSpace(change.ConfigPath)
	section := strings.TrimSpace(change.Section)
	field := strings.TrimSpace(change.Field)

	meta, ok := lookupChangeMetadataFromParts(path, section, field)
	if ok {
		applyMetadataToPlanChange(change, meta)
	} else {
		applyProviderModelFallback(change, section, field)
	}

	if strings.TrimSpace(change.Operation) == "" {
		change.Operation = "set"
	}
	if planChangeHasBoolNewValue(change) &&
		(strings.TrimSpace(change.Operation) == "" ||
			strings.EqualFold(strings.TrimSpace(change.Operation), "set")) {
		change.Operation = "toggle"
	}
}

func lookupChangeMetadata(change SettingsPlanChange) (configmeta.ConfigFieldMetadata, bool) {
	return lookupChangeMetadataFromParts(
		strings.TrimSpace(change.ConfigPath),
		strings.TrimSpace(change.Section),
		strings.TrimSpace(change.Field),
	)
}

func lookupChangeMetadataFromParts(path, section, field string) (configmeta.ConfigFieldMetadata, bool) {
	if path != "" {
		if meta, ok := configmeta.GetByPath(path); ok {
			return meta, true
		}
	}
	if section != "" && field != "" {
		return configmeta.Get(section, field)
	}
	return configmeta.ConfigFieldMetadata{}, false
}

func applyMetadataToPlanChange(change *SettingsPlanChange, meta configmeta.ConfigFieldMetadata) {
	originalPath := strings.TrimSpace(change.ConfigPath)
	metaPath := strings.TrimSpace(meta.Path)
	if metaPath != "" {
		if !strings.Contains(metaPath, "*") || originalPath == "" {
			change.ConfigPath = metaPath
		}
	}
	change.Section = meta.Section
	change.Field = configedit.ConfigureFieldKeyForMetadata(meta)
	if strings.TrimSpace(change.Channel) == "" {
		change.Channel = channelFromConcreteConfigPath(originalPath, meta.Section)
	}
}

func applyProviderModelFallback(change *SettingsPlanChange, section, field string) {
	if section == "provider" && (field == "model" || field == "provider_model") {
		change.ConfigPath = "provider.model"
		change.Section = "provider"
		change.Field = "provider_model"
	}
}

func planChangeHasBoolNewValue(change *SettingsPlanChange) bool {
	if change == nil {
		return false
	}
	_, ok := boolPlanValue(change.NewValue.Value)
	return ok
}

func channelFromConcreteConfigPath(path, section string) string {
	path = strings.TrimSpace(path)
	if path == "" || strings.Contains(path, "*") {
		return ""
	}
	switch section {
	case "skills_entry":
		const prefix = "skills.entries."
		if !strings.HasPrefix(path, prefix) {
			return ""
		}
		rest := strings.TrimPrefix(path, prefix)
		if dot := strings.Index(rest, "."); dot > 0 {
			return rest[:dot]
		}
	}
	return ""
}
