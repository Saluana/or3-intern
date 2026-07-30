package streaming

import (
	"context"
	"strings"

	"or3-intern/internal/channels"
)

type conversationObserverContextKey struct{}
type conversationSessionContextKey struct{}
type conversationActionContextKey struct{}
type streamingChannelContextKey struct{}

// ConversationActionSessionReset marks a session reset action in context.
const ConversationActionSessionReset = "session_reset"

// ConversationObserver receives turn streaming callbacks for service jobs and channels.
type ConversationObserver interface {
	OnTextDelta(ctx context.Context, text string)
	OnToolCall(ctx context.Context, name string, arguments string)
	OnToolResult(ctx context.Context, name string, result string, err error)
	OnCompletion(ctx context.Context, finalText string, streamed bool)
	OnError(ctx context.Context, err error)
}

// ToolLifecycleEvent carries structured tool lifecycle data for SSE observers.
type ToolLifecycleEvent struct {
	ToolCallID       string
	Name             string
	Status           string
	Arguments        string
	ArgumentsPreview string
	Result           string
	ResultPreview    string
	ArtifactID       string
	ApprovalID       int64
	PublicCode       string
}

// ToolLifecycleObserver receives structured tool lifecycle events.
type ToolLifecycleObserver interface {
	OnToolLifecycle(ctx context.Context, event ToolLifecycleEvent)
}

func ContextWithConversationObserver(ctx context.Context, observer ConversationObserver) context.Context {
	if observer == nil {
		return ctx
	}
	return context.WithValue(ctx, conversationObserverContextKey{}, observer)
}

func ConversationObserverFromContext(ctx context.Context) ConversationObserver {
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

func StreamingChannelFromContext(ctx context.Context) channels.StreamingChannel {
	if ctx == nil {
		return nil
	}
	streamer, _ := ctx.Value(streamingChannelContextKey{}).(channels.StreamingChannel)
	return streamer
}
