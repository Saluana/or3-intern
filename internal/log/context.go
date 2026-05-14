package log

import (
	"context"
	"strings"
)

type traceIDContextKey struct{}

func WithTraceID(ctx context.Context, traceID string) context.Context {
	traceID = strings.TrimSpace(traceID)
	if ctx == nil || traceID == "" {
		return ctx
	}
	return context.WithValue(ctx, traceIDContextKey{}, traceID)
}

func TraceIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	traceID, _ := ctx.Value(traceIDContextKey{}).(string)
	return strings.TrimSpace(traceID)
}
