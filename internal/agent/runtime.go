// Package agent coordinates prompt building, tool execution, and turn delivery.
package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"or3-intern/internal/approval"
	"or3-intern/internal/artifacts"
	"or3-intern/internal/bus"
	"or3-intern/internal/channels"
	"or3-intern/internal/config"
	"or3-intern/internal/db"
	"or3-intern/internal/heartbeat"
	"or3-intern/internal/memory"
	"or3-intern/internal/providers"
	"or3-intern/internal/security"
	"or3-intern/internal/skills"
	"or3-intern/internal/tools"
)

const (
	commandNewSession = "/new"
	commandClear      = "/clear"
	commandStatus     = "/status"
	commandPrune      = "/prune"
)

const maxTrackedQuotaSessions = 1024

// Deliverer sends a completed response to a channel target.
type Deliverer interface {
	Deliver(ctx context.Context, channel, to, text string) error
}

// MetaDeliverer sends a completed response with channel-specific metadata.
type MetaDeliverer interface {
	DeliverWithMeta(ctx context.Context, channel, to, text string, meta map[string]any) error
}

// Runtime executes conversational turns against the configured model and tools.
type Runtime struct {
	DB                         *db.DB
	Provider                   *providers.Client
	Model                      string
	Temperature                float64
	SubagentProvider           *providers.Client
	SubagentModel              string
	Tools                      *tools.Registry
	Hardening                  config.HardeningConfig
	AccessProfiles             config.AccessProfilesConfig
	WorkspaceDir               string
	Builder                    *Builder
	Artifacts                  *artifacts.Store
	MaxToolBytes               int
	MaxToolLoops               int
	MaxToolLoopsExceededAction config.QuotaExceededAction
	ToolPreviewBytes           int
	DynamicToolExposure        bool
	EnforceActivePlan          bool
	Audit                      *security.AuditLogger
	ApprovalBroker             *approval.Broker
	ContextManager             config.ContextManagerConfig
	ContextManagerProvider     *providers.Client

	Deliver  Deliverer
	Streamer channels.StreamingChannel

	Consolidator                *memory.Consolidator
	ConsolidationScheduler      *memory.Scheduler
	DisableRollingConsolidation bool
	DefaultScopeKey             string
	LinkDirectMessages          bool
	IdentityScopeMap            map[string]string
	modelConfig                 atomic.Value

	locksMu     sync.Mutex
	locks       map[string]*sessionLock
	quotaMu     sync.Mutex
	quotas      map[string]*sessionQuotaState
	idleMu      sync.Mutex
	idleTimers  map[string]*time.Timer
	idleVersion map[string]uint64
}

// RuntimeModelConfig contains the model/provider choices that can be swapped for new turns.
type RuntimeModelConfig struct {
	Provider               *providers.Client
	Model                  string
	Temperature            float64
	SubagentProvider       *providers.Client
	SubagentModel          string
	ContextManagerProvider *providers.Client
	ContextManagerModel    string
}

func (r *Runtime) ApplyLiveModelConfig(cfg RuntimeModelConfig) {
	r.modelConfig.Store(cfg)
}

func (r *Runtime) CurrentModelConfig() RuntimeModelConfig {
	if stored := r.modelConfig.Load(); stored != nil {
		if cfg, ok := stored.(RuntimeModelConfig); ok {
			return cfg
		}
	}
	return RuntimeModelConfig{
		Provider:               r.Provider,
		Model:                  r.Model,
		Temperature:            r.Temperature,
		SubagentProvider:       r.SubagentProvider,
		SubagentModel:          r.SubagentModel,
		ContextManagerProvider: r.ContextManagerProvider,
		ContextManagerModel:    r.ContextManager.Model,
	}
}

func (r *Runtime) modelConfigForEvent(eventType bus.EventType) RuntimeModelConfig {
	cfg := r.CurrentModelConfig()
	if eventType == bus.EventSystem && cfg.SubagentProvider != nil {
		cfg.Provider = cfg.SubagentProvider
		if strings.TrimSpace(cfg.SubagentModel) != "" {
			cfg.Model = cfg.SubagentModel
		}
	}
	return cfg
}

