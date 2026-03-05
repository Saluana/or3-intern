package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"or3-intern/internal/artifacts"
	"or3-intern/internal/bus"
	"or3-intern/internal/cron"
	"or3-intern/internal/db"
	"or3-intern/internal/memory"
	"or3-intern/internal/providers"
	"or3-intern/internal/skills"
	"or3-intern/internal/tools"
)

type Deliverer interface {
	Deliver(ctx context.Context, channel, to, text string) error
}

type Runtime struct {
	DB *db.DB
	Provider *providers.Client
	Model string
	EmbedModel string
	Tools *tools.Registry
	Builder *Builder
	Artifacts *artifacts.Store
	MaxToolBytes int
	HistoryMax int
	VectorK int
	FTSK int
	TopK int

	Deliver Deliverer

	locks sync.Map // sessionKey -> *sync.Mutex
}

func (r *Runtime) lockFor(key string) *sync.Mutex {
	v, _ := r.locks.LoadOrStore(key, &sync.Mutex{})
	return v.(*sync.Mutex)
}

func (r *Runtime) Handle(ctx context.Context, ev bus.Event) error {
	mu := r.lockFor(ev.SessionKey)
	mu.Lock()
	defer mu.Unlock()
	switch ev.Type {
	case bus.EventUserMessage, bus.EventCron, bus.EventSystem:
		return r.turn(ctx, ev)
	default:
		return nil
	}
}

func (r *Runtime) turn(ctx context.Context, ev bus.Event) error {
	// persist user message
	msgID, err := r.DB.AppendMessage(ctx, ev.SessionKey, "user", ev.Message, map[string]any{
		"channel": ev.Channel, "from": ev.From, "meta": ev.Meta,
	})
	if err != nil { return err }

	// build prompt
	if r.Builder == nil {
		r.Builder = &Builder{
			DB: r.DB,
			Skills: skills.Inventory{},
			Mem: memory.NewRetriever(r.DB),
			Provider: r.Provider,
			EmbedModel: r.EmbedModel,
			HistoryMax: r.HistoryMax,
			VectorK: r.VectorK,
			FTSK: r.FTSK,
			TopK: r.TopK,
		}
	}
	pp, _, err := r.Builder.Build(ctx, ev.SessionKey, ev.Message)
	if err != nil { return err }

	messages := append([]providers.ChatMessage{}, pp.System...)
	messages = append(messages, pp.History...)
	messages = append(messages, providers.ChatMessage{Role: "user", Content: ev.Message})

	// tool loop
	maxLoops := 6
	var finalText string
	for loop := 0; loop < maxLoops; loop++ {
		req := providers.ChatCompletionRequest{
			Model: r.Model,
			Messages: messages,
			Tools: toToolDefs(r.Tools),
			Temperature: 0,
		}
		resp, err := r.Provider.Chat(ctx, req)
		if err != nil { return err }
		if len(resp.Choices) == 0 { return fmt.Errorf("no choices") }
		msg := resp.Choices[0].Message
		if len(msg.ToolCalls) == 0 {
			finalText = strings.TrimSpace(msg.Content)
			messages = append(messages, providers.ChatMessage{Role: "assistant", Content: finalText})
			break
		}
		// assistant message with tool calls (store content too)
		messages = append(messages, providers.ChatMessage{Role: "assistant", Content: msg.Content})

		// execute tools sequentially
		for _, tc := range msg.ToolCalls {
			out, err := r.Tools.Execute(ctx, tc.Function.Name, tc.Function.Arguments)
			if err != nil { out = "tool error: " + err.Error() }

			payload := map[string]any{
				"tool": tc.Function.Name,
				"args": json.RawMessage([]byte(tc.Function.Arguments)),
			}
			sendOut := out
			if r.MaxToolBytes > 0 && len(out) > r.MaxToolBytes && r.Artifacts != nil {
				id, e := r.Artifacts.Save(ctx, ev.SessionKey, "text/plain", []byte(out))
				if e == nil {
					preview := out
					if len(preview) > 500 { preview = preview[:500] + "…[preview]" }
					payload["artifact_id"] = id
					payload["preview"] = preview
					sendOut = fmt.Sprintf("artifact_id=%s\npreview:\n%s", id, preview)
				}
			}
			// persist tool result as a message with role=tool? nanobot stores tool outputs; we keep it but bounded.
			_, _ = r.DB.AppendMessage(ctx, ev.SessionKey, "tool", sendOut, payload)
			messages = append(messages, providers.ChatMessage{Role: "tool", ToolCallID: tc.ID, Content: sendOut})
		}
	}

	if finalText == "" {
		finalText = "(no response)"
	}
	_, _ = r.DB.AppendMessage(ctx, ev.SessionKey, "assistant", finalText, map[string]any{"in_reply_to": msgID})

	// deliver
	if r.Deliver != nil {
		_ = r.Deliver.Deliver(ctx, ev.Channel, ev.From, finalText)
	}
	return nil
}

func toToolDefs(reg *tools.Registry) []providers.ToolDef {
	if reg == nil { return nil }
	raw := reg.Definitions()
	out := make([]providers.ToolDef, 0, len(raw))
	for _, d := range raw {
		fn, _ := d["function"].(map[string]any)
		td := providers.ToolDef{
			Type: "function",
			Function: providers.ToolFunc{
				Name: fmt.Sprint(fn["name"]),
				Description: fmt.Sprint(fn["description"]),
				Parameters: fn["parameters"],
			},
		}
		out = append(out, td)
	}
	return out
}

// Cron runner helper: turns a job into a bus event message
func CronRunner(b *bus.Bus, sessionKey string) cron.Runner {
	return func(ctx context.Context, job cron.CronJob) error {
		msg := job.Payload.Message
		if strings.TrimSpace(msg) == "" { msg = "cron job: " + job.Name }
		ev := bus.Event{Type: bus.EventCron, SessionKey: sessionKey, Channel: job.Payload.Channel, From: job.Payload.To, Message: msg, Meta: map[string]any{"job_id": job.ID}}
		b.Publish(ev)
		return nil
	}
}

func WithTimeout(ctx context.Context, sec int) (context.Context, context.CancelFunc) {
	if sec <= 0 { sec = 60 }
	return context.WithTimeout(ctx, time.Duration(sec)*time.Second)
}
