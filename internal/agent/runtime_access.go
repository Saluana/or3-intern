package agent

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"

	"or3-intern/internal/bus"
	"or3-intern/internal/config"
	"or3-intern/internal/providers"
	"or3-intern/internal/security"
	"or3-intern/internal/skills"
	"or3-intern/internal/tools"
)

type trustedToolAccessContextKey struct{}

var toolIntentPatterns = map[string]*regexp.Regexp{
	"capabilities": regexp.MustCompile(`\b(tool|tools|capability|capabilities|what can you do)\b`),
	"write":        regexp.MustCompile(`\b(write|edit|modify|patch|delete|remove|trash|unlink)\b|\bcreate\s+file\b`),
	"exec":         regexp.MustCompile(`\b(run|exec|execute|command|shell|test|build)\b`),
	"web":          regexp.MustCompile(`\b(http|https|web|url|search|internet)\b`),
	"cron":         regexp.MustCompile(`\b(cron|schedule|remind)\b`),
	"skills":       regexp.MustCompile(`\b(skill|skills)\b`),
}

func (r *Runtime) effectiveTools(ctx context.Context, fallback *tools.Registry) *tools.Registry {
	if reg := toolRegistryFromContext(ctx); reg != nil {
		return reg
	}
	if fallback == nil {
		return tools.NewRegistry()
	}
	return fallback
}

func (r *Runtime) exposedToolsForTurn(ctx context.Context, reg *tools.Registry, messages []providers.ChatMessage, channel string) *tools.Registry {
	if reg == nil {
		return reg
	}
	filtered := r.filterToolsForContext(ctx, reg)
	if r == nil || !r.DynamicToolExposure {
		return filtered
	}
	intent := latestUserText(messages) + " " + strings.TrimSpace(channel)
	groups := selectedToolGroups(intent)
	allowed := map[string]struct{}{}
	for _, name := range filtered.Names() {
		meta := filtered.Metadata(name)
		if meta.Hidden || hasGroup(meta.Groups, tools.ToolGroupHidden) {
			continue
		}
		if hasAnyGroup(meta.Groups, groups) {
			allowed[name] = struct{}{}
		}
	}
	return filtered.CloneSelected(allowed)
}

func (r *Runtime) filterToolsForContext(ctx context.Context, reg *tools.Registry) *tools.Registry {
	if reg == nil {
		return reg
	}
	if r != nil {
		allowlist := map[string]struct{}{}
		for _, name := range r.Hardening.MetadataScanner.Allowlist {
			name = strings.TrimSpace(name)
			if name != "" {
				allowlist[name] = struct{}{}
			}
		}
		var diagnostics []tools.MetadataDiagnostic
		reg, diagnostics = tools.FilterSuspiciousExternalTools(reg, r.Hardening.MetadataScanner.Mode, allowlist)
		for _, diagnostic := range diagnostics {
			log.Printf("tool metadata scanner: %s", diagnostic.String())
		}
	}
	ceiling := tools.CapabilityCeilingFromContext(ctx)
	profile := tools.ActiveProfileFromContext(ctx)
	if strings.TrimSpace(profile.Name) != "" {
		if ceiling == "" || capabilityRank(profile.MaxCapability) < capabilityRank(ceiling) {
			ceiling = profile.MaxCapability
		}
	}
	allowed := map[string]struct{}{}
	profileAllowed := profile.AllowedTools
	for _, name := range reg.Names() {
		if len(profileAllowed) > 0 {
			if _, ok := profileAllowed[name]; !ok {
				continue
			}
		}
		if ceiling != "" && capabilityRank(tools.ToolCapability(reg.Get(name), nil)) > capabilityRank(ceiling) {
			continue
		}
		allowed[name] = struct{}{}
	}
	return reg.CloneSelected(allowed)
}

func latestUserText(messages []providers.ChatMessage) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "user" {
			return contentToString(messages[i].Content)
		}
	}
	return ""
}

