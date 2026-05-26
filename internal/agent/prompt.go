package agent

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"or3-intern/internal/artifacts"
	"or3-intern/internal/db"
	"or3-intern/internal/heartbeat"
	"or3-intern/internal/memory"
	"or3-intern/internal/providers"
	"or3-intern/internal/scope"
	"or3-intern/internal/skills"
	"or3-intern/internal/triggers"
)

type retrievedMemoryLine struct{ memory.Retrieved }

func (m retrievedMemoryLine) memoryKind() string { return m.Kind }
func (m retrievedMemoryLine) memoryID() int64    { return m.ID }
func (m retrievedMemoryLine) memoryText() string { return m.Text }
func (m retrievedMemoryLine) memoryRef() string  { return m.Ref }

const DefaultSoul = `# Soul
I am or3-intern, a personal AI assistant.
- Be clear, direct, and practical.
- Prefer bounded, deterministic work over broad guessing.
- Use tools when current facts, files, or exact outputs matter.
- Keep answers concise unless the task needs detail.
`

const DefaultAgentInstructions = `# Agent Instructions
Basic loop:
1. Restate the task internally in one sentence.
2. Check the most reliable context first.
3. If facts, files, dates, APIs, or outputs matter, use tools before deciding.
4. Make the smallest useful change or answer.
5. Report what changed, what was verified, and any real blocker.

Context rules:
- Current user request is primary. Use older context only when relevant.
- Reliability order: Pinned Memory > stable local instruction files > recent conversation > Memory Digest > Retrieved Memory > Workspace/Indexed excerpts.
- Pinned Memory is durable. Retrieved Memory and file excerpts may be stale or partial.
- Verify stale/partial context before using it for code, dates, APIs, paths, or settled decisions.

Work rules:
- Before editing code, inspect the relevant files and follow existing patterns.
- Keep changes scoped to the request. Avoid unrelated refactors.
- If information is missing, gather it with tools. Do not invent facts.
- Large outputs live behind previews/artifact IDs; request the exact range, search result, or artifact content needed.
`

const DefaultToolNotes = `# Tool Usage Notes
Prefer the narrowest, lowest-capability tool or mode that will answer the question. If preview, range, grep, outline, or search is enough, use that before broader reads or writes.

Files:
- Use list_dir before reading a directory or when you need to discover what files exist.
- For unknown files, first use search_file or read_file mode=outline.
- Use read_file mode=grep to find matching lines.
- Use read_file mode=range for exact code sections.
- Use preview only for small files or quick orientation.
- Use read_file mode=full when you need the whole bounded file; it is a safe read-only operation.
- Use edit_file for focused changes to existing files. Use write_file when replacing or creating the whole file intentionally.
- Use read_artifact when another tool returns an artifact_id instead of trying to repeat the original broad operation.
- Use read_skill mode=outline first. Read full skill content only when the outline is not enough.
- If a skill references a prerequisite or shared skill, read that prerequisite before executing commands from the skill.
- Treat shell examples inside skills/docs as syntax references to adapt to the active tool schema, not as literal payloads to copy unchanged.

Tool results:
- Read summary and stats first.
- Treat preview as partial.
- If artifact_id exists, use it only when the missing detail is actually needed.
- Prefer narrower follow-up reads over asking for huge output.
- The advertised tool schemas for this turn are the authority. Do not assume hidden tools or higher-capability modes are available.

memory:
- Use memory_recent for recent conversation context.
- Use memory_get_pinned for durable session/global facts that should already matter.
- Use memory_search for semantic recall when you have a topic or fact to recover.
- Use memory_add_note only for durable facts, decisions, or lessons worth retrieving later; not for temporary scratch notes.
- Use memory_set_pinned only for facts that should consistently appear in future prompts.

web:
- Use web_search to discover candidate URLs when you do not already have one.
- Use web_fetch as the default way to read a specific URL.
- Use web_fetch_markdown only when you specifically need explicit HTML-to-Markdown artifact control.
- web_fetch automatically converts HTML pages into Markdown artifacts to avoid dumping raw HTML into context; use raw=true only when literal response bytes are required.
- Use render=true for JavaScript-heavy pages.
- Use selector or waitMs when the important content loads late.

exec:
- Prefer program + args over shell command strings.
- When a skill or doc shows a CLI like gws tasks tasklists list --format table, call exec with program "gws" and args ["tasks", "tasklists", "list", "--format", "table"].
- Do not send both exec program and a full shell command unless the tool schema explicitly requires it; a non-empty command field changes approval and shell policy semantics.
- Commands have timeouts, policy checks, and bounded output.
- Output is previewed. If output is too broad, rerun with a narrower command.
- Omit cwd unless you need a subdirectory; when cwd is set, keep it inside the stated working directory/workspace.
- Use run_skill for approved skills when a skill actually needs code execution; prefer read_skill first.
- run_skill freezes a plan before approval. After approval, either retry the same arguments or resume with the returned plan_id instead of inventing a different tool call.
- If exec returns approval required, retry the identical executable and argv after approval. Dropping or changing args asks approval for a different command.
- If a skill describes CLI commands but no exec/script tool is advertised, do not guess files or hidden scripts; state that execution is unavailable in this turn.

messaging/subagents:
- Use send_message only when delivery is part of the task, especially for reminders, scheduled follow-ups, or proactive outbound updates.
- Use reply_in_thread only when you want to reuse the current thread/target.
- Use spawn_subagent for longer background work or parallelizable tasks; do not use it for quick synchronous steps you can do directly.

cron:
- Use cron only for scheduled reminders or recurring tasks, and only when cron is advertised as an available tool.
- Do not use cron for work that should happen immediately in the current turn.

Tool names:
- Use the advertised tool names exactly as shown.
- Do not invent generic tool names like memory, files, browser, or shell when specific built-ins exist.
`