// BackgroundRunInput describes an isolated subagent-style background run.
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

// BackgroundRunResult contains the final persisted outputs of a background run.
type BackgroundRunResult struct {
	FinalText  string
	Preview    string
	ArtifactID string
}

// Handle routes a published event into the runtime turn pipeline.
func (r *Runtime) Handle(ctx context.Context, ev bus.Event) error {
	ctx = ContextWithConversationSession(ctx, ev.SessionKey)
	ctx = r.contextWithEventProfile(ctx, ev)
	entry := r.acquireSessionLock(ev.SessionKey)
	entry.mu.Lock()
	defer func() {
		entry.mu.Unlock()
		r.releaseSessionLock(ev.SessionKey, entry)
	}()
	r.markSessionActivity(ev.SessionKey)
	switch ev.Type {
	case bus.EventUserMessage, bus.EventCron, bus.EventHeartbeat, bus.EventSystem, bus.EventWebhook, bus.EventFileChange:
		return r.turn(ctx, ev)
	default:
		return nil
	}
}

func (r *Runtime) turn(ctx context.Context, ev bus.Event) error {
	defer releaseEvent(ev)

	if handled, err := r.handleTurnCommand(ctx, ev); handled || err != nil {
		return err
	}
	r.ensureSessionScope(ctx, ev)

	msgID, err := r.DB.AppendMessage(ctx, ev.SessionKey, "user", ev.Message, map[string]any{
		"channel": ev.Channel, "from": ev.From, "meta": ev.Meta,
	})
	if err != nil {
		return err
	}
	defer r.syncExternalChannelChatSessionMeta(ctx, ev)
	if handled, err := r.handleTurnPreExecution(ctx, ev, msgID); handled || err != nil {
		if handled && err == nil {
			r.cleanupActiveTurnTask(ctx, ev.SessionKey)
		}
		return err
	}

	replyTarget := deliveryTarget(ev)
	replyMeta := channels.ReplyMeta(ev.Meta)
	if err := r.handleTurnExecution(ctx, ev, msgID, replyTarget, replyMeta); err != nil {
		return err
	}
	r.handleTurnPostCleanup(ctx, ev)
	return nil
}

func (r *Runtime) handleTurnCommand(ctx context.Context, ev bus.Event) (bool, error) {
	if ev.Type != bus.EventUserMessage {
		return false, nil
	}
	message := strings.TrimSpace(ev.Message)
	switch {
	case isNewSessionCommand(message):
		return true, r.handleNewSession(ctx, ev)
	case strings.EqualFold(message, commandStatus):
		r.ensureSessionScope(ctx, ev)
		return true, r.handleStatus(ctx, ev)
	case strings.EqualFold(message, commandPrune):
		r.ensureSessionScope(ctx, ev)
		return true, r.handlePruneSession(ctx, ev, "manual")
	default:
		return false, nil
	}
}

func (r *Runtime) handleTurnPreExecution(ctx context.Context, ev bus.Event, msgID int64) (bool, error) {
	if handled, err := r.handleExplicitSkillInvocation(ctx, ev, msgID); handled || err != nil {
		return handled, err
	}
	if handled, err := r.handleStructuredAutonomy(ctx, ev, msgID); handled || err != nil {
		return handled, err
	}
	if ev.Type == bus.EventUserMessage {
		r.ensureTaskCardForTurn(ctx, ev, msgID)
	}
	return false, nil
}