func selectedToolGroups(intent string) map[string]struct{} {
	lower := strings.ToLower(intent)
	groups := map[string]struct{}{
		tools.ToolGroupRead:   {},
		tools.ToolGroupMemory: {},
		tools.ToolGroupPlan:   {},
	}
	if toolIntentPatterns["capabilities"].MatchString(lower) {
		groups[tools.ToolGroupWrite] = struct{}{}
		groups[tools.ToolGroupExec] = struct{}{}
		groups[tools.ToolGroupWeb] = struct{}{}
		groups[tools.ToolGroupCron] = struct{}{}
		groups[tools.ToolGroupSkills] = struct{}{}
		groups[tools.ToolGroupChannels] = struct{}{}
		groups[tools.ToolGroupMCP] = struct{}{}
		groups[tools.ToolGroupService] = struct{}{}
	}
	if toolIntentPatterns["write"].MatchString(lower) {
		groups[tools.ToolGroupWrite] = struct{}{}
	}
	if toolIntentPatterns["exec"].MatchString(lower) {
		groups[tools.ToolGroupExec] = struct{}{}
	}
	if toolIntentPatterns["web"].MatchString(lower) {
		groups[tools.ToolGroupWeb] = struct{}{}
	}
	if toolIntentPatterns["cron"].MatchString(lower) {
		groups[tools.ToolGroupCron] = struct{}{}
	}
	if toolIntentPatterns["skills"].MatchString(lower) {
		groups[tools.ToolGroupSkills] = struct{}{}
	}
	if strings.TrimSpace(intent) != "" {
		groups[tools.ToolGroupChannels] = struct{}{}
	}
	return groups
}

func hasAnyGroup(groups []string, allowed map[string]struct{}) bool {
	for _, group := range groups {
		if _, ok := allowed[group]; ok {
			return true
		}
	}
	return false
}

func hasGroup(groups []string, want string) bool {
	for _, group := range groups {
		if group == want {
			return true
		}
	}
	return false
}

func (r *Runtime) guardToolExecution(ctx context.Context, tool tools.Tool, capability tools.CapabilityLevel, params map[string]any) error {
	if tool == nil {
		return nil
	}
	sessionKey := tools.SessionFromContext(ctx)
	if err := r.enforcePlanBeforeTool(ctx, tool, sessionKey); err != nil {
		return err
	}
	profile := tools.ActiveProfileFromContext(ctx)
	if tool.Name() == tools.ToolNameSendMessage && trustedToolAccessFromContext(ctx) {
		capability = tools.CapabilitySafe
	}
	if ceiling := tools.CapabilityCeilingFromContext(ctx); ceiling != "" && capabilityRank(capability) > capabilityRank(ceiling) {
		return fmt.Errorf("tool exceeds request capability ceiling: %s", tool.Name())
	}
	if err := r.enforceProfile(ctx, profile, tool, capability, params); err != nil {
		return err
	}
	if err := r.enforceSkillPolicy(ctx, tool, params); err != nil {
		return err
	}
	if capability == tools.CapabilityGuarded && !r.Hardening.GuardedTools {
		return fmt.Errorf("tool requires guarded access: %s", tool.Name())
	}
	if capability == tools.CapabilityPrivileged && !r.Hardening.PrivilegedTools {
		return fmt.Errorf("tool requires privileged access: %s", tool.Name())
	}
	if r.ApprovalBroker != nil && tools.IsExecutionToolName(tool.Name()) {
		if mode := r.approvalModeForTool(tool.Name()); mode == config.ApprovalModeAsk || mode == config.ApprovalModeAllowlist || mode == config.ApprovalModeDeny {
			if len(r.ApprovalBroker.SignKey) == 0 {
				return fmt.Errorf("approval broker unavailable for %s", tool.Name())
			}
		}
	}
	if r.Audit != nil && (capability == tools.CapabilityPrivileged || tool.Name() == tools.ToolNameSpawnSubagent) {
		if err := r.Audit.Record(ctx, "tool.execute", tools.SessionFromContext(ctx), profileActor(profile), map[string]any{
			"tool":       tool.Name(),
			"capability": capability,
			"profile":    profile.Name,
			"summary":    summarizeToolParams(tool.Name(), params),
		}); err != nil {
			return err
		}
	}
	if !r.Hardening.Quotas.Enabled {
		return nil
	}
	return r.incrementQuota(ctx, tools.SessionFromContext(ctx), tool.Name())
}