// defaultDigestLineMax bounds the number of lines in the Memory Digest section.
const defaultDigestLineMax = 10

const (
	defaultBootstrapMaxChars      = 20000
	defaultBootstrapTotalMaxChars = 150000
	defaultPinnedOneLineMax       = 220
	defaultDigestOneLineMax       = 160
	defaultRetrievedOneLineMax    = 240
	defaultSkillsSummaryMax       = 80
	defaultVisionMaxImages        = 4
	defaultVisionMaxImageBytes    = 4 << 20
	defaultVisionTotalBytes       = 8 << 20
	embedCacheTTL                 = 5 * time.Minute
	embedCacheMaxEntries          = 128
)

type embedCacheKey struct {
	fingerprint string
	model       string
	input       string
}

type embedCacheEntry struct {
	vec       []float32
	expiresAt time.Time
	usedAt    time.Time
}

var promptEmbedCache = struct {
	mu      sync.Mutex
	entries map[embedCacheKey]embedCacheEntry
}{entries: map[embedCacheKey]embedCacheEntry{}}

type PromptParts struct {
	System  []providers.ChatMessage
	History []providers.ChatMessage
	Budget  BudgetReport
}

// BuildOptions holds options for building a prompt.
type BuildOptions struct {
	SessionKey      string
	UserMessage     string
	UserMessageID   int64
	TurnAttachments []ChatAttachment
	Autonomous      bool // true for cron/webhook/file-change events
	EventMeta       map[string]any
}

type turnPromptInput struct {
	pinnedText, digestText, memText, identityText, staticMemoryText string
	docContextText, workspaceContextText                            string
	heartbeatText, compactionText, activePlanText, eventContextText string
	toolPolicyMode                                                  string
	currentUserMessage                                              string
	currentUserMessageID                                            int64
	turnAttachments                                                 []ChatAttachment
	recentExecutionText                                             string
}

type Builder struct {
	DB               *db.DB
	Artifacts        *artifacts.Store
	Skills           skills.Inventory
	Mem              *memory.Retriever
	Provider         *providers.Client
	EmbedModel       string
	EmbedFingerprint string
	EnableVision     bool

	Soul                   string
	AgentInstructions      string
	ToolNotes              string
	BootstrapMaxChars      int
	BootstrapTotalMaxChars int
	SkillsSummaryMax       int

	HistoryMax int
	VectorK    int
	FTSK       int
	TopK       int

	// New fields for lightweight OpenClaw parity
	IdentityText       string               // content of IDENTITY.md
	StaticMemory       string               // content of MEMORY.md
	HeartbeatText      string               // content of HEARTBEAT.md – injected only for autonomous turns
	HeartbeatTasksFile string               // configured heartbeat file path for per-turn refresh
	DocRetriever       *memory.DocRetriever // for indexed file context
	DocRetrieveLimit   int                  // max docs to retrieve
	WorkspaceDir       string

	ContextMaxInputTokens      int
	ContextOutputReserveTokens int
	ContextSafetyMarginTokens  int
	ContextSectionBudgets      ContextSectionBudgets
	DisableTaskCard            bool
}

// Build builds a prompt snapshot. It is a convenience wrapper around BuildWithOptions.
func (b *Builder) Build(ctx context.Context, sessionKey string, userMessage string) (PromptParts, []memory.Retrieved, error) {
	return b.BuildWithOptions(ctx, BuildOptions{SessionKey: sessionKey, UserMessage: userMessage})
}

// BuildWithOptions builds a prompt snapshot using the provided options.
func (b *Builder) BuildWithOptions(ctx context.Context, opts BuildOptions) (PromptParts, []memory.Retrieved, error) {
	packet, retrieved, err := b.buildPacket(ctx, opts)
	if err != nil {
		return PromptParts{}, nil, err
	}
	sys := renderProviderMessages(&packet, b)
	return PromptParts{
		System:  sys,
		History: packet.RecentHistory,
		Budget:  packet.Budget,
	}, retrieved, nil
}

