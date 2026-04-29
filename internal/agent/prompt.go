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
Files:
- For unknown files, first use search_file or read_file mode=outline.
- Use read_file mode=grep to find matching lines.
- Use read_file mode=range for exact code sections.
- Use preview only for small files or broad orientation.
- Use read_skill mode=outline first. Read full skill content only when the outline is not enough.

Tool results:
- Read summary and stats first.
- Treat preview as partial.
- If artifact_id exists, use it only when the missing detail is actually needed.
- Prefer narrower follow-up reads over asking for huge output.

exec:
- Commands have timeouts and policy checks.
- Output is previewed. If output is too broad, rerun with a narrower command.

memory:
- Use the advertised memory tools by exact name. Common names are memory_search, memory_recent, memory_get_pinned, memory_add_note, and memory_set_pinned.
- Do not call a generic tool named memory.

web:
- Use web_fetch as the default fetch tool.
- web_fetch automatically converts HTML pages into Markdown artifacts to avoid dumping raw HTML into context; use raw=true only when literal response bytes are required.
- Use web_fetch_markdown only when you specifically need explicit HTML-to-Markdown source-byte controls.
- Use render=true for JavaScript-heavy pages.
- Use selector or waitMs when the important content loads late.

cron:
- Use cron only for scheduled reminders or recurring tasks, and only when cron is advertised as an available tool.
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
	SessionKey  string
	UserMessage string
	Autonomous  bool // true for cron/webhook/file-change events
	EventMeta   map[string]any
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
	sys := renderProviderMessages(packet, b)
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
	taskCardText := ""
	if b.DB != nil && !b.DisableTaskCard {
		if card, ok, err := loadTaskCard(ctx, b.DB, opts.SessionKey); err == nil && ok {
			taskCardText = renderTaskCard(card, structuredMax)
		}
	}
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
	if b.Mem != nil && b.Provider != nil && strings.TrimSpace(opts.UserMessage) != "" {
		vec, err := cachedEmbed(ctx, b.Provider, b.EmbedFingerprint, b.EmbedModel, opts.UserMessage)
		if err == nil {
			mem := *b.Mem
			mem.EmbedFingerprint = b.EmbedFingerprint
			mem.TaskContext = taskCardText
			retrieved, err = mem.Retrieve(ctx, scopeKey, opts.UserMessage, vec, b.VectorK, b.FTSK, b.TopK)
			if err != nil {
				log.Printf("memory retrieve failed for scope %q: %v", scopeKey, err)
				retrieved = nil
			}
			rejected = append(rejected, mem.LastRejected...)
		}
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
		docs, _ := b.DocRetriever.RetrieveDocs(ctx, scope.GlobalMemoryScope, opts.UserMessage, limit)
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
	visionBudget := newVisionBudget()
	hist := make([]providers.ChatMessage, 0, len(histRows))
	pendingToolCallIDs := make([]string, 0)
	for _, m := range histRows {
		msg := providers.ChatMessage{Role: m.Role, Content: m.Content}
		var payload map[string]any
		if err := json.Unmarshal([]byte(m.PayloadJSON), &payload); err == nil {
			if m.Role == "assistant" {
				if raw, ok := payload["tool_calls"]; ok {
					b, _ := json.Marshal(raw)
					var tcs []providers.ToolCall
					if err := json.Unmarshal(b, &tcs); err == nil {
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
	structuredContext := ""
	if opts.Autonomous {
		heartbeat = b.currentHeartbeatText()
		structuredContext = formatStructuredEventContext(opts.EventMeta, structuredMax)
	}
	if strings.TrimSpace(taskCardText) != "" {
		if strings.TrimSpace(structuredContext) == "" {
			structuredContext = "active_task_card:\n" + taskCardText
		} else {
			structuredContext = structuredContext + "\n\nactive_task_card:\n" + taskCardText
		}
	}
	if strings.TrimSpace(compactionText) != "" {
		if strings.TrimSpace(structuredContext) == "" {
			structuredContext = "compacted_chat_context:\n" + compactionText
		} else {
			structuredContext = structuredContext + "\n\ncompacted_chat_context:\n" + compactionText
		}
	}
	packet := b.buildContextPacket(pinnedText, digestText, memText, b.IdentityText, b.StaticMemory, heartbeat, structuredContext, docContextText, workspaceContextText)
	packet.RecentHistory = hist
	packet.Budget = estimatePacketBudget(packet, b)
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
	b, _ := json.Marshal(raw)
	var atts []artifacts.Attachment
	if err := json.Unmarshal(b, &atts); err != nil {
		return nil
	}
	out := make([]artifacts.Attachment, 0, len(atts))
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
	stable := b.renderStablePrefix(pinnedText, digestText, memText, identityText, staticMemoryText, docContextText, workspaceContextText)
	volatile := b.renderVolatileSuffix(heartbeatText, structuredContextText)
	if strings.TrimSpace(volatile) == "" {
		return stable
	}
	return strings.TrimSpace(stable + "\n\n" + volatile)
}

func (b *Builder) composeSystemContent(pinnedText, digestText, memText, identityText, staticMemoryText, heartbeatText, structuredContextText, docContextText, workspaceContextText string) any {
	stable := b.renderStablePrefix(pinnedText, digestText, memText, identityText, staticMemoryText, docContextText, workspaceContextText)
	volatile := b.renderVolatileSuffix(heartbeatText, structuredContextText)
	if b != nil && b.Provider != nil && b.Provider.SupportsExplicitPromptCache() {
		return providers.BuildCacheAwareSystemContent(stable, volatile)
	}
	if strings.TrimSpace(volatile) == "" {
		return stable
	}
	return strings.TrimSpace(stable + "\n\n" + volatile)
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

	type section struct {
		title string
		text  string
	}
	// Build sections in order, omitting optional ones when empty.
	sections := []section{
		{title: "SOUL.md", text: truncateText(soul, maxEach)},
	}
	if t := strings.TrimSpace(identityText); t != "" {
		sections = append(sections, section{title: "Identity", text: truncateText(t, maxEach)})
	}
	sections = append(sections, section{title: "AGENTS.md", text: truncateText(inst, maxEach)})
	if t := strings.TrimSpace(staticMemoryText); t != "" {
		sections = append(sections, section{title: "Static Memory", text: truncateText(t, maxEach)})
	}
	sections = append(sections, section{title: "TOOLS.md", text: truncateText(notes, maxEach)})
	sections = append(sections, section{title: "Pinned Memory", text: pinnedText})
	if t := strings.TrimSpace(digestText); t != "" {
		sections = append(sections, section{title: "Memory Digest", text: truncateText(t, maxEach)})
	}
	sections = append(sections, section{title: "Retrieved Memory", text: memText})
	if t := strings.TrimSpace(workspaceContextText); t != "" {
		sections = append(sections, section{title: "Workspace Context", text: truncateText(t, maxEach)})
	}
	if t := strings.TrimSpace(docContextText); t != "" {
		sections = append(sections, section{title: "Indexed File Context", text: truncateText(t, maxEach)})
	}
	sections = append(sections, section{title: "Skills Inventory", text: b.Skills.ModelSummary(skillsMax)})

	var out strings.Builder
	out.WriteString("# System Prompt\n")
	for _, s := range sections {
		out.WriteString("\n## ")
		out.WriteString(s.title)
		out.WriteString("\n")
		out.WriteString(strings.TrimSpace(s.text))
		out.WriteString("\n")
	}
	return truncateText(strings.TrimSpace(out.String()), maxTotal)
}

func (b *Builder) renderVolatileSuffix(heartbeatText, structuredContextText string) string {
	maxEach := b.BootstrapMaxChars
	if maxEach <= 0 {
		maxEach = defaultBootstrapMaxChars
	}
	type section struct {
		title string
		text  string
	}
	var sections []section
	if t := strings.TrimSpace(heartbeatText); t != "" {
		sections = append(sections, section{title: "Heartbeat", text: truncateText(t, maxEach)})
	}
	if t := strings.TrimSpace(structuredContextText); t != "" {
		sections = append(sections, section{title: "Structured Trigger Context", text: truncateText(t, maxEach)})
	}
	if len(sections) == 0 {
		return ""
	}
	var out strings.Builder
	for _, s := range sections {
		out.WriteString("## ")
		out.WriteString(s.title)
		out.WriteString("\n")
		out.WriteString(strings.TrimSpace(s.text))
		out.WriteString("\n\n")
	}
	return strings.TrimSpace(out.String())
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
	if max > 0 && len(s) > max {
		return strings.TrimSpace(s[:max]) + "\n…[truncated]"
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
	if max > 0 && len(s) > max {
		s = s[:max] + "…"
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
