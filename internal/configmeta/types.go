// Package configmeta defines the backend-owned config metadata registry for
// field labels, descriptions, paths, defaults, risk, restart requirements,
// dependencies, validation, rollback behavior, and user-intent examples.
package configmeta

// RiskLevel represents the safety classification of a config field or change.
type RiskLevel string

const (
	// RiskSafe indicates the change is safe to apply automatically.
	RiskSafe RiskLevel = "safe"
	// RiskNotice indicates the change should show a notice but can apply automatically.
	RiskNotice RiskLevel = "notice"
	// RiskWarning indicates the change requires explicit consent and identity verification.
	RiskWarning RiskLevel = "warning"
	// RiskDanger indicates the change requires admin approval with passkey or PIN.
	RiskDanger RiskLevel = "danger"
)

// RiskRank returns a numeric rank for risk comparison.
func RiskRank(level RiskLevel) int {
	switch level {
	case RiskDanger:
		return 4
	case RiskWarning:
		return 3
	case RiskNotice:
		return 2
	case RiskSafe:
		return 1
	default:
		return 0
	}
}

// HigherRisk returns the higher of two risk levels.
func HigherRisk(a, b RiskLevel) RiskLevel {
	if RiskRank(a) >= RiskRank(b) {
		return a
	}
	return b
}

// ConfigRelation describes a dependency or conflict between config fields.
type ConfigRelation struct {
	Path        string `json:"path"`
	Description string `json:"description,omitempty"`
}

// ValidationRule describes a validation constraint for a config field.
type ValidationRule struct {
	Kind        string `json:"kind"` // "required", "min", "max", "pattern", "url", "enum"
	Value       any    `json:"value,omitempty"`
	Description string `json:"description,omitempty"`
}

// RollbackBehavior describes how a field can be rolled back.
type RollbackBehavior struct {
	Safe            bool   `json:"safe"`
	ManualOnly      bool   `json:"manual_only,omitempty"`
	RestartRequired bool   `json:"restart_required,omitempty"`
	Instructions    string `json:"instructions,omitempty"`
}

// ConfigFieldMetadata describes a single config field with all metadata needed
// for UI, Doctor, validation, risk classification, and docs.
type ConfigFieldMetadata struct {
	Section          string           `json:"section"`
	Key              string           `json:"key"`
	Path             string           `json:"path"`
	Label            string           `json:"label"`
	Description      string           `json:"description"`
	DefaultValue     any              `json:"default_value,omitempty"`
	AllowedValues    []string         `json:"allowed_values,omitempty"`
	CurrentValue     any              `json:"current_value,omitempty"`
	Secret           bool             `json:"secret,omitempty"`
	Risk             RiskLevel        `json:"risk_level"`
	RestartRequired  bool             `json:"restart_required"`
	RequiresApproval bool             `json:"requires_approval"`
	RequiresStepUp   bool             `json:"requires_step_up_auth"`
	Dependencies     []ConfigRelation `json:"dependencies,omitempty"`
	Conflicts        []ConfigRelation `json:"conflicts,omitempty"`
	Validation       []ValidationRule `json:"validation_rules,omitempty"`
	Rollback         RollbackBehavior `json:"rollback_behavior"`
	UserIntents      []string         `json:"user_intents,omitempty"`
	Docs             string           `json:"docs,omitempty"`
	AdvancedOnly     bool             `json:"advanced_only,omitempty"`
}

// Registry is the interface for looking up config field metadata.
type Registry interface {
	// Get returns metadata for a config field by section and key.
	Get(section, key string) (ConfigFieldMetadata, bool)
	// GetByPath returns metadata for a config field by its full path.
	GetByPath(path string) (ConfigFieldMetadata, bool)
	// List returns all registered fields.
	List() []ConfigFieldMetadata
	// ListBySection returns all fields in a section.
	ListBySection(section string) []ConfigFieldMetadata
}