func (b *Builder) buildPacket(ctx context.Context, opts BuildOptions) (ContextPacket, []memory.Retrieved, error) {
	scopeKey := opts.SessionKey
	if b.DB != nil && strings.TrimSpace(opts.SessionKey) != "" {
		if resolved, err := b.DB.ResolveScopeKey(ctx, opts.SessionKey); err == nil && strings.TrimSpace(resolved) != "" {
			scopeKey = resolved
		}
	}
	pinned, err := b.DB.GetPinned(ctx, scopeKey)
	if err != nil {
		return ContextPacket{}, nil, err
	}
	pinnedText := formatPinned(pinned)

	structuredMax := b.BootstrapMaxChars
	if structuredMax <= 0 {
		structuredMax = defaultBootstrapMaxChars
	}
	var activePlanMeta ActivePlanMetadata
	var taskCard TaskCard
	var hasTaskCard bool
	taskCardText := ""
	if b.DB != nil && !b.DisableTaskCard {
		if card, ok, err := loadTaskCard(ctx, b.DB, opts.SessionKey); err == nil && ok {
			hasTaskCard = true
			taskCard = card
			activePlanMeta = card.Metadata
			if activePlanIsEstablished(activePlanMeta) {
				taskCardText = renderActivePlanCompact(card, activePlanMeta, structuredMax)
			} else {
				taskCardText = renderTaskCard(card, structuredMax)
			}
		}
	}
	turnAttachments := mergeTurnAttachments(opts.TurnAttachments, opts.EventMeta)
	recentExecutionText := b.buildRecentExecutionState(ctx, opts.SessionKey, activePlanMeta)
	compactionText := ""
	var compactionCutoff int64
	if b.DB != nil {
		if compaction, ok, err := b.DB.GetContextCompaction(ctx, scopeKey); err == nil && ok {
			compactionCutoff = compaction.CutoffMessageID
			compactionText = renderContextCompaction(compaction, structuredMax)
		}
	}

	// embed and retrieve
	var retrieved []memory.Retrieved
	var rejected []string
	if b.Mem != nil && strings.TrimSpace(opts.UserMessage) != "" {
		var queryVec []float32
		if b.Provider != nil {
			vec, embedErr := cachedEmbed(ctx, b.Provider, b.EmbedFingerprint, b.EmbedModel, opts.UserMessage)
			if embedErr != nil {
				log.Printf("memory embed failed for scope %q, using FTS fallback: %v", scopeKey, embedErr)
			} else {
				queryVec = vec
			}
		}
		mem := *b.Mem
		mem.EmbedFingerprint = b.EmbedFingerprint
		mem.TaskContext = taskCardText
		var err error
		retrieved, err = mem.Retrieve(ctx, scopeKey, opts.UserMessage, queryVec, b.VectorK, b.FTSK, b.TopK)
		if err != nil {
			log.Printf("memory retrieve failed for scope %q: %v", scopeKey, err)
			retrieved = nil
		}
		rejected = append(rejected, mem.LastRejected...)
	}
	maxEach := b.BootstrapMaxChars
	if maxEach <= 0 {
		maxEach = defaultBootstrapMaxChars
	}
	memText, retrievedIDs := formatRetrievedBounded(retrieved, maxEach)

	// Build Memory Digest from top active durable-kind notes.
	digestText, digestIDs := formatMemoryDigestBounded(retrieved, defaultDigestLineMax, maxEach)

	// Best-effort usage logging for notes that made it into the prompt.
	if b.DB != nil {
		ids := append([]int64(nil), retrievedIDs...)
		ids = append(ids, digestIDs...)
		ids = uniqueInt64(ids)
		if len(ids) > 0 {
			_ = b.DB.TouchMemoryNotes(ctx, scopeKey, ids, db.NowMS())
		}
	}

	// indexed doc context
	var docContextText string
	if b.DocRetriever != nil && strings.TrimSpace(opts.UserMessage) != "" {
		limit := b.DocRetrieveLimit
		if limit <= 0 {
			limit = 5
		}
		docs, docErr := b.DocRetriever.RetrieveDocs(ctx, scope.GlobalMemoryScope, opts.UserMessage, limit)
		if docErr != nil {
			log.Printf("indexed doc retrieval failed: %v", docErr)
		}
		if len(docs) > 0 {
			var sb strings.Builder
			for i, d := range docs {
				sb.WriteString(fmt.Sprintf("%d) [%s] %s\n", i+1, d.Path, d.Excerpt))
			}
			docContextText = strings.TrimSpace(sb.String())
		}
	}
	workspaceContextText := memory.BuildWorkspaceContext(memory.WorkspaceContextConfig{
		WorkspaceDir: b.WorkspaceDir,
	}, opts.UserMessage)

	histRows, err := b.DB.GetLastMessagesScoped(ctx, opts.SessionKey, b.HistoryMax)
	if err != nil {
		return ContextPacket{}, nil, err
	}
	if compactionCutoff > 0 {
		filtered := histRows[:0]
		for _, row := range histRows {
			if row.ID > compactionCutoff {
				filtered = append(filtered, row)
			}
		}
		histRows = filtered
	}
	if opts.UserMessageID > 0 {
		filtered := histRows[:0]
		for _, row := range histRows {
			if row.ID != opts.UserMessageID {
				filtered = append(filtered, row)
			}
		}
		histRows = filtered
	}
	visionBudget := newVisionBudget()
	hist := make([]providers.ChatMessage, 0, len(histRows))
	pendingToolCallIDs := make([]string, 0)
	for _, m := range histRows {
		msg := providers.ChatMessage{Role: m.Role, Content: m.Content}
		var payload map[string]any
		if err := json.Unmarshal([]byte(m.PayloadJSON), &payload); err == nil {
			if m.Role == "assistant" {
				if raw, ok := payload["tool_calls"]; ok {
					if tcs := toolCallsFromPayload(raw); len(tcs) > 0 {
						msg.ToolCalls = tcs
						pendingToolCallIDs = pendingToolCallIDs[:0]
						for _, tc := range tcs {
							if id := strings.TrimSpace(tc.ID); id != "" {
								pendingToolCallIDs = append(pendingToolCallIDs, id)
							}
						}
					}
				}
			}
			if m.Role == "tool" {
				if rawID, ok := payload["tool_call_id"]; ok {
					msg.ToolCallID = strings.TrimSpace(fmt.Sprint(rawID))
				}
				if msg.ToolCallID == "" && len(pendingToolCallIDs) > 0 {
					msg.ToolCallID = pendingToolCallIDs[0]
				}
				if msg.ToolCallID != "" && len(pendingToolCallIDs) > 0 {
					if pendingToolCallIDs[0] == msg.ToolCallID {
						pendingToolCallIDs = pendingToolCallIDs[1:]
					} else {
						for i, id := range pendingToolCallIDs {
							if id == msg.ToolCallID {
								pendingToolCallIDs = append(pendingToolCallIDs[:i], pendingToolCallIDs[i+1:]...)
								break
							}
						}
					}
				}
			}
			if m.Role == "user" {
				msg.Content = b.buildUserContent(ctx, m.Content, attachmentsFromPayload(payload), visionBudget)
			}
		}
		hist = append(hist, msg)
	}

	heartbeat := ""
	eventContext := ""
	if opts.Autonomous {
		heartbeat = b.currentHeartbeatText()
		eventContext = formatStructuredEventContext(opts.EventMeta, structuredMax)
	}
	activePlanText := ""
	if hasTaskCard && activePlanIsEstablished(activePlanMeta) {
		activePlanText = renderActivePlanCompact(taskCard, activePlanMeta, structuredMax)
	}
	packet := b.buildContextPacket(turnPromptInput{
		pinnedText:           pinnedText,
		digestText:           digestText,
		memText:              memText,
		identityText:         b.IdentityText,
		staticMemoryText:     b.StaticMemory,
		docContextText:       docContextText,
		workspaceContextText: workspaceContextText,
		heartbeatText:        heartbeat,
		compactionText:       compactionText,
		activePlanText:       activePlanText,
		eventContextText:     eventContext,
		toolPolicyMode:       metaStringValue(opts.EventMeta, "tool_policy_mode"),
		currentUserMessage:   opts.UserMessage,
		currentUserMessageID: opts.UserMessageID,
		turnAttachments:      turnAttachments,
		recentExecutionText:  recentExecutionText,
	})
	packet.RecentHistory = hist
	packet.Budget = estimatePacketBudget(&packet, b)
	if len(rejected) > 0 {
		packet.Budget.Rejected = append(packet.Budget.Rejected, rejected...)
	}
	return packet, retrieved, nil
}