func (r *Runtime) GuardToolExecution(ctx context.Context, tool tools.Tool, capability tools.CapabilityLevel, params map[string]any) error {
	return r.guardToolExecution(ctx, tool, capability, params)
}

func (r *Runtime) approvalModeForTool(toolName string) config.ApprovalMode {
	if r == nil || r.ApprovalBroker == nil {
		return config.ApprovalModeTrusted
	}
	switch toolName {
	case tools.ToolNameExec:
		return r.ApprovalBroker.Config.Exec.Mode
	case tools.ToolNameRunSkill, tools.ToolNameRunSkillScript:
		return r.ApprovalBroker.Config.SkillExecution.Mode
	default:
		return config.ApprovalModeTrusted
	}
}

func (r *Runtime) enforceSkillPolicy(ctx context.Context, tool tools.Tool, params map[string]any) error {
	policy := tools.SkillPolicyFromContext(ctx)
	if tool == nil || strings.TrimSpace(policy.Name) == "" {
		return nil
	}
	if len(policy.AllowedTools) > 0 {
		if _, ok := policy.AllowedTools[tool.Name()]; !ok {
			return fmt.Errorf("tool denied by skill policy: %s", tool.Name())
		}
	}
	switch tool.Name() {
	case tools.ToolNameExec, tools.ToolNameRunSkill, tools.ToolNameRunSkillScript:
		if !policy.AllowExecution {
			return fmt.Errorf("execution denied by skill policy: %s", tool.Name())
		}
		if cwd := strings.TrimSpace(fmt.Sprint(params["cwd"])); cwd != "" && cwd != "<nil>" && len(policy.WritablePaths) > 0 {
			if err := validateProfileWritablePath(policy.WritablePaths, cwd); err != nil {
				return err
			}
		}
	case tools.ToolNameWriteFile, tools.ToolNameEditFile, tools.ToolNameDeleteFile:
		if !policy.AllowWrite {
			return fmt.Errorf("write denied by skill policy: %s", tool.Name())
		}
		if len(policy.WritablePaths) > 0 {
			if err := validateProfileWritablePath(policy.WritablePaths, fmt.Sprint(params["path"])); err != nil {
				return err
			}
		}
	case tools.ToolNameWebFetch, tools.ToolNameWebFetchMarkdown:
		if !policy.AllowNetwork {
			return fmt.Errorf("network denied by skill policy: %s", tool.Name())
		}
		if len(policy.AllowedHosts) > 0 {
			parsed, err := url.Parse(strings.TrimSpace(fmt.Sprint(params["url"])))
			if err != nil {
				return err
			}
			if err := (security.HostPolicy{Enabled: true, DefaultDeny: true, AllowedHosts: policy.AllowedHosts}).ValidateURL(ctx, parsed); err != nil {
				return err
			}
		}
	case tools.ToolNameWebSearch:
		if !policy.AllowNetwork {
			return fmt.Errorf("network denied by skill policy: %s", tool.Name())
		}
		if len(policy.AllowedHosts) > 0 {
			if err := (security.HostPolicy{Enabled: true, DefaultDeny: true, AllowedHosts: policy.AllowedHosts}).ValidateHost(ctx, "api.search.brave.com"); err != nil {
				return err
			}
		}
	}
	return nil
}