func (r *Runtime) handleTurnExecution(ctx context.Context, ev bus.Event, msgID int64, replyTarget string, replyMeta map[string]any) error {
	if r.Builder == nil {
		return fmt.Errorf("runtime builder not configured")
	}
	messages, err := r.BuildPromptSnapshotWithOptions(ctx, BuildOptions{
		SessionKey:      ev.SessionKey,
		UserMessage:     ev.Message,
		UserMessageID:   msgID,
		TurnAttachments: chatAttachmentsFromMeta(ev.Meta),
		Autonomous:      isAutonomousEvent(ev.Type),
		EventMeta:       cloneMap(ev.Meta),
	})
	if err != nil {
		return err
	}
	turnCtx := ContextWithTurnState(ctx, TurnState{
		SessionKey:    ev.SessionKey,
		UserMessageID: msgID,
		UserMessage:   ev.Message,
	})
	finalText, streamed, err := r.executeConversation(turnCtx, ev.Type, ev.SessionKey, messages, r.effectiveTools(turnCtx, r.Tools), ev.Channel, replyTarget, replyMeta)
	if err != nil {
		var approvalErr *tools.ApprovalRequiredError
		if errors.As(err, &approvalErr) && strings.TrimSpace(finalText) != "" {
			r.persistAssistantReply(ctx, ev.SessionKey, msgID, ev.Channel, replyTarget, finalText, replyMeta, streamed, shouldAutoDeliver(ev))
		}
		return err
	}
	r.persistAssistantReply(ctx, ev.SessionKey, msgID, ev.Channel, replyTarget, finalText, replyMeta, streamed, shouldAutoDeliver(ev))
	return nil
}

func (r *Runtime) handleTurnPostCleanup(ctx context.Context, ev bus.Event) {
	r.cleanupActiveTurnTask(ctx, ev.SessionKey)
	if !r.DisableRollingConsolidation && r.Consolidator != nil && r.Builder != nil && r.ConsolidationScheduler != nil {
		r.ConsolidationScheduler.Trigger(ev.SessionKey)
	} else if !r.DisableRollingConsolidation && r.Consolidator != nil && r.Builder != nil {
		historyMax := r.Builder.HistoryMax
		if historyMax <= 0 {
			historyMax = 40
		}
		if err := r.Consolidator.MaybeConsolidate(ctx, ev.SessionKey, historyMax); err != nil {
			log.Printf("consolidation failed: session=%s err=%v", ev.SessionKey, err)
		}
	}
	r.scheduleIdlePrune(ctx, ev)
}