func (b *Builder) currentHeartbeatText() string {
	if b == nil {
		return ""
	}
	if path, text, err := heartbeat.LoadTasksFile(b.HeartbeatTasksFile, b.WorkspaceDir); err == nil && strings.TrimSpace(path) != "" {
		if heartbeat.HasActiveInstructions(text) {
			return text
		}
		return ""
	}
	return strings.TrimSpace(b.HeartbeatText)
}

func attachmentsFromPayload(payload map[string]any) []artifacts.Attachment {
	chatAtts := chatAttachmentsFromPayload(payload)
	out := make([]artifacts.Attachment, 0, len(chatAtts))
	for _, att := range chatAtts {
		if art, ok := att.ToArtifactAttachment(); ok {
			out = append(out, art)
			continue
		}
	}
	if len(out) > 0 {
		return out
	}
	if len(payload) == 0 {
		return nil
	}
	raw := payload["attachments"]
	if raw == nil {
		if meta, ok := payload["meta"].(map[string]any); ok {
			raw = meta["attachments"]
		}
	}
	if raw == nil {
		return nil
	}
	atts := attachmentsFromRaw(raw)
	for _, att := range atts {
		if strings.TrimSpace(att.ArtifactID) == "" {
			continue
		}
		if strings.TrimSpace(att.Filename) == "" {
			att.Filename = "attachment"
		}
		if strings.TrimSpace(att.Kind) == "" {
			att.Kind = artifacts.DetectKind(att.Filename, att.Mime)
		}
		out = append(out, att)
	}
	return out
}

func chatAttachmentsFromPayload(payload map[string]any) []ChatAttachment {
	if len(payload) == 0 {
		return nil
	}
	raw := payload["attachments"]
	if raw == nil {
		if meta, ok := payload["meta"].(map[string]any); ok {
			raw = meta["attachments"]
		}
	}
	return DecodeChatAttachments(raw)
}

func toolCallsFromPayload(raw any) []providers.ToolCall {
	switch typed := raw.(type) {
	case []providers.ToolCall:
		return append([]providers.ToolCall(nil), typed...)
	case []any:
		out := make([]providers.ToolCall, 0, len(typed))
		for _, item := range typed {
			call, ok := decodeToolCall(item)
			if !ok {
				continue
			}
			out = append(out, call)
		}
		return out
	case []map[string]any:
		out := make([]providers.ToolCall, 0, len(typed))
		for _, item := range typed {
			call, ok := decodeToolCall(item)
			if !ok {
				continue
			}
			out = append(out, call)
		}
		return out
	default:
		return nil
	}
}

func decodeToolCall(raw any) (providers.ToolCall, bool) {
	switch typed := raw.(type) {
	case providers.ToolCall:
		if strings.TrimSpace(typed.Function.Name) == "" {
			return providers.ToolCall{}, false
		}
		if strings.TrimSpace(typed.Type) == "" {
			typed.Type = "function"
		}
		return typed, true
	case map[string]any:
		var call providers.ToolCall
		call.ID = payloadStringValue(typed["id"])
		call.Index = payloadIntValue(typed["index"])
		call.Type = payloadStringValue(typed["type"])
		function, _ := typed["function"].(map[string]any)
		call.Function.Name = payloadStringValue(function["name"])
		call.Function.Arguments = payloadStringValue(function["arguments"])
		if strings.TrimSpace(call.Function.Name) == "" {
			return providers.ToolCall{}, false
		}
		if strings.TrimSpace(call.Type) == "" {
			call.Type = "function"
		}
		return call, true
	default:
		return providers.ToolCall{}, false
	}
}

func attachmentsFromRaw(raw any) []artifacts.Attachment {
	switch typed := raw.(type) {
	case []artifacts.Attachment:
		return append([]artifacts.Attachment(nil), typed...)
	case []any:
		out := make([]artifacts.Attachment, 0, len(typed))
		for _, item := range typed {
			att, ok := decodeAttachment(item)
			if !ok {
				continue
			}
			out = append(out, att)
		}
		return out
	case []map[string]any:
		out := make([]artifacts.Attachment, 0, len(typed))
		for _, item := range typed {
			att, ok := decodeAttachment(item)
			if !ok {
				continue
			}
			out = append(out, att)
		}
		return out
	default:
		return nil
	}
}

