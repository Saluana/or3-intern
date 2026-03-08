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
	"or3-intern/internal/heartbeat"
	"or3-intern/internal/memory"
	"or3-intern/internal/providers"
	"or3-intern/internal/skills"
	"or3-intern/internal/tools"
)

const commandNewSession = "/new"

type Deliverer interface {
	Deliver(ctx context.Context, channel, to, text string) error
}

type sessionLock struct {
	mu   sync.Mutex
	refs int
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
	DefaultScopeKey        string
	LinkDirectMessages     bool
	IdentityScopeMap       map[string]string

	locksMu sync.Mutex
	locks   map[string]*sessionLock
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
	return &r.getSessionLock(key).mu
}

func (r *Runtime) acquireSessionLock(key string) *sessionLock {
	r.locksMu.Lock()
	if r.locks == nil {
		r.locks = map[string]*sessionLock{}
	}
	entry := r.locks[key]
	if entry == nil {
		entry = &sessionLock{}
		r.locks[key] = entry
	}
	entry.refs++
	r.locksMu.Unlock()
	return entry
}

func (r *Runtime) releaseSessionLock(key string, entry *sessionLock) {
	if r == nil || entry == nil {
		return
	}
	r.locksMu.Lock()
	if entry.refs > 0 {
		entry.refs--
	}
	if entry.refs == 0 {
		if current := r.locks[key]; current == entry {
			delete(r.locks, key)
		}
	}
	r.locksMu.Unlock()
}

func (r *Runtime) getSessionLock(key string) *sessionLock {
	r.locksMu.Lock()
	defer r.locksMu.Unlock()
	if r.locks == nil {
		r.locks = map[string]*sessionLock{}
	}
	entry := r.locks[key]
	if entry == nil {
		entry = &sessionLock{}
		r.locks[key] = entry
	}
	return entry
}

func (r *Runtime) Handle(ctx context.Context, ev bus.Event) error {
	entry := r.acquireSessionLock(ev.SessionKey)
	entry.mu.Lock()
	defer func() {
		entry.mu.Unlock()
		r.releaseSessionLock(ev.SessionKey, entry)
	}()
	switch ev.Type {
	case bus.EventUserMessage, bus.EventCron, bus.EventHeartbeat, bus.EventSystem, bus.EventWebhook, bus.EventFileChange:
		return r.turn(ctx, ev)
	default:
		return nil
	}
}

