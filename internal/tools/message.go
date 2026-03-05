package tools

import (
	"context"
	"fmt"
	"strings"
)

type DeliverFunc func(ctx context.Context, channel, to, text string) error

type SendMessage struct {
	Base
	Deliver DeliverFunc
	DefaultChannel string
	DefaultTo string
}

func (t *SendMessage) Name() string { return "send_message" }
func (t *SendMessage) Description() string {
	return "Send a message via a configured channel (for reminders/cron or proactive messages)."
}
func (t *SendMessage) Parameters() map[string]any {
	return map[string]any{"type":"object","properties":map[string]any{
		"channel": map[string]any{"type":"string"},
		"to": map[string]any{"type":"string"},
		"text": map[string]any{"type":"string"},
	},"required":[]string{"text"}}
}
func (t *SendMessage) Schema() map[string]any { return t.SchemaFor(t.Name(), t.Description(), t.Parameters()) }
func (t *SendMessage) Execute(ctx context.Context, params map[string]any) (string, error) {
	if t.Deliver == nil { return "", fmt.Errorf("deliver not configured") }
	ch := strings.TrimSpace(fmt.Sprint(params["channel"]))
	to := strings.TrimSpace(fmt.Sprint(params["to"]))
	text := strings.TrimSpace(fmt.Sprint(params["text"]))
	if ch == "" { ch = t.DefaultChannel }
	if to == "" { to = t.DefaultTo }
	if text == "" { return "", fmt.Errorf("empty text") }
	if err := t.Deliver(ctx, ch, to, text); err != nil { return "", err }
	return "ok", nil
}
