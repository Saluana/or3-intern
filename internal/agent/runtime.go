package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"or3-intern/internal/artifacts"
	"or3-intern/internal/bus"
	"or3-intern/internal/cron"
	"or3-intern/internal/db"
	"or3-intern/internal/memory"
	"or3-intern/internal/providers"
	"or3-intern/internal/tools"
)

const commandNewSession = "/new"

type Deliverer interface {
	Deliver(ctx context.Context, channel, to, text string) error
}

type Runtime struct {
	DB               *db.DB
	Provider         *providers.Client
	Model            string
	Temperature      float64
	Tools            *tools.Registry
	Builder          *Builder
	Artifacts        *artifacts.Store
	MaxToolBytes     int
	MaxToolLoops     int
	ToolPreviewBytes int

	Deliver Deliverer

	Consolidator           *memory.Consolidator
	ConsolidationScheduler *memory.Scheduler

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
	if ev.Type == bus.EventUserMessage && strings.EqualFold(strings.TrimSpace(ev.Message), commandNewSession) {
		return r.handleNewSession(ctx, ev)
	}

	// persist user message
	msgID, err := r.DB.AppendMessage(ctx, ev.SessionKey, "user", ev.Message, map[string]any{
		"channel": ev.Channel, "from": ev.From, "meta": ev.Meta,
	})
	if err != nil {
		return err
	}

	// build prompt
	if r.Builder == nil {
		return fmt.Errorf("runtime builder not configured")
	}
	pp, _, err := r.Builder.Build(ctx, ev.SessionKey, ev.Message)
	if err != nil {
		return err
	}

	messages := append([]providers.ChatMessage{}, pp.System...)
	messages = append(messages, pp.History...)
	messages = append(messages, providers.ChatMessage{Role: "user", Content: ev.Message})

	// tool loop
	maxLoops := r.MaxToolLoops
	if maxLoops <= 0 {
		maxLoops = 6
	}
	previewBytes := r.ToolPreviewBytes
	if previewBytes <= 0 {
		previewBytes = 500
	}
	var finalText string
	for loop := 0; loop < maxLoops; loop++ {
		req := providers.ChatCompletionRequest{
			Model:       r.Model,
			Messages:    messages,
			Tools:       toToolDefs(r.Tools),
			Temperature: r.Temperature,
		}
		resp, err := r.Provider.Chat(ctx, req)
		if err != nil {
			return err
		}
		if len(resp.Choices) == 0 {
			return fmt.Errorf("no choices")
		}
		msg := resp.Choices[0].Message
		if len(msg.ToolCalls) == 0 {
			finalText = strings.TrimSpace(contentToString(msg.Content))
			messages = append(messages, providers.ChatMessage{Role: "assistant", Content: finalText})
			break
		}
		// assistant message with tool calls (store content too)
		messages = append(messages, providers.ChatMessage{Role: "assistant", Content: msg.Content, ToolCalls: msg.ToolCalls})
		if _, err := r.DB.AppendMessage(ctx, ev.SessionKey, "assistant", contentToString(msg.Content), map[string]any{"tool_calls": msg.ToolCalls}); err != nil {
			log.Printf("append assistant(tool_calls) failed: %v", err)
		}

		// execute tools sequentially
		for _, tc := range msg.ToolCalls {
			toolCtx := tools.ContextWithSession(ctx, ev.SessionKey)
			out, err := r.Tools.Execute(toolCtx, tc.Function.Name, tc.Function.Arguments)
			if err != nil {
				out = "tool error: " + err.Error()
			}

			payload := map[string]any{
				"tool": tc.Function.Name,
				"args": json.RawMessage([]byte(tc.Function.Arguments)),
			}
			sendOut := out
			if r.MaxToolBytes > 0 && len(out) > r.MaxToolBytes && r.Artifacts != nil {
				id, e := r.Artifacts.Save(ctx, ev.SessionKey, "text/plain", []byte(out))
				if e == nil {
					preview := out
					if len(preview) > previewBytes {
						preview = preview[:previewBytes] + "…[preview]"
					}
					payload["artifact_id"] = id
					payload["preview"] = preview
					sendOut = fmt.Sprintf("artifact_id=%s\npreview:\n%s", id, preview)
				} else {
					log.Printf("artifact save failed: %v", e)
				}
			}
			// persist tool result as a message with role=tool? nanobot stores tool outputs; we keep it but bounded.
			if _, err := r.DB.AppendMessage(ctx, ev.SessionKey, "tool", sendOut, payload); err != nil {
				log.Printf("append tool message failed: %v", err)
			}
			messages = append(messages, providers.ChatMessage{Role: "tool", ToolCallID: tc.ID, Content: sendOut})
		}
	}

	if finalText == "" {
		finalText = "(no response)"
	}
	if _, err := r.DB.AppendMessage(ctx, ev.SessionKey, "assistant", finalText, map[string]any{"in_reply_to": msgID}); err != nil {
		log.Printf("append assistant(final) failed: %v", err)
	}

	// deliver
	if r.Deliver != nil {
		if err := r.Deliver.Deliver(ctx, ev.Channel, ev.From, finalText); err != nil {
			log.Printf("deliver failed: %v", err)
		}
	}

	// best-effort rolling consolidation of old messages into memory notes
	if r.Consolidator != nil && r.Builder != nil && r.ConsolidationScheduler != nil {
		r.ConsolidationScheduler.Trigger(ev.SessionKey)
	} else if r.Consolidator != nil && r.Builder != nil {
		historyMax := r.Builder.HistoryMax
		if historyMax <= 0 {
			historyMax = 40
		}
		if err := r.Consolidator.MaybeConsolidate(ctx, ev.SessionKey, historyMax); err != nil {
			log.Printf("consolidation failed: session=%s err=%v", ev.SessionKey, err)
		}
	}

	return nil
}

