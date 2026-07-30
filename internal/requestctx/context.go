// Package requestctx carries request-scoped identity and delivery metadata for service actions.
package requestctx

import (
	"context"
	"fmt"
	"strings"

	"or3-intern/internal/approval"
	"or3-intern/internal/capability"
	"or3-intern/internal/scope"
)

type sessionContextKey struct{}
type deliveryChannelContextKey struct{}
type deliveryToContextKey struct{}
type deliveryFromContextKey struct{}
type deliveryMetaContextKey struct{}
type envContextKey struct{}
type approvalTokenContextKey struct{}
type requesterIdentityContextKey struct{}
type requestSourceContextKey struct{}
type capabilityCeilingContextKey struct{}

// RequesterIdentity identifies the actor performing a service action.
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

func ContextWithDeliveryFrom(ctx context.Context, from string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	from = strings.TrimSpace(from)
	if from == "" {
		return ctx
	}
	return context.WithValue(ctx, deliveryFromContextKey{}, from)
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

func ContextWithApprovalRequesterContext(ctx context.Context) context.Context {
	return ContextWithApprovalRequesterContextForSession(ctx, SessionFromContext(ctx))
}

func ContextWithApprovalRequesterContextForSession(ctx context.Context, sessionKey string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	channel, to := DeliveryFromContext(ctx)
	meta := DeliveryMetaFromContext(ctx)
	requester := approval.RequesterContext{
		Channel:         channel,
		SessionKey:      strings.TrimSpace(sessionKey),
		From:            DeliveryOriginFromContext(ctx),
		ReplyTarget:     to,
		ReplyMeta:       meta,
		SourceMessageID: sourceMessageIDFromReplyMeta(meta),
	}
	return approval.ContextWithRequesterContext(ctx, requester)
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

func ContextWithCapabilityCeiling(ctx context.Context, level capability.Level) context.Context {
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

func DeliveryOriginFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	from, _ := ctx.Value(deliveryFromContextKey{}).(string)
	return strings.TrimSpace(from)
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

func sourceMessageIDFromReplyMeta(meta map[string]any) string {
	for _, key := range []string{"message_reference", "reply_to_message_id", "thread_ts"} {
		if value, ok := meta[key]; ok {
			text := strings.TrimSpace(strings.Trim(strings.TrimSpace(anyToString(value)), `"`))
			if text != "" && text != "<nil>" {
				return text
			}
		}
	}
	return ""
}

func anyToString(value any) string {
	if value == nil {
		return ""
	}
	switch v := value.(type) {
	case string:
		return v
	default:
		return fmt.Sprint(v)
	}
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

func CapabilityCeilingFromContext(ctx context.Context) capability.Level {
	if ctx == nil {
		return ""
	}
	level, _ := ctx.Value(capabilityCeilingContextKey{}).(capability.Level)
	return level
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
