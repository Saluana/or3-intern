package tools

import (
	"context"
	"strings"

	"or3-intern/internal/scope"
)

type sessionContextKey struct{}
type deliveryChannelContextKey struct{}
type deliveryToContextKey struct{}
type deliveryMetaContextKey struct{}
type envContextKey struct{}
type toolGuardContextKey struct{}
type activeProfileContextKey struct{}
type skillPolicyContextKey struct{}
type approvalTokenContextKey struct{}
type requesterIdentityContextKey struct{}
type requestSourceContextKey struct{}
type capabilityCeilingContextKey struct{}

type ToolGuard func(ctx context.Context, tool Tool, capability CapabilityLevel, params map[string]any) error

type ActiveProfile struct {
	Name           string
	MaxCapability  CapabilityLevel
	AllowedTools   map[string]struct{}
	AllowedHosts   []string
	WritablePaths  []string
	AllowSubagents bool
}

type SkillPolicy struct {
	Name           string
	AllowedTools   map[string]struct{}
	AllowExecution bool
	AllowNetwork   bool
	AllowWrite     bool
	AllowedHosts   []string
	WritablePaths  []string
}

type RequesterIdentity struct {
	Actor string
	Role  string
}

const (
	RequestSourceCLI     = "cli"
	RequestSourceService = "service"
)

func ContextWithSession(ctx context.Context, sessionKey string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if sessionKey == "" {
		sessionKey = scope.GlobalMemoryScope
	}
	return context.WithValue(ctx, sessionContextKey{}, sessionKey)
}

func ContextWithDelivery(ctx context.Context, channel, to string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx = context.WithValue(ctx, deliveryChannelContextKey{}, channel)
	return context.WithValue(ctx, deliveryToContextKey{}, to)
}

func ContextWithDeliveryMeta(ctx context.Context, meta map[string]any) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if len(meta) == 0 {
		return ctx
	}
	cloned := make(map[string]any, len(meta))
	for k, v := range meta {
		cloned[k] = v
	}
	return context.WithValue(ctx, deliveryMetaContextKey{}, cloned)
}

func ContextWithEnv(ctx context.Context, env map[string]string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if len(env) == 0 {
		return ctx
	}
	copyEnv := make(map[string]string, len(env))
	for k, v := range env {
		copyEnv[k] = v
	}
	return context.WithValue(ctx, envContextKey{}, copyEnv)
}

func ContextWithToolGuard(ctx context.Context, guard ToolGuard) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if guard == nil {
		return ctx
	}
	return context.WithValue(ctx, toolGuardContextKey{}, guard)
}

func ContextWithActiveProfile(ctx context.Context, profile ActiveProfile) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if strings.TrimSpace(profile.Name) == "" && len(profile.AllowedTools) == 0 && len(profile.AllowedHosts) == 0 && len(profile.WritablePaths) == 0 && !profile.AllowSubagents && profile.MaxCapability == "" {
		return ctx
	}
	cloned := ActiveProfile{
		Name:           strings.TrimSpace(profile.Name),
		MaxCapability:  profile.MaxCapability,
		AllowedHosts:   append([]string{}, profile.AllowedHosts...),
		WritablePaths:  append([]string{}, profile.WritablePaths...),
		AllowSubagents: profile.AllowSubagents,
	}
	if len(profile.AllowedTools) > 0 {
		cloned.AllowedTools = make(map[string]struct{}, len(profile.AllowedTools))
		for key := range profile.AllowedTools {
			cloned.AllowedTools[key] = struct{}{}
		}
	}
	return context.WithValue(ctx, activeProfileContextKey{}, cloned)
}

func ContextWithSkillPolicy(ctx context.Context, policy SkillPolicy) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if strings.TrimSpace(policy.Name) == "" && len(policy.AllowedTools) == 0 && !policy.AllowExecution && !policy.AllowNetwork && !policy.AllowWrite && len(policy.AllowedHosts) == 0 && len(policy.WritablePaths) == 0 {
		return ctx
	}
	cloned := SkillPolicy{
		Name:           strings.TrimSpace(policy.Name),
		AllowExecution: policy.AllowExecution,
		AllowNetwork:   policy.AllowNetwork,
		AllowWrite:     policy.AllowWrite,
		AllowedHosts:   append([]string{}, policy.AllowedHosts...),
		WritablePaths:  append([]string{}, policy.WritablePaths...),
	}
	if len(policy.AllowedTools) > 0 {
		cloned.AllowedTools = make(map[string]struct{}, len(policy.AllowedTools))
		for key := range policy.AllowedTools {
			cloned.AllowedTools[key] = struct{}{}
		}
	}
	return context.WithValue(ctx, skillPolicyContextKey{}, cloned)
}

