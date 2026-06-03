// Package tools defines the MCP tool interface and shared result aliases.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"or3-intern/internal/capability"
)

type CapabilityLevel = capability.Level

const (
	CapabilitySafe       = capability.CapabilitySafe
	CapabilityGuarded    = capability.CapabilityGuarded
	CapabilityPrivileged = capability.CapabilityPrivileged
)

// Tool is implemented by MCP remote tools.
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

type CapabilityForContextParamsReporter interface {
	CapabilityForContextParams(ctx context.Context, params map[string]any) CapabilityLevel
}

func ToolCapability(t Tool, params map[string]any) CapabilityLevel {
	return ToolCapabilityForContext(context.Background(), t, params)
}

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

type Base struct{}

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
