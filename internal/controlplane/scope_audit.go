package controlplane

import (
	"context"
	"strings"
)

type scopeActorContextKey struct{}

// WithScopeActor attaches an audit actor to ctx for scope operations.
func WithScopeActor(ctx context.Context, actor string) context.Context {
	actor = strings.TrimSpace(actor)
	if actor == "" {
		return ctx
	}
	return context.WithValue(ctx, scopeActorContextKey{}, actor)
}

func scopeActor(explicit string, ctx context.Context) string {
	if actor := strings.TrimSpace(explicit); actor != "" {
		return actor
	}
	if ctx != nil {
		if actor, ok := ctx.Value(scopeActorContextKey{}).(string); ok && strings.TrimSpace(actor) != "" {
			return strings.TrimSpace(actor)
		}
	}
	return "unknown"
}
