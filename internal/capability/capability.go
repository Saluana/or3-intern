// Package capability defines trust levels for service and admin actions.
package capability

type Level string

const (
	Safe       Level = "safe"
	Guarded    Level = "guarded"
	Privileged Level = "privileged"
)

// CapabilityLevel is retained for legacy imports during runner-first migration.
type CapabilityLevel = Level

const (
	CapabilitySafe       = Safe
	CapabilityGuarded    = Guarded
	CapabilityPrivileged = Privileged
)
