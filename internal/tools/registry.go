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

const (
	ToolGroupRead     = "read"
	ToolGroupMemory   = "memory"
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

func (r *Registry) Metadata(name string) ToolMetadata {
	if r == nil || r.tools == nil {
		return ToolMetadata{}
	}
	t := r.tools[name]
	if t == nil {
		return ToolMetadata{}
	}
	if reporter, ok := t.(MetadataReporter); ok {
		meta := reporter.Metadata()
		meta.Groups = normalizeGroups(meta.Groups)
		return meta
	}
	return inferToolMetadata(name)
}

func (r *Registry) CloneSelected(allowedNames map[string]struct{}) *Registry {
	clone := NewRegistry()
	if r == nil {
		return clone
	}
	for _, name := range r.Names() {
		if _, ok := allowedNames[name]; ok {
			clone.Register(r.tools[name])
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

func inferToolMetadata(name string) ToolMetadata {
	name = strings.ToLower(strings.TrimSpace(name))
	var groups []string
	switch {
	case name == "read_file" || name == "read_artifact" || name == "list_files" || name == "grep_files" || name == "search_memory" || name == "show_diff":
		groups = []string{ToolGroupRead}
	case strings.HasPrefix(name, "memory_"):
		groups = []string{ToolGroupMemory, ToolGroupRead}
	case name == "write_file" || name == "edit_file":
		groups = []string{ToolGroupWrite}
	case name == "exec" || name == "run_skill_script":
		groups = []string{ToolGroupExec}
	case strings.HasPrefix(name, "web_"):
		groups = []string{ToolGroupWeb}
	case strings.HasPrefix(name, "cron"):
		groups = []string{ToolGroupCron}
	case strings.Contains(name, "skill"):
		groups = []string{ToolGroupSkills, ToolGroupRead}
	case name == "send_message":
		groups = []string{ToolGroupChannels}
	case strings.HasPrefix(name, "mcp_"):
		groups = []string{ToolGroupMCP}
	case strings.Contains(name, "service"):
		groups = []string{ToolGroupService}
	}
	return ToolMetadata{Groups: normalizeGroups(groups), Capabilities: []string{string(ToolCapability(nil, nil))}}
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
