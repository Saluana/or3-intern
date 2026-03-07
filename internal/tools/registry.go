package tools

import (
	"context"
	"encoding/json"
	"fmt"
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
	return out
}

func (r *Registry) Definitions() []map[string]any {
	out := make([]map[string]any, 0, len(r.tools))
	for _, t := range r.tools {
		out = append(out, t.Schema())
	}
	return out
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
	return t.Execute(ctx, params)
}
