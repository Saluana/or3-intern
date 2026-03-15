// Package tools defines the shared interfaces and capability model for runtime tools.
package tools

import (
	"context"
)

// CapabilityLevel classifies the trust required to invoke a tool.
type CapabilityLevel string

const (
	// CapabilitySafe covers tools that do not require elevated approval.
	CapabilitySafe CapabilityLevel = "safe"
	// CapabilityGuarded covers tools that may need policy checks.
	CapabilityGuarded CapabilityLevel = "guarded"
	// CapabilityPrivileged covers tools reserved for elevated execution.
	CapabilityPrivileged CapabilityLevel = "privileged"
)

// Tool is the runtime interface implemented by all callable tools.
type Tool interface {
	Name() string
	Description() string
	Parameters() map[string]any
	Execute(ctx context.Context, params map[string]any) (string, error)
	Schema() map[string]any
}

// CapabilityReporter reports a static capability level for a tool.
type CapabilityReporter interface {
	Capability() CapabilityLevel
}

// CapabilityForParamsReporter reports a capability level for specific params.
type CapabilityForParamsReporter interface {
	CapabilityForParams(params map[string]any) CapabilityLevel
}

// ToolCapability returns the effective capability level for t and params.
func ToolCapability(t Tool, params map[string]any) CapabilityLevel {
	if t == nil {
		return CapabilitySafe
	}
	if dynamic, ok := t.(CapabilityForParamsReporter); ok {
		if level := dynamic.CapabilityForParams(params); level != "" {
			return level
		}
	}
	if static, ok := t.(CapabilityReporter); ok {
		if level := static.Capability(); level != "" {
			return level
		}
	}
	return CapabilitySafe
}

// Base provides common schema helpers for tool implementations.
type Base struct{}

// SchemaFor builds a function-style tool schema.
func (Base) SchemaFor(name, desc string, params map[string]any) map[string]any {
	return map[string]any{
		"type": "function",
		"function": map[string]any{
			"name":        name,
			"description": desc,
			"parameters":  params,
		},
	}
}