func isNewSessionCommand(message string) bool {
	message = strings.TrimSpace(message)
	return strings.EqualFold(message, commandNewSession) || strings.EqualFold(message, commandClear)
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
	replyMeta := channels.ReplyMeta(ev.Meta)
	skill, found := r.Builder.Skills.Get(commandName)
	if !found {
		return false, nil
	}
	if !skill.UserInvocable {
		r.persistAssistantReply(ctx, ev.SessionKey, msgID, ev.Channel, replyTarget, "skill is not user-invocable: "+skill.Name, replyMeta, false, shouldAutoDeliver(ev))
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
		r.persistAssistantReply(ctx, ev.SessionKey, msgID, ev.Channel, replyTarget, message, replyMeta, false, shouldAutoDeliver(ev))
		return true, nil
	}
	if skill.CommandDispatch == "tool" {
		text := r.dispatchExplicitSkillTool(ctx, ev, skill, commandName, rawArgs)
		r.persistAssistantReply(ctx, ev.SessionKey, msgID, ev.Channel, replyTarget, text, replyMeta, false, shouldAutoDeliver(ev))
		return true, nil
	}

	promptInput := strings.TrimSpace(rawArgs)
	if promptInput == "" {
		promptInput = skill.Name
	}
	messages, err := r.BuildPromptSnapshotWithOptions(ctx, BuildOptions{
		SessionKey:  ev.SessionKey,
		UserMessage: promptInput,
		EventMeta:   cloneMap(ev.Meta),
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
	runCtx = tools.ContextWithSkillPolicy(runCtx, skillPolicyForSkill(skill))
	finalText, streamed, err := r.executeConversation(runCtx, ev.Type, ev.SessionKey, seeded, r.effectiveTools(runCtx, r.Tools), ev.Channel, replyTarget, replyMeta)
	if err != nil {
		return true, err
	}
	r.persistAssistantReply(ctx, ev.SessionKey, msgID, ev.Channel, replyTarget, finalText, replyMeta, streamed, shouldAutoDeliver(ev))
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
	toolCtx = tools.ContextWithDeliveryMeta(toolCtx, channels.ReplyMeta(ev.Meta))
	toolCtx = tools.ContextWithEnv(toolCtx, r.skillRunEnvFor(skill.Name))
	toolCtx = tools.ContextWithSkillPolicy(toolCtx, skillPolicyForSkill(skill))
	toolCtx = r.contextWithTrustedToolAccess(toolCtx, ev)
	toolCtx = tools.ContextWithToolGuard(toolCtx, r.guardToolExecution)
	params := map[string]any{
		"command":     rawArgs,
		"commandName": commandName,
		"skillName":   skill.Name,
	}
	out, err := r.Tools.ExecuteParams(toolCtx, skill.CommandTool, params)
	if err != nil {
		out = formatToolExecutionError(skill.CommandTool, params, out, err)
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

// BuildPromptSnapshot builds the prompt snapshot for a normal user turn.
func (r *Runtime) BuildPromptSnapshot(ctx context.Context, sessionKey string, userMessage string) ([]providers.ChatMessage, error) {
	if r.Builder == nil {
		return nil, fmt.Errorf("runtime builder not configured")
	}
	pp, _, err := r.Builder.Build(ctx, sessionKey, userMessage)
	if err != nil {
		return nil, err
	}
	messages := promptSnapshotSystemMessages(ctx, pp.System)
	messages = append(messages, pp.History...)
	if len(pp.History) == 0 || pp.History[len(pp.History)-1].Role != "user" {
		messages = append(messages, providers.ChatMessage{Role: "user", Content: userMessage})
	}
	return messages, nil
}

// BuildPromptSnapshotWithOptions builds a prompt snapshot with explicit turn options.
func (r *Runtime) BuildPromptSnapshotWithOptions(ctx context.Context, opts BuildOptions) ([]providers.ChatMessage, error) {
	if r.Builder == nil {
		return nil, fmt.Errorf("runtime builder not configured")
	}
	pp, _, err := r.Builder.BuildWithOptions(ctx, opts)
	if err != nil {
		return nil, err
	}
	messages := promptSnapshotSystemMessages(ctx, pp.System)
	messages = append(messages, pp.History...)
	if len(pp.History) == 0 || pp.History[len(pp.History)-1].Role != "user" {
		visionBudget := newVisionBudget()
		var content any = opts.UserMessage
		if r.Builder != nil {
			atts := mergeTurnAttachments(opts.TurnAttachments, opts.EventMeta)
			content = r.Builder.buildUserContent(ctx, opts.UserMessage, chatAttachmentsToArtifactAttachments(atts), visionBudget)
		}
		messages = append(messages, providers.ChatMessage{Role: "user", Content: content})
	}
	return messages, nil
}

func promptSnapshotSystemMessages(ctx context.Context, bootstrap []providers.ChatMessage) []providers.ChatMessage {
	if prompt := trustedSystemPromptFromContext(ctx); prompt != "" {
		return []providers.ChatMessage{{Role: "system", Content: prompt}}
	}
	return append([]providers.ChatMessage{}, bootstrap...)
}

// RunBackground executes a background task without auto-persisting a user event.
func (r *Runtime) RunBackground(ctx context.Context, input BackgroundRunInput) (BackgroundRunResult, error) {
	ctx = r.contextWithProfileName(ctx, strings.TrimSpace(fmt.Sprint(input.Meta["profile_name"])))
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
		reg = r.effectiveTools(ctx, r.Tools)
	}
	finalText, _, err := r.executeConversation(ctx, bus.EventSystem, input.SessionKey, append([]providers.ChatMessage{}, input.PromptSnapshot...), reg, input.Channel, input.ReplyTo, nil)
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

func (r *Runtime) pruneSessionContext(ctx context.Context, sessionKey, reason string) (string, error) {
	if r.DB == nil {
		return "", fmt.Errorf("runtime database not configured")
	}
	if strings.TrimSpace(sessionKey) == "" {
		return "", fmt.Errorf("session key required")
	}
	if r.ContextManager.Enabled {
		return r.compactSessionContextWithModel(ctx, sessionKey, reason)
	}
	if r.Consolidator == nil || r.Consolidator.Provider == nil {
		return "", fmt.Errorf("consolidation is not configured, so pruning would discard unsummarized history")
	}
	historyMax := 40
	if r.Builder != nil && r.Builder.HistoryMax > 0 {
		historyMax = r.Builder.HistoryMax
	}
	if err := r.Consolidator.ArchiveResetWindow(ctx, sessionKey, historyMax); err != nil {
		return "", err
	}
	if err := r.DB.ResetSessionHistory(ctx, sessionKey); err != nil {
		return "", err
	}
	label := strings.TrimSpace(reason)
	if label == "" {
		label = "manual"
	}
	return "Context pruned (" + label + "). Recent chat was archived into memory and the live context window was cleared.", nil
}

func (r *Runtime) compactSessionContextWithModel(ctx context.Context, sessionKey, reason string) (string, error) {
	provider := r.contextManagerProvider()
	if provider == nil {
		return "", fmt.Errorf("context manager provider not configured")
	}
	scopeKey := sessionKey
	if resolved, err := r.DB.ResolveScopeKey(ctx, sessionKey); err == nil && strings.TrimSpace(resolved) != "" {
		scopeKey = resolved
	}
	input, existingCutoff, err := r.buildContextManagerPruneInput(ctx, sessionKey, scopeKey, reason)
	if err != nil {
		return "", err
	}
	if len(input.Messages) == 0 {
		return "Context prune skipped. There are no live messages to compact.", nil
	}
	proposal, err := requestContextManagerCompaction(ctx, provider, r.contextManagerModel(), input)
	if err != nil {
		return "", err
	}
	policy := ContextManagerPolicy{Enabled: true, AllowTaskUpdates: r.ContextManager.AllowTaskUpdates, AllowStalePropose: r.ContextManager.AllowStalePropose, ScopeKey: scopeKey}
	if err := validateContextManagerProposal(proposal, policy); err != nil {
		return "", err
	}
	if proposal.Compaction == nil || proposal.Compaction.CompactThroughMessageID <= existingCutoff {
		return "Context prune skipped. The model found no additional context that was safe to compact.", nil
	}
	protectedMin := int64(0)
	if card, ok, err := loadTaskCard(ctx, r.DB, sessionKey); err == nil && ok {
		protectedMin = protectedCompactionMinMessageID(card.Metadata, 0)
	}
	resolvedCutoff, cutoffAdjusted, err := enforceProtectedCompactionCutoff(proposal.Compaction.CompactThroughMessageID, input.Messages, protectedMin)
	if err != nil {
		return "", err
	}
	refs, _ := json.Marshal(proposal.Compaction.Refs)
	if err := r.DB.UpsertContextCompaction(ctx, db.ContextCompaction{
		ScopeKey:        scopeKey,
		SessionKey:      sessionKey,
		Summary:         proposal.Compaction.Summary,
		CutoffMessageID: resolvedCutoff,
		MessageRefsJSON: string(refs),
	}); err != nil {
		return "", err
	}
	if proposal.TaskCardUpdates != nil && r.ContextManager.AllowTaskUpdates {
		card, _, _ := loadTaskCard(ctx, r.DB, sessionKey)
		card = applyContextManagerTaskUpdate(card, *proposal.TaskCardUpdates)
		if err := saveTaskCard(ctx, r.DB, sessionKey, scopeKey, card); err != nil {
			log.Printf("context manager task-card update failed: %v", err)
		}
	}
	message := fmt.Sprintf("Context pruned (%s). The model compacted live prompt history through message %d; raw chat history was preserved.", nonEmptyLabel(reason, "manual"), resolvedCutoff)
	if cutoffAdjusted {
		message += " The requested cutoff was normalized to the nearest valid message in the provided context window."
	}
	return message, nil
}

func (r *Runtime) buildContextManagerPruneInput(ctx context.Context, sessionKey, scopeKey, reason string) (contextManagerPruneInput, int64, error) {
	historyMax := 40
	if r.Builder != nil && r.Builder.HistoryMax > 0 {
		historyMax = r.Builder.HistoryMax
	}
	limit := historyMax * 2
	if limit < 40 {
		limit = 40
	}
	if limit > 120 {
		limit = 120
	}
	rows, err := r.DB.GetLastMessagesScoped(ctx, sessionKey, limit)
	if err != nil {
		return contextManagerPruneInput{}, 0, err
	}
	var existing db.ContextCompaction
	var existingCutoff int64
	if row, ok, err := r.DB.GetContextCompaction(ctx, scopeKey); err == nil && ok {
		existing = row
		existingCutoff = row.CutoffMessageID
	}
	maxChars := r.ContextManager.MaxInputTokens * 4
	if maxChars <= 0 {
		maxChars = 12000
	}
	messages := make([]contextManagerMessage, 0, len(rows))
	used := 0
	for _, row := range rows {
		if row.ID <= existingCutoff {
			continue
		}
		content := oneLine(row.Content, 1200)
		entryCost := len(content) + 80
		if used+entryCost > maxChars && len(messages) > 0 {
			break
		}
		messages = append(messages, contextManagerMessage{ID: row.ID, SessionKey: row.SessionKey, Role: row.Role, Content: content, CreatedAt: row.CreatedAt})
		used += entryCost
	}
	taskCardText := ""
	if r.Builder == nil || !r.Builder.DisableTaskCard {
		if card, ok, err := loadTaskCard(ctx, r.DB, sessionKey); err == nil && ok {
			taskCardText = renderTaskCard(card, 3000)
		}
	}
	return contextManagerPruneInput{
		Reason:          nonEmptyLabel(reason, "manual"),
		SessionKey:      sessionKey,
		ScopeKey:        scopeKey,
		ExistingSummary: existing.Summary,
		TaskCard:        taskCardText,
		Messages:        messages,
	}, existingCutoff, nil
}

func (r *Runtime) contextManagerProvider() *providers.Client {
	if cfg := r.CurrentModelConfig(); cfg.ContextManagerProvider != nil {
		return cfg.ContextManagerProvider
	}
	if r.ContextManagerProvider != nil {
		return r.ContextManagerProvider
	}
	if r.Consolidator != nil && r.Consolidator.Provider != nil {
		return r.Consolidator.Provider
	}
	return r.Provider
}

func (r *Runtime) contextManagerModel() string {
	if model := strings.TrimSpace(r.CurrentModelConfig().ContextManagerModel); model != "" {
		return model
	}
	if model := strings.TrimSpace(r.ContextManager.Model); model != "" {
		return model
	}
	if r.Consolidator != nil && strings.TrimSpace(r.Consolidator.ChatModel) != "" {
		return r.Consolidator.ChatModel
	}
	return r.Model
}

func normalizeCompactionCutoff(cutoff int64, messages []contextManagerMessage) (int64, bool, error) {
	if cutoff <= 0 {
		return 0, false, nil
	}
	var nearestLower int64
	for _, msg := range messages {
		if msg.ID == cutoff {
			return cutoff, false, nil
		}
		if msg.ID < cutoff && msg.ID > nearestLower {
			nearestLower = msg.ID
		}
	}
	if nearestLower > 0 {
		return nearestLower, true, nil
	}
	return 0, false, fmt.Errorf("compaction cutoff %d was not in the provided context window", cutoff)
}

func enforceProtectedCompactionCutoff(cutoff int64, messages []contextManagerMessage, protectedMinMessageID int64) (int64, bool, error) {
	resolved, adjusted, err := normalizeCompactionCutoff(cutoff, messages)
	if err != nil {
		return 0, false, err
	}
	if protectedMinMessageID <= 0 {
		return resolved, adjusted, nil
	}
	maxSafe := protectedMinMessageID - 1
	if maxSafe <= 0 {
		return 0, true, nil
	}
	if resolved <= maxSafe {
		return resolved, adjusted, nil
	}
	clamped, clampAdjusted, err := normalizeCompactionCutoff(maxSafe, messages)
	if err != nil {
		return 0, false, fmt.Errorf("compaction cutoff %d would remove protected turn/plan context", cutoff)
	}
	return clamped, adjusted || clampAdjusted, nil
}

func nonEmptyLabel(value, fallbackValue string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallbackValue
	}
	return value
}

func (r *Runtime) ensureTaskCardForTurn(ctx context.Context, ev bus.Event, msgID int64) {
	if r.DB == nil || r.Builder == nil || r.Builder.DisableTaskCard || strings.TrimSpace(ev.SessionKey) == "" {
		return
	}
	message := strings.TrimSpace(ev.Message)
	if message == "" || strings.HasPrefix(message, "/") {
		return
	}
	scopeKey := ev.SessionKey
	if resolved, err := r.DB.ResolveScopeKey(ctx, ev.SessionKey); err == nil && strings.TrimSpace(resolved) != "" {
		scopeKey = resolved
	}
	card, ok, _ := loadTaskCard(ctx, r.DB, ev.SessionKey)
	if !ok || strings.TrimSpace(card.Goal) == "" {
		card.Goal = oneLine(message, 240)
	}
	maybeReplaceStaleActivePlan(&card.Metadata, message, msgID)
	card.Metadata.CurrentRequest = oneLine(message, maxPlanNoteChars)
	if msgID > 0 {
		card.Metadata.CurrentRequestMessageID = msgID
	}
	card.Status = "active"
	if err := saveTaskCard(ctx, r.DB, ev.SessionKey, scopeKey, card); err != nil {
		log.Printf("save task card before prompt failed: %v", err)
	}
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

func (r *Runtime) toolPreviewBytes() int {
	if r.ToolPreviewBytes <= 0 {
		if r.MaxToolBytes > 0 {
			return r.MaxToolBytes
		}
		return config.DefaultMaxToolBytes
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

func (r *Runtime) syncExternalChannelChatSessionMeta(ctx context.Context, ev bus.Event) {
	if r.DB == nil || strings.TrimSpace(ev.SessionKey) == "" || !isExternalUserChannel(ev.Channel) {
		return
	}
	if err := r.DB.SyncChatSessionMessageSummary(ctx, ev.SessionKey, externalChannelSessionTitle(ev), "or3-intern", "OR3 Intern"); err != nil {
		log.Printf("sync external chat session metadata failed: session=%s channel=%s err=%v", ev.SessionKey, ev.Channel, err)
	}
}

func isExternalUserChannel(channel string) bool {
	switch strings.ToLower(strings.TrimSpace(channel)) {
	case "telegram", "discord", "slack", "whatsapp", "email":
		return true
	default:
		return false
	}
}

func externalChannelSessionTitle(ev bus.Event) string {
	channel := strings.ToLower(strings.TrimSpace(ev.Channel))
	target := deliveryTarget(ev)
	if target == "" {
		target = strings.TrimSpace(ev.From)
	}
	if target == "" {
		target = strings.TrimSpace(ev.SessionKey)
	}
	switch channel {
	case "telegram":
		return "Telegram " + target
	case "discord":
		return "Discord " + target
	case "slack":
		return "Slack " + target
	case "whatsapp":
		return "WhatsApp " + target
	case "email":
		return "Email " + target
	default:
		return strings.TrimSpace(ev.SessionKey)
	}
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

// WithTimeout derives a timeout context when sec is positive.
func WithTimeout(ctx context.Context, sec int) (context.Context, context.CancelFunc) {
	if sec <= 0 {
		sec = 60
	}
	return context.WithTimeout(ctx, time.Duration(sec)*time.Second)
}
