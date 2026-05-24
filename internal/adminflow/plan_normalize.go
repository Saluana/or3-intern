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

	var meta configmeta.ConfigFieldMetadata
	var ok bool
	if path != "" {
		meta, ok = configmeta.GetByPath(path)
	}
	if !ok && section != "" && field != "" {
		meta, ok = configmeta.Get(section, field)
	}
	if ok {
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
	} else if section == "provider" && (field == "model" || field == "provider_model") {
		change.ConfigPath = "provider.model"
		change.Section = "provider"
		change.Field = "provider_model"
	}

	if strings.TrimSpace(change.Operation) == "" {
		change.Operation = "set"
	}
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
