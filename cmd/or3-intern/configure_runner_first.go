package main

import (
	"or3-intern/internal/config"
	"or3-intern/internal/configmeta"
)

func filterConfigureFields(cfg config.Config, fields []configureField) []configureField {
	if !cfg.RunnerFirst() {
		return fields
	}
	out := make([]configureField, 0, len(fields))
	for _, field := range fields {
		status := configmeta.StatusForConfigureKey(cfg, field.Key)
		if status == configmeta.FieldStatusHidden {
			continue
		}
		field.Status = string(status)
		field.Label, field.Description = configmeta.ApplyConfigureFieldCopy(
			cfg,
			field.Key,
			field.Label,
			field.Description,
		)
		out = append(out, field)
	}
	return out
}
