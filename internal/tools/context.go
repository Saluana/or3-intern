package tools

import (
	"context"

	"or3-intern/internal/scope"
)

type sessionContextKey struct{}
type deliveryChannelContextKey struct{}
type deliveryToContextKey struct{}
type envContextKey struct{}

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