func decodeAttachment(raw any) (artifacts.Attachment, bool) {
	switch typed := raw.(type) {
	case artifacts.Attachment:
		return typed, true
	case map[string]any:
		att := artifacts.Attachment{
			ArtifactID: payloadStringValue(typed["artifact_id"]),
			Filename:   payloadStringValue(typed["filename"]),
			Mime:       payloadStringValue(typed["mime"]),
			Kind:       payloadStringValue(typed["kind"]),
			SizeBytes:  payloadInt64Value(typed["size_bytes"]),
		}
		return att, true
	default:
		return artifacts.Attachment{}, false
	}
}

func payloadStringValue(raw any) string {
	switch typed := raw.(type) {
	case string:
		return strings.TrimSpace(typed)
	case json.Number:
		return strings.TrimSpace(typed.String())
	default:
		if raw == nil {
			return ""
		}
		return strings.TrimSpace(fmt.Sprint(raw))
	}
}

func payloadIntValue(raw any) int {
	switch typed := raw.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case json.Number:
		if value, err := typed.Int64(); err == nil {
			return int(value)
		}
	}
	return 0
}

func payloadInt64Value(raw any) int64 {
	switch typed := raw.(type) {
	case int:
		return int64(typed)
	case int64:
		return typed
	case float64:
		return int64(typed)
	case json.Number:
		if value, err := typed.Int64(); err == nil {
			return value
		}
	}
	return 0
}

type visionBudget struct {
	remainingImages int
	remainingBytes  int64
}

func newVisionBudget() *visionBudget {
	return &visionBudget{
		remainingImages: defaultVisionMaxImages,
		remainingBytes:  defaultVisionTotalBytes,
	}
}

func (b *Builder) buildUserContent(ctx context.Context, text string, atts []artifacts.Attachment, budget *visionBudget) any {
	if !b.EnableVision || b.Artifacts == nil || len(atts) == 0 {
		return text
	}
	parts := make([]map[string]any, 0, len(atts)+1)
	imageParts := 0
	if strings.TrimSpace(text) != "" {
		parts = append(parts, map[string]any{
			"type": "text",
			"text": text,
		})
	}
	for _, att := range atts {
		if strings.TrimSpace(att.Kind) != artifacts.KindImage && !strings.HasPrefix(strings.ToLower(strings.TrimSpace(att.Mime)), "image/") {
			continue
		}
		part, ok := b.imagePart(ctx, att, budget)
		if !ok {
			continue
		}
		parts = append(parts, part)
		imageParts++
	}
	if imageParts == 0 {
		return text
	}
	return parts
}

func (b *Builder) imagePart(ctx context.Context, att artifacts.Attachment, budget *visionBudget) (map[string]any, bool) {
	if budget == nil || budget.remainingImages <= 0 || budget.remainingBytes <= 0 {
		return nil, false
	}
	stored, err := b.Artifacts.Lookup(ctx, att.ArtifactID)
	if err != nil {
		return nil, false
	}
	sizeBytes := stored.SizeBytes
	if sizeBytes <= 0 {
		info, err := os.Stat(stored.Path)
		if err != nil {
			return nil, false
		}
		sizeBytes = info.Size()
	}
	if sizeBytes <= 0 || sizeBytes > defaultVisionMaxImageBytes || sizeBytes > budget.remainingBytes {
		return nil, false
	}
	data, err := readCappedFile(stored.Path, defaultVisionMaxImageBytes)
	if err != nil {
		return nil, false
	}
	if int64(len(data)) > budget.remainingBytes {
		return nil, false
	}
	mimeType := strings.TrimSpace(stored.Mime)
	if mimeType == "" {
		mimeType = strings.TrimSpace(att.Mime)
	}
	if mimeType == "" {
		mimeType = mime.TypeByExtension(filepath.Ext(stored.Path))
	}
	if !strings.HasPrefix(strings.ToLower(mimeType), "image/") {
		return nil, false
	}
	budget.remainingImages--
	budget.remainingBytes -= int64(len(data))
	return map[string]any{
		"type": "image_url",
		"image_url": map[string]any{
			"url": "data:" + mimeType + ";base64," + base64.StdEncoding.EncodeToString(data),
		},
	}, true
}

func readCappedFile(path string, maxBytes int64) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("file exceeds vision limit")
	}
	return data, nil
}

func (b *Builder) composeSystemPrompt(pinnedText, digestText, memText, identityText, staticMemoryText, heartbeatText, structuredContextText, docContextText, workspaceContextText string) string {
	packet := b.buildContextPacket(turnPromptInput{
		pinnedText:           pinnedText,
		digestText:           digestText,
		memText:              memText,
		identityText:         identityText,
		staticMemoryText:     staticMemoryText,
		heartbeatText:        heartbeatText,
		eventContextText:     structuredContextText,
		docContextText:       docContextText,
		workspaceContextText: workspaceContextText,
	})
	combined := renderStablePrefix(packet)
	if turn := renderTurnTier(packet); strings.TrimSpace(turn) != "" {
		if combined == "" {
			combined = turn
		} else {
			combined = strings.TrimSpace(combined + "\n\n" + turn)
		}
	}
	maxTotal := b.BootstrapTotalMaxChars
	if maxTotal <= 0 {
		maxTotal = defaultBootstrapTotalMaxChars
	}
	return truncateText(combined, maxTotal)
}