func skillPolicyForSkill(skill skills.SkillMeta) tools.SkillPolicy {
	allowed := map[string]struct{}{}
	for _, toolName := range skill.AllowedTools {
		toolName = strings.TrimSpace(toolName)
		if toolName == "" {
			continue
		}
		allowed[toolName] = struct{}{}
	}
	if commandTool := strings.TrimSpace(skill.CommandTool); commandTool != "" {
		allowed[commandTool] = struct{}{}
	}
	return tools.SkillPolicy{
		Name:           skill.Name,
		AllowedTools:   allowed,
		AllowExecution: skill.Permissions.Shell || (strings.EqualFold(skill.CommandDispatch, "tool") && tools.IsExecutionToolName(strings.TrimSpace(skill.CommandTool))),
		AllowNetwork:   skill.Permissions.Network,
		AllowWrite:     skill.Permissions.Write,
		AllowedHosts:   append([]string{}, skill.Permissions.AllowedHosts...),
		WritablePaths:  append([]string{}, skill.Permissions.AllowedPaths...),
	}
}

func (r *Runtime) contextWithEventProfile(ctx context.Context, ev bus.Event) context.Context {
	if r == nil {
		return ctx
	}
	return r.contextWithProfileName(ctx, r.profileNameForEvent(ev))
}

func (r *Runtime) contextWithProfileName(ctx context.Context, name string) context.Context {
	profile, ok := r.resolveProfile(name)
	if !ok {
		return ctx
	}
	return tools.ContextWithActiveProfile(ctx, profile)
}

func (r *Runtime) ContextWithProfileName(ctx context.Context, name string) context.Context {
	return r.contextWithProfileName(ctx, name)
}

func (r *Runtime) profileNameForEvent(ev bus.Event) string {
	if len(ev.Meta) > 0 {
		if profileName := strings.TrimSpace(fmt.Sprint(ev.Meta["profile_name"])); profileName != "" && profileName != "<nil>" {
			return profileName
		}
	}
	if !r.AccessProfiles.Enabled {
		return ""
	}
	triggerKey := strings.ToLower(strings.TrimSpace(string(ev.Type)))
	if profileName := strings.TrimSpace(r.AccessProfiles.Triggers[triggerKey]); profileName != "" {
		return profileName
	}
	if profileName := strings.TrimSpace(r.AccessProfiles.Channels[strings.ToLower(strings.TrimSpace(ev.Channel))]); profileName != "" {
		return profileName
	}
	return strings.TrimSpace(r.AccessProfiles.Default)
}

func (r *Runtime) resolveProfile(name string) (tools.ActiveProfile, bool) {
	name = strings.TrimSpace(name)
	if !r.AccessProfiles.Enabled || name == "" {
		return tools.ActiveProfile{}, false
	}
	profileCfg, ok := r.AccessProfiles.Profiles[name]
	if !ok {
		return tools.ActiveProfile{}, false
	}
	profileCfg = config.ExpandAccessProfile(profileCfg, r.WorkspaceDir)
	allowed := map[string]struct{}{}
	for _, toolName := range profileCfg.AllowedTools {
		allowed[strings.TrimSpace(toolName)] = struct{}{}
	}
	maxCapability := tools.CapabilityPrivileged
	switch strings.ToLower(strings.TrimSpace(profileCfg.MaxCapability)) {
	case "safe":
		maxCapability = tools.CapabilitySafe
	case "guarded":
		maxCapability = tools.CapabilityGuarded
	case "privileged", "":
		maxCapability = tools.CapabilityPrivileged
	}
	return tools.ActiveProfile{
		Name:           name,
		MaxCapability:  maxCapability,
		AllowedTools:   allowed,
		AllowedHosts:   append([]string{}, profileCfg.AllowedHosts...),
		WritablePaths:  append([]string{}, profileCfg.WritablePaths...),
		AllowSubagents: profileCfg.AllowSubagents,
	}, true
}

