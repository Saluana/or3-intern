// Package doctoradmin provides typed Doctor admin-brain actions without the generic model tool registry.
package doctoradmin

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"

	"or3-intern/internal/providers"
)

// Action is a named Doctor admin operation callable from the internal admin brain.
type Action struct {
	Name        string
	Description string
	Run         func(ctx context.Context, params map[string]any) (string, error)
}

// Registry holds Doctor admin actions for the built-in admin brain.
type Registry struct {
	mu      sync.RWMutex
	actions map[string]Action
}

func NewRegistry() *Registry {
	return &Registry{actions: map[string]Action{}}
}

func (r *Registry) Register(action Action) {
	if r == nil || action.Name == "" || action.Run == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.actions[action.Name] = action
}

func (r *Registry) Names() []string {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.actions))
	for name := range r.actions {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func (r *Registry) Has(name string) bool {
	if r == nil {
		return false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.actions[name]
	return ok
}

// ProviderToolDefs returns provider tool definitions for the allowed action names.
func (r *Registry) ProviderToolDefs(allowed []string) []providers.ToolDef {
	if r == nil {
		return nil
	}
	names := allowed
	if len(names) == 0 {
		names = r.Names()
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	defs := make([]providers.ToolDef, 0, len(names))
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		action, ok := r.actions[name]
		if !ok {
			continue
		}
		defs = append(defs, providers.ToolDef{
			Type: "function",
			Function: providers.ToolFunc{
				Name:        action.Name,
				Description: action.Description,
				Parameters:  map[string]any{"type": "object", "properties": map[string]any{}},
			},
		})
	}
	return defs
}

func (r *Registry) Execute(ctx context.Context, name string, params map[string]any) (string, error) {
	if r == nil {
		return "", fmt.Errorf("doctor admin actions unavailable")
	}
	r.mu.RLock()
	action, ok := r.actions[name]
	r.mu.RUnlock()
	if !ok {
		return "", fmt.Errorf("unknown doctor action %q", name)
	}
	if params == nil {
		params = map[string]any{}
	}
	return action.Run(ctx, params)
}