func (b *Builder) composeSystemContent(pinnedText, digestText, memText, identityText, staticMemoryText, heartbeatText, structuredContextText, docContextText, workspaceContextText string) any {
	packet := b.buildContextPacket(turnPromptInput{
		pinnedText:           pinnedText,
		digestText:           digestText,
		memText:              memText,
		identityText:         identityText,
		staticMemoryText:     staticMemoryText,
		heartbeatText:        heartbeatText,
		eventContextText:     structuredContextText,
		docContextText:       docContextText,
		workspaceContextText: workspaceContextText,
	})
	static := renderStaticTier(packet)
	session := renderSessionTier(packet)
	turn := renderTurnTier(packet)
	if b != nil && b.Provider != nil && b.Provider.SupportsExplicitPromptCache() {
		return providers.BuildCacheAwareTieredContent(static, session, turn)
	}
	combined := renderStablePrefix(packet)
	if strings.TrimSpace(turn) != "" {
		if combined == "" {
			combined = turn
		} else {
			combined = strings.TrimSpace(combined + "\n\n" + turn)
		}
	}
	maxTotal := b.BootstrapTotalMaxChars
	if maxTotal <= 0 {
		maxTotal = defaultBootstrapTotalMaxChars
	}
	return truncateText(combined, maxTotal)
}

func (b *Builder) renderStablePrefix(pinnedText, digestText, memText, identityText, staticMemoryText, docContextText, workspaceContextText string) string {
	maxEach := b.BootstrapMaxChars
	if maxEach <= 0 {
		maxEach = defaultBootstrapMaxChars
	}
	maxTotal := b.BootstrapTotalMaxChars
	if maxTotal <= 0 {
		maxTotal = defaultBootstrapTotalMaxChars
	}
	skillsMax := b.SkillsSummaryMax
	if skillsMax <= 0 {
		skillsMax = defaultSkillsSummaryMax
	}

	soul := strings.TrimSpace(b.Soul)
	if soul == "" {
		soul = DefaultSoul
	}
	inst := strings.TrimSpace(b.AgentInstructions)
	if inst == "" {
		inst = DefaultAgentInstructions
	}
	notes := strings.TrimSpace(b.ToolNotes)
	if notes == "" {
		notes = DefaultToolNotes
	}

	var out strings.Builder
	out.WriteString("# System Prompt\n")
	for _, s := range append(b.staticPromptSections(identityText), b.sessionPromptSections(pinnedText, digestText, staticMemoryText)...) {
		out.WriteString("\n## ")
		out.WriteString(s.Title)
		out.WriteString("\n")
		out.WriteString(strings.TrimSpace(truncateText(s.Text, maxEach)))
		out.WriteString("\n")
	}
	return truncateText(strings.TrimSpace(out.String()), maxTotal)
}

func (b *Builder) renderVolatileSuffix(heartbeatText, structuredContextText, currentTurnText string) string {
	packet := b.buildContextPacket(turnPromptInput{
		heartbeatText:        heartbeatText,
		eventContextText:     structuredContextText,
		currentUserMessage:   currentTurnText,
		currentUserMessageID: 0,
	})
	return renderTurnTier(packet)
}

func (b *Builder) staticPromptSections(identityText string) []systemPromptSection {
	budgets := b.contextSectionBudgets()
	skillsMax := b.SkillsSummaryMax
	if skillsMax <= 0 {
		skillsMax = defaultSkillsSummaryMax
	}
	soul := strings.TrimSpace(b.Soul)
	if soul == "" {
		soul = DefaultSoul
	}
	inst := strings.TrimSpace(b.AgentInstructions)
	if inst == "" {
		inst = DefaultAgentInstructions
	}
	notes := strings.TrimSpace(b.ToolNotes)
	if notes == "" {
		notes = DefaultToolNotes
	}
	sections := []systemPromptSection{
		{Title: "SOUL.md", XMLTag: xmlTagAssistantIdentity, Text: soul, Protected: true, CacheClass: CacheClassStatic, TokenCap: budgets.SoulIdentity, MinTokens: minProtectedTokens(budgets.SoulIdentity)},
	}
	if t := strings.TrimSpace(identityText); t != "" {
		sections = append(sections, systemPromptSection{
			Title: "Identity", XMLTag: xmlTagAssistantIdentity, Text: t, Protected: true, CacheClass: CacheClassStatic,
			TokenCap: budgets.SoulIdentity, MinTokens: minProtectedTokens(budgets.SoulIdentity),
		})
	}
	sections = append(sections,
		systemPromptSection{Title: "AGENTS.md", XMLTag: xmlTagCodingAgentRules, Text: inst, Protected: true, CacheClass: CacheClassStatic, TokenCap: budgets.SoulIdentity, MinTokens: minProtectedTokens(budgets.SoulIdentity)},
		systemPromptSection{Title: "TOOLS.md", XMLTag: xmlTagToolPolicy, Text: notes, Protected: true, CacheClass: CacheClassStatic, TokenCap: budgets.ToolPolicy, MinTokens: minProtectedTokens(budgets.ToolPolicy)},
		systemPromptSection{Title: "Skills Inventory", XMLTag: xmlTagSkillsInventory, Text: b.Skills.ModelSummary(skillsMax), CacheClass: CacheClassStatic, TokenCap: budgets.ToolSchemas},
	)
	return sections
}