func (r *Runtime) enforceProfile(ctx context.Context, profile tools.ActiveProfile, tool tools.Tool, capability tools.CapabilityLevel, params map[string]any) error {
	if strings.TrimSpace(profile.Name) == "" {
		return nil
	}
	if capabilityRank(capability) > capabilityRank(profile.MaxCapability) {
		return fmt.Errorf("tool exceeds profile capability: %s", tool.Name())
	}
	if len(profile.AllowedTools) > 0 {
		if _, ok := profile.AllowedTools[tool.Name()]; !ok {
			return fmt.Errorf("tool denied by profile: %s", tool.Name())
		}
	}
	if tool.Name() == tools.ToolNameSpawnSubagent && !profile.AllowSubagents {
		return fmt.Errorf("subagents denied by profile")
	}
	switch tool.Name() {
	case tools.ToolNameWriteFile, tools.ToolNameEditFile, tools.ToolNameDeleteFile:
		if len(profile.WritablePaths) == 0 {
			return fmt.Errorf("path denied by profile")
		}
		if err := validateProfileWritablePath(profile.WritablePaths, fmt.Sprint(params["path"])); err != nil {
			return err
		}
	case tools.ToolNameExec:
		if cwd := strings.TrimSpace(fmt.Sprint(params["cwd"])); cwd != "" && cwd != "<nil>" {
			if len(profile.WritablePaths) == 0 {
				return fmt.Errorf("path denied by profile")
			}
			if err := validateProfileWritablePath(profile.WritablePaths, cwd); err != nil {
				return err
			}
		}
	}
	switch tool.Name() {
	case tools.ToolNameWebFetch, tools.ToolNameWebFetchMarkdown:
		parsed, err := url.Parse(strings.TrimSpace(fmt.Sprint(params["url"])))
		if err != nil {
			return err
		}
		if err := (security.HostPolicy{Enabled: true, DefaultDeny: true, AllowedHosts: profile.AllowedHosts}).ValidateURL(ctx, parsed); err != nil {
			return err
		}
	case tools.ToolNameWebSearch:
		if err := (security.HostPolicy{Enabled: true, DefaultDeny: true, AllowedHosts: profile.AllowedHosts}).ValidateHost(ctx, "api.search.brave.com"); err != nil {
			return err
		}
	}
	return nil
}

func capabilityRank(level tools.CapabilityLevel) int {
	switch level {
	case tools.CapabilitySafe:
		return 0
	case tools.CapabilityGuarded:
		return 1
	case tools.CapabilityPrivileged:
		return 2
	default:
		return 2
	}
}

func validateProfileWritablePath(allowed []string, path string) error {
	path = strings.TrimSpace(path)
	if path == "" || path == "<nil>" {
		return nil
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	for _, root := range allowed {
		rootPath, rootErr := filepath.Abs(root)
		if rootErr != nil {
			continue
		}
		rel, relErr := filepath.Rel(rootPath, absPath)
		if relErr == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return nil
		}
	}
	return fmt.Errorf("path denied by profile")
}

func profileActor(profile tools.ActiveProfile) string {
	if strings.TrimSpace(profile.Name) == "" {
		return "runtime"
	}
	return "profile:" + profile.Name
}

func (r *Runtime) contextWithTrustedToolAccess(ctx context.Context, ev bus.Event) context.Context {
	if !isTrustedToolEvent(ev.Type) {
		return ctx
	}
	return context.WithValue(ctx, trustedToolAccessContextKey{}, true)
}

func trustedToolAccessFromContext(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	trusted, _ := ctx.Value(trustedToolAccessContextKey{}).(bool)
	return trusted
}

func isTrustedToolEvent(eventType bus.EventType) bool {
	switch eventType {
	case bus.EventHeartbeat, bus.EventCron:
		return true
	default:
		return false
	}
}
