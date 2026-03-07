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
	"or3-intern/internal/channels"
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

	Deliver  Deliverer
	Streamer channels.StreamingChannel

	Consolidator           *memory.Consolidator
	ConsolidationScheduler *memory.Scheduler

	locks sync.Map // sessionKey -> *sync.Mutex
}

type BackgroundRunInput struct {
	SessionKey       string
	ParentSessionKey string
	Task             string
	PromptSnapshot   []providers.ChatMessage
	Tools            *tools.Registry
	Meta             map[string]any
	Channel          string
	ReplyTo          string
}

type BackgroundRunResult struct {
	FinalText  string
	Preview    string
	ArtifactID string
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
	case bus.EventUserMessage, bus.EventCron, bus.EventSystem, bus.EventWebhook, bus.EventFileChange:
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
	isAutonomous := ev.Type == bus.EventCron || ev.Type == bus.EventWebhook || ev.Type == bus.EventFileChange
	messages, err := r.BuildPromptSnapshotWithOptions(ctx, BuildOptions{
		SessionKey:  ev.SessionKey,
		UserMessage: ev.Message,
		Autonomous:  isAutonomous,
	})
	if err != nil {
		return err
	}

	replyTarget := deliveryTarget(ev)
	finalText, streamed, err := r.executeConversation(ctx, ev.SessionKey, messages, r.Tools, ev.Channel, replyTarget)
	if err != nil {
		return err
	}

	if finalText == "" {
		finalText = "(no response)"
	}
	if _, err := r.DB.AppendMessage(ctx, ev.SessionKey, "assistant", finalText, map[string]any{"in_reply_to": msgID}); err != nil {
		log.Printf("append assistant(final) failed: %v", err)
	}

	// deliver only when the response was not already streamed to the channel
	if !streamed && r.Deliver != nil {
		if err := r.Deliver.Deliver(ctx, ev.Channel, replyTarget, finalText); err != nil {
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

func (r *Runtime) BuildPromptSnapshot(ctx context.Context, sessionKey string, userMessage string) ([]providers.ChatMessage, error) {
	if r.Builder == nil {
		return nil, fmt.Errorf("runtime builder not configured")
	}
	pp, _, err := r.Builder.Build(ctx, sessionKey, userMessage)
	if err != nil {
		return nil, err
	}
	messages := append([]providers.ChatMessage{}, pp.System...)
	messages = append(messages, pp.History...)
	if len(pp.History) == 0 || pp.History[len(pp.History)-1].Role != "user" {
		messages = append(messages, providers.ChatMessage{Role: "user", Content: userMessage})
	}
	return messages, nil
}

func (r *Runtime) BuildPromptSnapshotWithOptions(ctx context.Context, opts BuildOptions) ([]providers.ChatMessage, error) {
	if r.Builder == nil {
		return nil, fmt.Errorf("runtime builder not configured")
	}
	pp, _, err := r.Builder.BuildWithOptions(ctx, opts)
	if err != nil {
		return nil, err
	}
	messages := append([]providers.ChatMessage{}, pp.System...)
	messages = append(messages, pp.History...)
	if len(pp.History) == 0 || pp.History[len(pp.History)-1].Role != "user" {
		messages = append(messages, providers.ChatMessage{Role: "user", Content: opts.UserMessage})
	}
	return messages, nil
}

func (r *Runtime) RunBackground(ctx context.Context, input BackgroundRunInput) (BackgroundRunResult, error) {
	mu := r.lockFor(input.SessionKey)
	mu.Lock()
	defer mu.Unlock()

	if strings.TrimSpace(input.SessionKey) == "" {
		return BackgroundRunResult{}, fmt.Errorf("background session key required")
	}
	if len(input.PromptSnapshot) == 0 {
		return BackgroundRunResult{}, fmt.Errorf("background prompt snapshot required")
	}
	if _, err := r.DB.AppendMessage(ctx, input.SessionKey, "user", input.Task, input.Meta); err != nil {
		return BackgroundRunResult{}, err
	}
	reg := input.Tools
	if reg == nil {
		reg = r.Tools
	}
	finalText, _, err := r.executeConversation(ctx, input.SessionKey, append([]providers.ChatMessage{}, input.PromptSnapshot...), reg, input.Channel, input.ReplyTo)
	if err != nil {
		return BackgroundRunResult{}, err
	}
	storedText, preview, artifactID := r.boundTextResult(ctx, input.SessionKey, finalText)
	payload := cloneMap(input.Meta)
	if input.ParentSessionKey != "" {
		payload["parent_session_key"] = input.ParentSessionKey
	}
	if artifactID != "" {
		payload["artifact_id"] = artifactID
		payload["preview"] = preview
	}
	if _, err := r.DB.AppendMessage(ctx, input.SessionKey, "assistant", storedText, payload); err != nil {
		log.Printf("append background assistant(final) failed: %v", err)
	}
	return BackgroundRunResult{FinalText: finalText, Preview: preview, ArtifactID: artifactID}, nil
}

func (r *Runtime) handleNewSession(ctx context.Context, ev bus.Event) error {
	replyTarget := deliveryTarget(ev)
	if r.Consolidator != nil && r.Builder != nil {
		historyMax := r.Builder.HistoryMax
		if historyMax <= 0 {
			historyMax = 40
		}
		if err := r.Consolidator.ArchiveAll(ctx, ev.SessionKey, historyMax); err != nil {
			msg := "Memory archival failed, session not cleared. Please try again."
			if r.Deliver != nil {
				if derr := r.Deliver.Deliver(ctx, ev.Channel, replyTarget, msg); derr != nil {
					log.Printf("deliver failed: %v", derr)
				}
			}
			return nil
		}
	}
	if err := r.DB.ResetSessionHistory(ctx, ev.SessionKey); err != nil {
		msg := "New session failed. Please try again."
		if r.Deliver != nil {
			if derr := r.Deliver.Deliver(ctx, ev.Channel, replyTarget, msg); derr != nil {
				log.Printf("deliver failed: %v", derr)
			}
		}
		return nil
	}
	if r.Deliver != nil {
		if err := r.Deliver.Deliver(ctx, ev.Channel, replyTarget, "New session started."); err != nil {
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

func (r *Runtime) executeConversation(ctx context.Context, sessionKey string, messages []providers.ChatMessage, reg *tools.Registry, channel string, replyTo string) (string, bool, error) {
	if reg == nil {
		reg = tools.NewRegistry()
	}
	maxLoops := r.MaxToolLoops
	if maxLoops <= 0 {
		maxLoops = 6
	}
	for loop := 0; loop < maxLoops; loop++ {
		req := providers.ChatCompletionRequest{
			Model:       r.Model,
			Messages:    messages,
			Tools:       toToolDefs(reg),
			Temperature: r.Temperature,
		}

		// Begin a stream for this turn if a streamer is configured.
		var sw channels.StreamWriter
		var onDelta func(string)
		if r.Streamer != nil {
			if writer, err := r.Streamer.BeginStream(ctx, replyTo, map[string]any{"channel": channel}); err == nil {
				sw = writer
				onDelta = func(text string) { _ = sw.WriteDelta(ctx, text) }
			}
		}

		var resp providers.ChatCompletionResponse
		var err error
		if onDelta != nil {
			resp, err = r.Provider.ChatStream(ctx, req, onDelta)
		} else {
			resp, err = r.Provider.Chat(ctx, req)
		}
		if err != nil {
			if sw != nil {
				_ = sw.Abort(ctx)
			}
			return "", false, err
		}
		if len(resp.Choices) == 0 {
			if sw != nil {
				_ = sw.Abort(ctx)
			}
			return "", false, fmt.Errorf("no choices")
		}
		msg := resp.Choices[0].Message
		if len(msg.ToolCalls) == 0 {
			finalText := strings.TrimSpace(contentToString(msg.Content))
			messages = append(messages, providers.ChatMessage{Role: "assistant", Content: finalText})
			if sw != nil {
				_ = sw.Close(ctx, finalText)
				return finalText, true, nil
			}
			return finalText, false, nil
		}

		// This turn has tool calls — abort any in-progress stream.
		if sw != nil {
			_ = sw.Abort(ctx)
		}

		messages = append(messages, providers.ChatMessage{Role: "assistant", Content: msg.Content, ToolCalls: msg.ToolCalls})
		if _, err := r.DB.AppendMessage(ctx, sessionKey, "assistant", contentToString(msg.Content), map[string]any{"tool_calls": msg.ToolCalls}); err != nil {
			log.Printf("append assistant(tool_calls) failed: %v", err)
		}

		for _, tc := range msg.ToolCalls {
			toolCtx := tools.ContextWithSession(ctx, sessionKey)
			toolCtx = tools.ContextWithDelivery(toolCtx, channel, replyTo)
			out, err := reg.Execute(toolCtx, tc.Function.Name, tc.Function.Arguments)
			if err != nil {
				out = "tool error: " + err.Error()
			}

			payload := map[string]any{
				"tool": tc.Function.Name,
				"args": json.RawMessage([]byte(tc.Function.Arguments)),
			}
			sendOut, preview, artifactID := r.boundTextResult(ctx, sessionKey, out)
			if artifactID != "" {
				payload["artifact_id"] = artifactID
				payload["preview"] = preview
			}
			if _, err := r.DB.AppendMessage(ctx, sessionKey, "tool", sendOut, payload); err != nil {
				log.Printf("append tool message failed: %v", err)
			}
			messages = append(messages, providers.ChatMessage{Role: "tool", ToolCallID: tc.ID, Content: sendOut})
		}
	}
	return "(no response)", false, nil
}

func (r *Runtime) boundTextResult(ctx context.Context, sessionKey string, text string) (stored string, preview string, artifactID string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return "(no response)", "(no response)", ""
	}
	preview = previewText(text, r.toolPreviewBytes())
	if r.MaxToolBytes > 0 && len(text) > r.MaxToolBytes && r.Artifacts != nil {
		id, err := r.Artifacts.Save(ctx, sessionKey, "text/plain", []byte(text))
		if err != nil {
			log.Printf("artifact save failed: %v", err)
			return text, preview, ""
		}
		return fmt.Sprintf("artifact_id=%s\npreview:\n%s", id, preview), preview, id
	}
	return text, preview, ""
}

func (r *Runtime) toolPreviewBytes() int {
	if r.ToolPreviewBytes <= 0 {
		return 500
	}
	return r.ToolPreviewBytes
}

func previewText(s string, max int) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "(no response)"
	}
	if max > 0 && len(s) > max {
		return s[:max] + "…[preview]"
	}
	return s
}

func cloneMap(in map[string]any) map[string]any {
	if len(in) == 0 {
		return map[string]any{}
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func deliveryTarget(ev bus.Event) string {
	if len(ev.Meta) > 0 {
		for _, key := range []string{"chat_id", "channel_id"} {
			if target := strings.TrimSpace(fmt.Sprint(ev.Meta[key])); target != "" && target != "<nil>" {
				return target
			}
		}
	}
	return strings.TrimSpace(ev.From)
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
func CronRunner(b *bus.Bus, defaultSessionKey string) cron.Runner {
	return func(ctx context.Context, job cron.CronJob) error {
		_ = ctx
		msg := job.Payload.Message
		if strings.TrimSpace(msg) == "" {
			msg = "cron job: " + job.Name
		}
		// prefer per-job session key over the default
		sessionKey := job.Payload.SessionKey
		if strings.TrimSpace(sessionKey) == "" {
			sessionKey = defaultSessionKey
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
