package agent

import (
	"context"
	"strings"
)

type turnModelOverrideContextKey struct{}

// ContextWithTurnModelOverride scopes a single chat turn to a specific model ID.
func ContextWithTurnModelOverride(ctx context.Context, model string) context.Context {
	model = strings.TrimSpace(model)
	if ctx == nil || model == "" {
		return ctx
	}
	return context.WithValue(ctx, turnModelOverrideContextKey{}, model)
}

func turnModelOverrideFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	model, _ := ctx.Value(turnModelOverrideContextKey{}).(string)
	return strings.TrimSpace(model)
}
