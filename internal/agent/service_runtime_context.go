package agent

import (
	"context"
	"strings"

	"or3-intern/internal/channels"
	"or3-intern/internal/tools"
)

type conversationObserverContextKey struct{}
type conversationSessionContextKey struct{}
type conversationActionContextKey struct{}
type streamingChannelContextKey struct{}
type toolRegistryContextKey struct{}

const ConversationActionSessionReset = "session_reset"

type ConversationObserver interface {
	OnTextDelta(ctx context.Context, text string)
	OnToolCall(ctx context.Context, name string, arguments string)
	OnToolResult(ctx context.Context, name string, result string, err error)
	OnCompletion(ctx context.Context, finalText string, streamed bool)
	OnError(ctx context.Context, err error)
}

func ContextWithConversationObserver(ctx context.Context, observer ConversationObserver) context.Context {
	if observer == nil {
		return ctx
	}
	return context.WithValue(ctx, conversationObserverContextKey{}, observer)
}

func conversationObserverFromContext(ctx context.Context) ConversationObserver {
	if ctx == nil {
		return nil
	}
	observer, _ := ctx.Value(conversationObserverContextKey{}).(ConversationObserver)
	return observer
}

func ContextWithConversationSession(ctx context.Context, sessionKey string) context.Context {
	sessionKey = strings.TrimSpace(sessionKey)
	if ctx == nil || sessionKey == "" {
		return ctx
	}
	return context.WithValue(ctx, conversationSessionContextKey{}, sessionKey)
}

func ConversationSessionFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	sessionKey, _ := ctx.Value(conversationSessionContextKey{}).(string)
	return strings.TrimSpace(sessionKey)
}

func ContextWithConversationAction(ctx context.Context, action string) context.Context {
	action = strings.TrimSpace(action)
	if ctx == nil || action == "" {
		return ctx
	}
	return context.WithValue(ctx, conversationActionContextKey{}, action)
}

func ConversationActionFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	action, _ := ctx.Value(conversationActionContextKey{}).(string)
	return strings.TrimSpace(action)
}

func ContextWithStreamingChannel(ctx context.Context, streamer channels.StreamingChannel) context.Context {
	if streamer == nil {
		return ctx
	}
	return context.WithValue(ctx, streamingChannelContextKey{}, streamer)
}

func streamingChannelFromContext(ctx context.Context) channels.StreamingChannel {
	if ctx == nil {
		return nil
	}
	streamer, _ := ctx.Value(streamingChannelContextKey{}).(channels.StreamingChannel)
	return streamer
}

func ContextWithToolRegistry(ctx context.Context, reg *tools.Registry) context.Context {
	if reg == nil {
		return ctx
	}
	return context.WithValue(ctx, toolRegistryContextKey{}, reg)
}

func toolRegistryFromContext(ctx context.Context) *tools.Registry {
	if ctx == nil {
		return nil
	}
	reg, _ := ctx.Value(toolRegistryContextKey{}).(*tools.Registry)
	return reg
}

func toolRegistryWithAllowlist(base *tools.Registry, allowed []string, restrict bool) *tools.Registry {
	if base == nil {
		return tools.NewRegistry()
	}
	trimmed := make([]string, 0, len(allowed))
	for _, name := range allowed {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		trimmed = append(trimmed, name)
	}
	if !restrict {
		return base
	}
	if len(trimmed) == 0 {
		return tools.NewRegistry()
	}
	return base.CloneFiltered(trimmed)
}