func (b *Builder) sessionPromptSections(pinnedText, digestText, staticMemoryText string) []systemPromptSection {
	budgets := b.contextSectionBudgets()
	var sections []systemPromptSection
	if t := strings.TrimSpace(pinnedText); t != "" {
		sections = append(sections, systemPromptSection{
			Title: "Pinned Memory", XMLTag: xmlTagPinnedMemory, Text: t, Protected: true, CacheClass: CacheClassSession,
			Attrs: envelopeAttrs{"authority": "durable"}, TokenCap: budgets.PinnedMemory, MinTokens: minProtectedTokens(budgets.PinnedMemory),
		})
	}
	if t := strings.TrimSpace(staticMemoryText); t != "" {
		sections = append(sections, systemPromptSection{Title: "Static Memory", XMLTag: xmlTagPinnedMemory, Text: t, CacheClass: CacheClassSession, Attrs: envelopeAttrs{"authority": "durable"}})
	}
	if t := strings.TrimSpace(digestText); t != "" {
		sections = append(sections, systemPromptSection{Title: "Memory Digest", XMLTag: xmlTagRetrievedMemory, Text: t, CacheClass: CacheClassSession, TokenCap: budgets.MemoryDigest})
	}
	return sections
}

func (b *Builder) turnPromptSections(input turnPromptInput) []systemPromptSection {
	budgets := b.contextSectionBudgets()
	var sections []systemPromptSection
	if t := strings.TrimSpace(input.memText); t != "" {
		sections = append(sections, systemPromptSection{
			Title: "Retrieved Memory", XMLTag: xmlTagRetrievedMemory, Text: t, CacheClass: CacheClassTurn,
			Attrs: envelopeAttrs{"authority": "suggestive", "freshness": "possibly_stale"}, TokenCap: budgets.RetrievedMemory,
		})
	}
	if t := strings.TrimSpace(input.workspaceContextText); t != "" {
		sections = append(sections, systemPromptSection{
			Title: "Workspace Context", XMLTag: xmlTagWorkspaceContext, Text: t, CacheClass: CacheClassTurn,
			Attrs: envelopeAttrs{"authority": "partial_index", "freshness": "possibly_stale"}, TokenCap: budgets.WorkspaceContext,
		})
	}
	if t := strings.TrimSpace(input.docContextText); t != "" {
		sections = append(sections, systemPromptSection{
			Title: "Indexed File Context", XMLTag: xmlTagWorkspaceContext, Text: t, CacheClass: CacheClassTurn,
			Attrs: envelopeAttrs{"authority": "partial_index", "freshness": "possibly_stale"}, TokenCap: budgets.WorkspaceContext,
		})
	}
	if t := strings.TrimSpace(input.compactionText); t != "" {
		sections = append(sections, systemPromptSection{Title: "Context Compaction", XMLTag: xmlTagContextCompaction, Text: t, CacheClass: CacheClassTurn})
	}
	if t := strings.TrimSpace(b.renderRuntimeContext(input.toolPolicyMode)); t != "" {
		sections = append(sections, systemPromptSection{Title: "Runtime Context", XMLTag: xmlTagRuntimeContext, Text: t, CacheClass: CacheClassTurn})
	}
	if t := strings.TrimSpace(renderCurrentUserRequestBody(input.currentUserMessage, input.currentUserMessageID)); t != "" {
		cap := budgets.CurrentTurn
		if cap <= 0 {
			cap = budgets.ActiveTaskCard
		}
		if cap <= 0 {
			cap = 160
		}
		sections = append(sections, systemPromptSection{
			Title: "Current Turn", XMLTag: xmlTagCurrentUserRequest, Text: t, Protected: true, CacheClass: CacheClassTurn,
			TokenCap: cap, MinTokens: minProtectedTokens(cap),
		})
	}
	if t := strings.TrimSpace(input.activePlanText); t != "" {
		sections = append(sections, systemPromptSection{
			Title: "Active Plan", XMLTag: xmlTagActiveTurnPlan, Text: t, Protected: true, CacheClass: CacheClassTurn,
			TokenCap: budgets.ActiveTaskCard, MinTokens: minProtectedTokens(budgets.ActiveTaskCard),
		})
	}
	if t := strings.TrimSpace(renderUserAttachmentsBody(input.turnAttachments)); t != "" {
		sections = append(sections, systemPromptSection{
			Title: "User Attachments", XMLTag: xmlTagUserAttachments, Text: t, Protected: true, CacheClass: CacheClassTurn,
			TokenCap: budgets.ActiveTaskCard, MinTokens: minProtectedTokens(budgets.ActiveTaskCard),
		})
	}
	if t := strings.TrimSpace(input.recentExecutionText); t != "" {
		sections = append(sections, systemPromptSection{Title: "Recent Execution", XMLTag: xmlTagRecentExecution, Text: t, CacheClass: CacheClassTurn})
	}
	if t := strings.TrimSpace(input.heartbeatText); t != "" {
		sections = append(sections, systemPromptSection{Title: "Heartbeat", XMLTag: xmlTagEventContext, Text: t, CacheClass: CacheClassTurn})
	}
	if t := strings.TrimSpace(input.eventContextText); t != "" {
		sections = append(sections, systemPromptSection{Title: "Event Context", XMLTag: xmlTagEventContext, Text: t, CacheClass: CacheClassTurn})
	}
	return sections
}

func formatStructuredEventContext(meta map[string]any, max int) string {
	if len(meta) == 0 {
		return ""
	}
	raw, ok := meta[triggers.MetaKeyStructuredEvent]
	if !ok {
		return ""
	}
	return truncateText(triggers.StructuredEventJSON(raw), max)
}

func truncateText(s string, max int) string {
	s = strings.TrimSpace(s)
	if max > 0 && len([]rune(s)) > max {
		return strings.TrimSpace(string([]rune(s)[:max])) + "\n…[truncated]"
	}
	return s
}