func (r *Runtime) turn(ctx context.Context, ev bus.Event) error {
	defer releaseEvent(ev)

	if ev.Type == bus.EventUserMessage && strings.EqualFold(strings.TrimSpace(ev.Message), commandNewSession) {
		return r.handleNewSession(ctx, ev)
	}
	r.ensureSessionScope(ctx, ev)

	// persist user message
	msgID, err := r.DB.AppendMessage(ctx, ev.SessionKey, "user", ev.Message, map[string]any{
		"channel": ev.Channel, "from": ev.From, "meta": ev.Meta,
	})
	if err != nil {
		return err
	}
	if handled, err := r.handleExplicitSkillInvocation(ctx, ev, msgID); handled || err != nil {
		return err
	}

	// build prompt
	if r.Builder == nil {
		return fmt.Errorf("runtime builder not configured")
	}
	isAutonomous := isAutonomousEvent(ev.Type)
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

	r.persistAssistantReply(ctx, ev.SessionKey, msgID, ev.Channel, replyTarget, finalText, streamed, shouldAutoDeliver(ev))

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

func (r *Runtime) ensureSessionScope(ctx context.Context, ev bus.Event) {
	if r == nil || r.DB == nil || strings.TrimSpace(ev.SessionKey) == "" {
		return
	}
	scopeKey, ok := r.scopeKeyForEvent(ev)
	if !ok {
		return
	}
	scopeKey = strings.TrimSpace(scopeKey)
	if scopeKey == "" || scopeKey == ev.SessionKey {
		return
	}
	meta := map[string]any{"auto": true, "channel": ev.Channel}
	_ = r.DB.LinkSession(ctx, ev.SessionKey, scopeKey, meta)
}

func (r *Runtime) scopeKeyForEvent(ev bus.Event) (string, bool) {
	if r == nil {
		return "", false
	}
	if scopeKey := strings.TrimSpace(r.IdentityScopeMap[ev.SessionKey]); scopeKey != "" {
		return scopeKey, true
	}
	if r.LinkDirectMessages && isDirectMessageEvent(ev) {
		scopeKey := strings.TrimSpace(r.DefaultScopeKey)
		if scopeKey == "" {
			scopeKey = ev.SessionKey
		}
		return scopeKey, true
	}
	return "", false
}

func isDirectMessageEvent(ev bus.Event) bool {
	if len(ev.Meta) == 0 {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(ev.Channel)) {
	case "telegram":
		return strings.EqualFold(strings.TrimSpace(fmt.Sprint(ev.Meta["chat_type"])), "private")
	case "slack":
		return strings.EqualFold(strings.TrimSpace(fmt.Sprint(ev.Meta["channel_type"])), "im")
	case "discord":
		if v, ok := ev.Meta["is_private"].(bool); ok {
			return v
		}
		return strings.TrimSpace(fmt.Sprint(ev.Meta["guild_id"])) == ""
	case "whatsapp":
		if v, ok := ev.Meta["is_group"].(bool); ok {
			return !v
		}
	case "email":
		return true
	}
	return false
}

func (r *Runtime) handleExplicitSkillInvocation(ctx context.Context, ev bus.Event, msgID int64) (bool, error) {
	if ev.Type != bus.EventUserMessage || r.Builder == nil {
		return false, nil
	}
	commandName, rawArgs, ok := parseSkillCommand(ev.Message)
	if !ok || commandName == "new" {
		return false, nil
	}
	replyTarget := deliveryTarget(ev)
	skill, found := r.Builder.Skills.Get(commandName)
	if !found {
		return false, nil
	}
	if !skill.UserInvocable {
		r.persistAssistantReply(ctx, ev.SessionKey, msgID, ev.Channel, replyTarget, "skill is not user-invocable: "+skill.Name, false, shouldAutoDeliver(ev))
		return true, nil
	}
	if !skill.Eligible {
		reasons := append([]string{}, skill.Missing...)
		reasons = append(reasons, skill.Unsupported...)
		if skill.ParseError != "" {
			reasons = append(reasons, skill.ParseError)
		}
		message := "skill unavailable: " + skill.Name
		if len(reasons) > 0 {
			message += " (" + strings.Join(reasons, "; ") + ")"
		}
		r.persistAssistantReply(ctx, ev.SessionKey, msgID, ev.Channel, replyTarget, message, false, shouldAutoDeliver(ev))
		return true, nil
	}
	if skill.CommandDispatch == "tool" {
		text := r.dispatchExplicitSkillTool(ctx, ev, skill, commandName, rawArgs)
		r.persistAssistantReply(ctx, ev.SessionKey, msgID, ev.Channel, replyTarget, text, false, shouldAutoDeliver(ev))
		return true, nil
	}

	promptInput := strings.TrimSpace(rawArgs)
	if promptInput == "" {
		promptInput = skill.Name
	}
	messages, err := r.BuildPromptSnapshotWithOptions(ctx, BuildOptions{
		SessionKey:  ev.SessionKey,
		UserMessage: promptInput,
	})
	if err != nil {
		return true, err
	}
	body, err := skills.LoadBody(skill.Path, 200000)
	if err != nil {
		return true, err
	}
	seed := fmt.Sprintf("Explicit skill requested: %s\nLocation: %s\nSource: %s\n\n%s", skill.Name, skill.Dir, skill.Source, body)
	seeded := make([]providers.ChatMessage, 0, len(messages)+1)
	if len(messages) > 0 {
		seeded = append(seeded, messages[0])
		seeded = append(seeded, providers.ChatMessage{Role: "system", Content: seed})
		seeded = append(seeded, messages[1:]...)
	} else {
		seeded = append(seeded, providers.ChatMessage{Role: "system", Content: seed})
		seeded = append(seeded, providers.ChatMessage{Role: "user", Content: promptInput})
	}
	runCtx := tools.ContextWithEnv(ctx, r.skillRunEnvFor(skill.Name))
	finalText, streamed, err := r.executeConversation(runCtx, ev.SessionKey, seeded, r.Tools, ev.Channel, replyTarget)
	if err != nil {
		return true, err
	}
	r.persistAssistantReply(ctx, ev.SessionKey, msgID, ev.Channel, replyTarget, finalText, streamed, shouldAutoDeliver(ev))
	return true, nil
}

func (r *Runtime) dispatchExplicitSkillTool(ctx context.Context, ev bus.Event, skill skills.SkillMeta, commandName, rawArgs string) string {
	if r.Tools == nil {
		return "tool registry not configured"
	}
	scopeKey := ev.SessionKey
	if r.DB != nil && strings.TrimSpace(ev.SessionKey) != "" {
		if resolved, err := r.DB.ResolveScopeKey(ctx, ev.SessionKey); err == nil && strings.TrimSpace(resolved) != "" {
			scopeKey = resolved
		}
	}
	toolCtx := tools.ContextWithSession(ctx, scopeKey)
	toolCtx = tools.ContextWithDelivery(toolCtx, ev.Channel, deliveryTarget(ev))
	toolCtx = tools.ContextWithEnv(toolCtx, r.skillRunEnvFor(skill.Name))
	params := map[string]any{
		"command":     rawArgs,
		"commandName": commandName,
		"skillName":   skill.Name,
	}
	out, err := r.Tools.ExecuteParams(toolCtx, skill.CommandTool, params)
	if err != nil {
		out = "tool error: " + err.Error()
	}
	payload := map[string]any{
		"tool":        skill.CommandTool,
		"skill":       skill.Name,
		"commandName": commandName,
		"args":        rawArgs,
	}
	sendOut, preview, artifactID := r.boundTextResult(ctx, ev.SessionKey, out)
	if artifactID != "" {
		payload["artifact_id"] = artifactID
		payload["preview"] = preview
	}
	if _, err := r.DB.AppendMessage(ctx, ev.SessionKey, "tool", sendOut, payload); err != nil {
		log.Printf("append tool message failed: %v", err)
	}
	return out
}

func parseSkillCommand(message string) (commandName string, rawArgs string, ok bool) {
	message = strings.TrimSpace(message)
	if !strings.HasPrefix(message, "/") || len(message) < 2 {
		return "", "", false
	}
	body := strings.TrimPrefix(message, "/")
	commandName, rawArgs, _ = strings.Cut(body, " ")
	commandName = strings.TrimSpace(commandName)
	rawArgs = strings.TrimSpace(rawArgs)
	if commandName == "" {
		return "", "", false
	}
	return commandName, rawArgs, true
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
	entry := r.acquireSessionLock(input.SessionKey)
	entry.mu.Lock()
	defer func() {
		entry.mu.Unlock()
		r.releaseSessionLock(input.SessionKey, entry)
	}()

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
	scopeKey := sessionKey
	if r.DB != nil && strings.TrimSpace(sessionKey) != "" {
		if resolved, err := r.DB.ResolveScopeKey(ctx, sessionKey); err == nil && strings.TrimSpace(resolved) != "" {
			scopeKey = resolved
		}
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

		var resp providers.ChatCompletionResponse
		var err error
		var sw channels.StreamWriter // lazily created on first text delta
		var swOnce sync.Once
		if r.Streamer != nil {
			resp, err = r.Provider.ChatStream(ctx, req, func(text string) {
				swOnce.Do(func() {
					w, beginErr := r.Streamer.BeginStream(ctx, replyTo, map[string]any{"channel": channel})
					if beginErr == nil {
						sw = w
					}
				})
				if sw != nil {
					_ = sw.WriteDelta(ctx, text)
				}
			})
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

		// Tool-call turn: close any partial stream that showed text.
		if sw != nil {
			_ = sw.Close(ctx, contentToString(msg.Content))
		}

		messages = append(messages, providers.ChatMessage{Role: "assistant", Content: msg.Content, ToolCalls: msg.ToolCalls})
		if _, err := r.DB.AppendMessage(ctx, sessionKey, "assistant", contentToString(msg.Content), map[string]any{"tool_calls": msg.ToolCalls}); err != nil {
			log.Printf("append assistant(tool_calls) failed: %v", err)
		}

		for _, tc := range msg.ToolCalls {
			toolCtx := tools.ContextWithSession(ctx, scopeKey)
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

func (r *Runtime) skillRunEnvFor(name string) map[string]string {
	if r.Builder == nil {
		return nil
	}
	return r.Builder.Skills.RunEnvForSkill(name)
}

func (r *Runtime) persistAssistantReply(ctx context.Context, sessionKey string, msgID int64, channel, replyTarget, finalText string, streamed bool, autoDeliver bool) {
	if strings.TrimSpace(finalText) == "" {
		finalText = "(no response)"
	}
	if _, err := r.DB.AppendMessage(ctx, sessionKey, "assistant", finalText, map[string]any{"in_reply_to": msgID}); err != nil {
		log.Printf("append assistant(final) failed: %v", err)
	}
	if autoDeliver && !streamed && r.Deliver != nil {
		if err := r.Deliver.Deliver(ctx, channel, replyTarget, finalText); err != nil {
			log.Printf("deliver failed: %v", err)
		}
	}
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

func isAutonomousEvent(eventType bus.EventType) bool {
	switch eventType {
	case bus.EventCron, bus.EventHeartbeat, bus.EventWebhook, bus.EventFileChange:
		return true
	default:
		return false
	}
}

func shouldAutoDeliver(ev bus.Event) bool {
	if ev.Type == bus.EventHeartbeat {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(ev.Channel), "email") {
		if len(ev.Meta) == 0 {
			return true
		}
		value, ok := ev.Meta["auto_reply_enabled"]
		if !ok {
			return true
		}
		switch cast := value.(type) {
		case bool:
			return cast
		default:
			return strings.EqualFold(strings.TrimSpace(fmt.Sprint(cast)), "true")
		}
	}
	return true
}

func releaseEvent(ev bus.Event) {
	if len(ev.Meta) == 0 {
		return
	}
	done, ok := ev.Meta[heartbeat.MetaKeyDone].(func())
	if !ok || done == nil {
		return
	}
	done()
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
