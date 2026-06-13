package configmeta

import "or3-intern/internal/config"

// FieldStatus describes how configure/doctor/settings UIs should treat a field.
type FieldStatus string

const (
	FieldStatusActive FieldStatus = "active"
)

// StatusForConfigureKey returns the UI status for a configure TUI field key.
// Runner-only is the only supported mode, so every active field is reported
// as active. Hidden/compatibility classifications have been removed along
// with the legacy built-in agent loop.
func StatusForConfigureKey(_ config.Config, _ string) FieldStatus {
	return FieldStatusActive
}

// Annotate returns the input field list unchanged. Runner-only mode no longer
// filters or annotates fields based on a compatibility status; the legacy
// hidden/compatibility layer was removed.
func Annotate(_ config.Config, fields []ConfigFieldMetadata) []ConfigFieldMetadata {
	return fields
}

// StatusForPath resolves metadata status. With runner-only mode as the only
// supported mode, every path is reported as active.
func StatusForPath(_ config.Config, _, _, _ string) FieldStatus {
	return FieldStatusActive
}

// ListForConfig returns registered metadata unchanged. Hidden fields are no
// longer filtered out here; legacy fields were deleted from the registry.
func ListForConfig(cfg config.Config) []ConfigFieldMetadata {
	return Annotate(cfg, List())
}
