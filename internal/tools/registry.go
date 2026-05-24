package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
)

type Registry struct {
	mu    sync.RWMutex
	tools map[string]Tool
}

const (
	ToolGroupRead     = "read"
	ToolGroupMemory   = "memory"
	ToolGroupPlan     = "plan"
	ToolGroupWrite    = "write"
	ToolGroupExec     = "exec"
	ToolGroupWeb      = "web"
	ToolGroupCron     = "cron"
	ToolGroupSkills   = "skills"
	ToolGroupChannels = "channels"
	ToolGroupMCP      = "mcp"
	ToolGroupService  = "service"
	ToolGroupHidden   = "hidden"
)

type ToolMetadata struct {
	Groups       []string
	Capabilities []string
	Hidden       bool
}

type MetadataReporter interface {
	Metadata() ToolMetadata
}

func NewRegistry() *Registry {
	return &Registry{tools: map[string]Tool{}}
}

func (r *Registry) Register(t Tool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tools[t.Name()] = t
	registerToolAdviceProvider(t)
}

func (r *Registry) Get(name string) Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.tools[name]
}

func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.tools))
	for k := range r.tools {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func (r *Registry) Definitions() []map[string]any {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.tools))
	for k := range r.tools {
		names = append(names, k)
	}
	sort.Strings(names)
	out := make([]map[string]any, 0, len(names))
	for _, name := range names {
		out = append(out, r.tools[name].Schema())
	}
	return out
}

func (r *Registry) Metadata(name string) ToolMetadata {
	if r == nil {
		return ToolMetadata{}
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.tools == nil {
		return ToolMetadata{}
	}
	t := r.tools[name]
	if t == nil {
		return ToolMetadata{}
	}
	if reporter, ok := t.(MetadataReporter); ok {
		meta := reporter.Metadata()
		if len(meta.Capabilities) == 0 {
			meta.Capabilities = []string{string(ToolCapability(t, nil))}
		}
		meta.Groups = normalizeGroups(meta.Groups)
		return meta
	}
	return inferToolMetadata(t)
}

func (r *Registry) CloneSelected(allowedNames map[string]struct{}) *Registry {
	clone := NewRegistry()
	if r == nil {
		return clone
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	for name := range r.tools {
		if _, ok := allowedNames[name]; ok {
			clone.tools[name] = r.tools[name]
		}
	}
	return clone
}

func normalizeGroups(groups []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(groups))
	for _, group := range groups {
		group = strings.ToLower(strings.TrimSpace(group))
		if group == "" {
			continue
		}
		if _, ok := seen[group]; ok {
			continue
		}
		seen[group] = struct{}{}
		out = append(out, group)
	}
	sort.Strings(out)
	return out
}

func inferToolMetadata(tool Tool) ToolMetadata {
	if tool == nil {
		return ToolMetadata{}
	}
	return ToolMetadata{Capabilities: []string{string(ToolCapability(tool, nil))}}
}

func (r *Registry) CloneFiltered(allowed []string) *Registry {
	if r == nil {
		return NewRegistry()
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if len(allowed) == 0 {
		clone := NewRegistry()
		for name := range r.tools {
			clone.tools[name] = r.tools[name]
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
	for name := range r.tools {
		if _, ok := allowedSet[name]; !ok {
			continue
		}
		clone.tools[name] = r.tools[name]
	}
	return clone
}

func (r *Registry) Execute(ctx context.Context, name string, argsJSON string) (string, error) {
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
	r.mu.RLock()
	t := r.tools[name]
	r.mu.RUnlock()
	if t == nil {
		return "", fmt.Errorf("tool '%s' not found", name)
	}
	if params == nil {
		params = map[string]any{}
	}
	if guard := ToolGuardFromContext(ctx); guard != nil {
		if err := guard(ctx, t, ToolCapabilityForContext(ctx, t, params), params); err != nil {
			return "", err
		}
	}
	return t.Execute(ctx, params)
}