func (r *Runtime) handleNewSession(ctx context.Context, ev bus.Event) error {
	if r.Consolidator != nil && r.Builder != nil {
		historyMax := r.Builder.HistoryMax
		if historyMax <= 0 {
			historyMax = 40
		}
		if err := r.Consolidator.ArchiveAll(ctx, ev.SessionKey, historyMax); err != nil {
			msg := "Memory archival failed, session not cleared. Please try again."
			if r.Deliver != nil {
				if derr := r.Deliver.Deliver(ctx, ev.Channel, ev.From, msg); derr != nil {
					log.Printf("deliver failed: %v", derr)
				}
			}
			return nil
		}
	}
	if err := r.DB.ResetSessionHistory(ctx, ev.SessionKey); err != nil {
		msg := "New session failed. Please try again."
		if r.Deliver != nil {
			if derr := r.Deliver.Deliver(ctx, ev.Channel, ev.From, msg); derr != nil {
				log.Printf("deliver failed: %v", derr)
			}
		}
		return nil
	}
	if r.Deliver != nil {
		if err := r.Deliver.Deliver(ctx, ev.Channel, ev.From, "New session started."); err != nil {
			log.Printf("deliver failed: %v", err)
		}
	}
	return nil
}

func contentToString(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	b, _ := json.Marshal(v)
	return string(b)
}

func toToolDefs(reg *tools.Registry) []providers.ToolDef {
	if reg == nil {
		return nil
	}
	raw := reg.Definitions()
	out := make([]providers.ToolDef, 0, len(raw))
	for _, d := range raw {
		fn, _ := d["function"].(map[string]any)
		td := providers.ToolDef{
			Type: "function",
			Function: providers.ToolFunc{
				Name:        fmt.Sprint(fn["name"]),
				Description: fmt.Sprint(fn["description"]),
				Parameters:  fn["parameters"],
			},
		}
		out = append(out, td)
	}
	return out
}

// Cron runner helper: turns a job into a bus event message
func CronRunner(b *bus.Bus, sessionKey string) cron.Runner {
	return func(ctx context.Context, job cron.CronJob) error {
		_ = ctx
		msg := job.Payload.Message
		if strings.TrimSpace(msg) == "" {
			msg = "cron job: " + job.Name
		}
		ev := bus.Event{Type: bus.EventCron, SessionKey: sessionKey, Channel: job.Payload.Channel, From: job.Payload.To, Message: msg, Meta: map[string]any{"job_id": job.ID}}
		if ok := b.Publish(ev); !ok {
			return fmt.Errorf("event bus full")
		}
		return nil
	}
}

func WithTimeout(ctx context.Context, sec int) (context.Context, context.CancelFunc) {
	if sec <= 0 {
		sec = 60
	}
	return context.WithTimeout(ctx, time.Duration(sec)*time.Second)
}