func ContextWithApprovalToken(ctx context.Context, token string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return ctx
	}
	return context.WithValue(ctx, approvalTokenContextKey{}, token)
}

func ContextWithRequesterIdentity(ctx context.Context, actor, role string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	identity := RequesterIdentity{Actor: strings.TrimSpace(actor), Role: strings.TrimSpace(role)}
	if identity.Actor == "" && identity.Role == "" {
		return ctx
	}
	return context.WithValue(ctx, requesterIdentityContextKey{}, identity)
}

func ContextWithRequestSource(ctx context.Context, source string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	source = strings.ToLower(strings.TrimSpace(source))
	if source == "" {
		return ctx
	}
	return context.WithValue(ctx, requestSourceContextKey{}, source)
}

func ContextWithCapabilityCeiling(ctx context.Context, level CapabilityLevel) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if level == "" {
		return ctx
	}
	return context.WithValue(ctx, capabilityCeilingContextKey{}, level)
}

func SessionFromContext(ctx context.Context) string {
	if ctx == nil {
		return scope.GlobalMemoryScope
	}
	if sessionKey, ok := ctx.Value(sessionContextKey{}).(string); ok && sessionKey != "" {
		return sessionKey
	}
	return scope.GlobalMemoryScope
}

func DeliveryFromContext(ctx context.Context) (channel string, to string) {
	if ctx == nil {
		return "", ""
	}
	if v, ok := ctx.Value(deliveryChannelContextKey{}).(string); ok {
		channel = v
	}
	if v, ok := ctx.Value(deliveryToContextKey{}).(string); ok {
		to = v
	}
	return channel, to
}

func DeliveryMetaFromContext(ctx context.Context) map[string]any {
	if ctx == nil {
		return nil
	}
	meta, _ := ctx.Value(deliveryMetaContextKey{}).(map[string]any)
	if len(meta) == 0 {
		return nil
	}
	cloned := make(map[string]any, len(meta))
	for k, v := range meta {
		cloned[k] = v
	}
	return cloned
}

func EnvFromContext(ctx context.Context) map[string]string {
	if ctx == nil {
		return nil
	}
	if env, ok := ctx.Value(envContextKey{}).(map[string]string); ok && len(env) > 0 {
		copyEnv := make(map[string]string, len(env))
		for k, v := range env {
			copyEnv[k] = v
		}
		return copyEnv
	}
	return nil
}

func RequestSourceFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	source, _ := ctx.Value(requestSourceContextKey{}).(string)
	return strings.ToLower(strings.TrimSpace(source))
}

func CapabilityCeilingFromContext(ctx context.Context) CapabilityLevel {
	if ctx == nil {
		return ""
	}
	level, _ := ctx.Value(capabilityCeilingContextKey{}).(CapabilityLevel)
	return level
}

func ToolGuardFromContext(ctx context.Context) ToolGuard {
	if ctx == nil {
		return nil
	}
	guard, _ := ctx.Value(toolGuardContextKey{}).(ToolGuard)
	return guard
}

func ActiveProfileFromContext(ctx context.Context) ActiveProfile {
	if ctx == nil {
		return ActiveProfile{}
	}
	profile, _ := ctx.Value(activeProfileContextKey{}).(ActiveProfile)
	return profile
}

func SkillPolicyFromContext(ctx context.Context) SkillPolicy {
	if ctx == nil {
		return SkillPolicy{}
	}
	policy, _ := ctx.Value(skillPolicyContextKey{}).(SkillPolicy)
	return policy
}

func ApprovalTokenFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	token, _ := ctx.Value(approvalTokenContextKey{}).(string)
	return strings.TrimSpace(token)
}

func RequesterIdentityFromContext(ctx context.Context) RequesterIdentity {
	if ctx == nil {
		return RequesterIdentity{}
	}
	identity, _ := ctx.Value(requesterIdentityContextKey{}).(RequesterIdentity)
	return identity
}
