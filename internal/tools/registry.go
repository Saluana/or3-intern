package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

type Registry struct {
	tools map[string]Tool
}

func NewRegistry() *Registry {
	return &Registry{tools: map[string]Tool{}}
}

func (r *Registry) Register(t Tool)      { r.tools[t.Name()] = t }
func (r *Registry) Get(name string) Tool { return r.tools[name] }
func (r *Registry) Names() []string {
	out := make([]string, 0, len(r.tools))
	for k := range r.tools {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func (r *Registry) Definitions() []map[string]any {
	names := r.Names()
	out := make([]map[string]any, 0, len(names))
	for _, name := range names {
		out = append(out, r.tools[name].Schema())
	}
	return out
}

func (r *Registry) CloneFiltered(allowed []string) *Registry {
	if r == nil {
		return NewRegistry()
	}
	if len(allowed) == 0 {
		clone := NewRegistry()
		for _, name := range r.Names() {
			clone.Register(r.tools[name])
		}
		return clone
	}
	allowedSet := make(map[string]struct{}, len(allowed))
	for _, name := range allowed {
		trimmed := strings.TrimSpace(name)
		if trimmed == "" {
			continue
		}
		allowedSet[trimmed] = struct{}{}
	}
	clone := NewRegistry()
	for _, name := range r.Names() {
		if _, ok := allowedSet[name]; !ok {
			continue
		}
		clone.Register(r.tools[name])
	}
	return clone
}

func (r *Registry) Execute(ctx context.Context, name string, argsJSON string) (string, error) {
	t := r.tools[name]
	if t == nil {
		return "", fmt.Errorf("tool '%s' not found", name)
	}
	var params map[string]any
	if argsJSON == "" {
		params = map[string]any{}
	} else {
		if err := json.Unmarshal([]byte(argsJSON), &params); err != nil {
			return "", fmt.Errorf("invalid tool args: %w", err)
		}
	}
	return r.ExecuteParams(ctx, name, params)
}

func (r *Registry) ExecuteParams(ctx context.Context, name string, params map[string]any) (string, error) {
	t := r.tools[name]
	if t == nil {
		return "", fmt.Errorf("tool '%s' not found", name)
	}
	if params == nil {
		params = map[string]any{}
	}
	if guard := ToolGuardFromContext(ctx); guard != nil {
		if err := guard(ctx, t, ToolCapability(t, params), params); err != nil {
			return "", err
		}
	}
	return t.Execute(ctx, params)
}
