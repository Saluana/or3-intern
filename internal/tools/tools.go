package tools

import (
	"context"
)

type CapabilityLevel string

const (
	CapabilitySafe       CapabilityLevel = "safe"
	CapabilityGuarded    CapabilityLevel = "guarded"
	CapabilityPrivileged CapabilityLevel = "privileged"
)

type Tool interface {
	Name() string
	Description() string
	Parameters() map[string]any
	Execute(ctx context.Context, params map[string]any) (string, error)
	Schema() map[string]any
}

type CapabilityReporter interface {
	Capability() CapabilityLevel
}

type CapabilityForParamsReporter interface {
	CapabilityForParams(params map[string]any) CapabilityLevel
}

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

type Base struct{}

func (Base) SchemaFor(name, desc string, params map[string]any) map[string]any {
	return map[string]any{
		"type": "function",
		"function": map[string]any{
			"name": name,
			"description": desc,
			"parameters": params,
		},
	}
}
