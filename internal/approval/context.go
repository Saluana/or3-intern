package approval

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

type requesterContextKey struct{}

func ContextWithRequesterContext(ctx context.Context, requester RequesterContext) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	requester = NormalizeRequesterContext(requester)
	if requester.IsZero() {
		return ctx
	}
	return context.WithValue(ctx, requesterContextKey{}, requester)
}

func RequesterContextFromContext(ctx context.Context) RequesterContext {
	if ctx == nil {
		return RequesterContext{}
	}
	requester, _ := ctx.Value(requesterContextKey{}).(RequesterContext)
	return NormalizeRequesterContext(requester)
}

func RequesterContextFromJSON(raw string) RequesterContext {
	if strings.TrimSpace(raw) == "" {
		return RequesterContext{}
	}
	var requester RequesterContext
	if err := json.Unmarshal([]byte(raw), &requester); err != nil {
		return RequesterContext{}
	}
	return NormalizeRequesterContext(requester)
}

func MarshalRequesterContext(requester RequesterContext) string {
	requester = NormalizeRequesterContext(requester)
	if requester.IsZero() {
		return "{}"
	}
	raw, err := json.Marshal(requester)
	if err != nil {
		return "{}"
	}
	return string(raw)
}

func NormalizeRequesterContext(requester RequesterContext) RequesterContext {
	requester.Channel = strings.ToLower(strings.TrimSpace(requester.Channel))
	requester.SessionKey = strings.TrimSpace(requester.SessionKey)
	requester.From = strings.TrimSpace(requester.From)
	requester.ReplyTarget = strings.TrimSpace(requester.ReplyTarget)
	requester.SourceMessageID = strings.TrimSpace(requester.SourceMessageID)
	requester.ReplyMeta = normalizeRequesterReplyMeta(requester.ReplyMeta)
	return requester
}

func (requester RequesterContext) IsZero() bool {
	return strings.TrimSpace(requester.Channel) == "" &&
		strings.TrimSpace(requester.SessionKey) == "" &&
		strings.TrimSpace(requester.From) == "" &&
		strings.TrimSpace(requester.ReplyTarget) == "" &&
		strings.TrimSpace(requester.SourceMessageID) == "" &&
		len(requester.ReplyMeta) == 0
}

func normalizeRequesterReplyMeta(meta map[string]any) map[string]any {
	if len(meta) == 0 {
		return nil
	}
	out := map[string]any{}
	for _, key := range []string{"thread_ts", "reply_to_message_id", "message_reference"} {
		value, ok := meta[key]
		if !ok {
			continue
		}
		if normalized, ok := normalizeRequesterMetaValue(value); ok {
			out[key] = normalized
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func normalizeRequesterMetaValue(value any) (any, bool) {
	switch v := value.(type) {
	case nil:
		return nil, false
	case string:
		trimmed := strings.TrimSpace(v)
		return trimmed, trimmed != ""
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64, bool:
		text := strings.TrimSpace(fmt.Sprint(v))
		return v, text != "" && text != "<nil>"
	default:
		text := strings.TrimSpace(fmt.Sprint(v))
		if text == "" || text == "<nil>" {
			return nil, false
		}
		return text, true
	}
}