func formatPinned(m map[string]string) string {
	if len(m) == 0 {
		return "(none)"
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		v := strings.TrimSpace(m[k])
		if v == "" {
			continue
		}
		b.WriteString(fmt.Sprintf("- %s: %s\n", k, oneLine(v, defaultPinnedOneLineMax)))
	}
	s := strings.TrimSpace(b.String())
	if s == "" {
		return "(none)"
	}
	return s
}

func formatRetrieved(ms []memory.Retrieved) string {
	text, _ := formatRetrievedBounded(ms, 0)
	return text
}

func formatRetrievedBounded(ms []memory.Retrieved, maxChars int) (string, []int64) {
	if len(ms) == 0 {
		return "(none)", nil
	}
	var b strings.Builder
	ids := make([]int64, 0, len(ms))
	for i, m := range ms {
		kind := strings.TrimSpace(m.Kind)
		if kind == "" {
			kind = "note"
		}
		ref := strings.TrimSpace(m.Ref)
		if ref == "" && m.ID > 0 {
			ref = fmt.Sprintf("memory:%d", m.ID)
		}
		if ref == "" {
			ref = m.Source
		}
		reason := strings.TrimSpace(m.Reason)
		if reason == "" {
			reason = "retrieved"
		}
		line := fmt.Sprintf("%d) [%s score=%.3f ref=%s reason=%s] %s\n", i+1, kind, m.Score, ref, reason, oneLine(m.Text, defaultRetrievedOneLineMax))
		if maxChars > 0 && b.Len()+len(line) > maxChars {
			if b.Len() > 0 {
				break
			}
			line = strings.TrimSpace(truncateText(line, maxChars)) + "\n"
		}
		b.WriteString(line)
		if m.ID > 0 {
			ids = append(ids, m.ID)
		}
	}
	out := strings.TrimSpace(b.String())
	if out == "" {
		return "(none)", nil
	}
	return out, ids
}

// digestKinds holds the note kinds that qualify for the Memory Digest section.
var digestKinds = map[string]struct{}{
	db.MemoryKindFact:       {},
	db.MemoryKindPreference: {},
	db.MemoryKindGoal:       {},
	db.MemoryKindProcedure:  {},
}

// formatMemoryDigest builds a compact digest from top active durable-kind
// notes in the retrieved set. It is bounded to maxLines lines.
func formatMemoryDigest(ms []memory.Retrieved, maxLines int) string {
	text, _ := formatMemoryDigestBounded(ms, maxLines, 0)
	return text
}

func formatMemoryDigestBounded(ms []memory.Retrieved, maxLines int, maxChars int) (string, []int64) {
	if maxLines <= 0 {
		maxLines = defaultDigestLineMax
	}
	var b strings.Builder
	ids := make([]int64, 0, maxLines)
	count := 0
	for _, m := range ms {
		if _, ok := digestKinds[m.Kind]; !ok {
			continue
		}
		// Treat empty status as active (notes inserted before the metadata
		// migration retain their zero-value status field).
		if m.Status != "" && m.Status != db.MemoryStatusActive {
			continue
		}
		line := renderSemanticMemoryDigestLine(retrievedMemoryLine{m}) + "\n"
		if maxChars > 0 && b.Len()+len(line) > maxChars {
			if b.Len() > 0 {
				break
			}
			line = strings.TrimSpace(truncateText(line, maxChars)) + "\n"
		}
		b.WriteString(line)
		if m.ID > 0 {
			ids = append(ids, m.ID)
		}
		count++
		if count >= maxLines {
			break
		}
	}
	return strings.TrimSpace(b.String()), ids
}

func uniqueInt64(ids []int64) []int64 {
	if len(ids) == 0 {
		return nil
	}
	seen := make(map[int64]struct{}, len(ids))
	out := make([]int64, 0, len(ids))
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

func oneLine(s string, max int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.Join(strings.Fields(s), " ")
	if max > 0 && len([]rune(s)) > max {
		s = string([]rune(s)[:max]) + "…"
	}
	return s
}

func cachedEmbed(ctx context.Context, provider *providers.Client, fingerprint, model, input string) ([]float32, error) {
	input = strings.TrimSpace(input)
	model = strings.TrimSpace(model)
	fingerprint = strings.TrimSpace(fingerprint)
	if provider == nil {
		return nil, fmt.Errorf("provider not configured")
	}
	if model == "" || input == "" {
		return provider.Embed(ctx, model, input)
	}
	key := embedCacheKey{fingerprint: fingerprint, model: model, input: input}
	now := time.Now()
	promptEmbedCache.mu.Lock()
	if entry, ok := promptEmbedCache.entries[key]; ok && entry.expiresAt.After(now) {
		entry.usedAt = now
		promptEmbedCache.entries[key] = entry
		vec := append([]float32(nil), entry.vec...)
		promptEmbedCache.mu.Unlock()
		return vec, nil
	}
	promptEmbedCache.mu.Unlock()

	vec, err := provider.Embed(ctx, model, input)
	if err != nil {
		return nil, err
	}
	promptEmbedCache.mu.Lock()
	if len(promptEmbedCache.entries) >= embedCacheMaxEntries {
		var oldestKey embedCacheKey
		var oldest time.Time
		for k, entry := range promptEmbedCache.entries {
			if oldest.IsZero() || entry.usedAt.Before(oldest) {
				oldest = entry.usedAt
				oldestKey = k
			}
		}
		delete(promptEmbedCache.entries, oldestKey)
	}
	promptEmbedCache.entries[key] = embedCacheEntry{
		vec:       append([]float32(nil), vec...),
		expiresAt: now.Add(embedCacheTTL),
		usedAt:    now,
	}
	promptEmbedCache.mu.Unlock()
	return vec, nil
}
