// Package tools defines the shared interfaces and capability model for runtime tools.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
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

// CapabilityForContextParamsReporter reports a context-aware capability level.
type CapabilityForContextParamsReporter interface {
	CapabilityForContextParams(ctx context.Context, params map[string]any) CapabilityLevel
}

// ToolCapability returns the effective capability level for t and params.
func ToolCapability(t Tool, params map[string]any) CapabilityLevel {
	return ToolCapabilityForContext(context.Background(), t, params)
}

// ToolCapabilityForContext returns the effective capability level for t and params
// using request context when a tool has source-specific capability behavior.
func ToolCapabilityForContext(ctx context.Context, t Tool, params map[string]any) CapabilityLevel {
	if t == nil {
		return CapabilityPrivileged
	}
	if dynamic, ok := t.(CapabilityForContextParamsReporter); ok {
		if level := dynamic.CapabilityForContextParams(ctx, params); level != "" {
			return level
		}
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

func stringParam(params map[string]any, key string) string {
	if params == nil {
		return ""
	}
	value, ok := params[key]
	if !ok || value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func floatParam(params map[string]any, key string) (float64, bool) {
	if params == nil {
		return 0, false
	}
	switch value := params[key].(type) {
	case float64:
		return value, true
	case float32:
		return float64(value), true
	case int:
		return float64(value), true
	case int64:
		return float64(value), true
	case json.Number:
		parsed, err := value.Float64()
		return parsed, err == nil
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
		return parsed, err == nil
	default:
		return 0, false
	}
}

func boolParam(params map[string]any, key string) (bool, bool) {
	if params == nil {
		return false, false
	}
	value, ok := params[key].(bool)
	return value, ok
}
