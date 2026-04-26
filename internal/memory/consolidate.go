package memory

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"or3-intern/internal/db"
	"or3-intern/internal/providers"
	"or3-intern/internal/scope"
)

const defaultCanonicalMemoryKey = "long_term_memory"
const canonicalMemoryInputDivisor = 2

// maxConsolidationItemLen caps the length of any single item from structured
// consolidation output to prevent prompt bloat.
const maxConsolidationItemLen = 400

// maxConsolidationItems caps the total number of typed note entries emitted
// per consolidation pass for any single list field.
const maxConsolidationItems = 10

// staleCleanupBatchSize controls how many stale summary rows are marked per
// cleanup pass after consolidation.
const staleCleanupBatchSize = 20

const consolidationToolName = "record_consolidated_memory"

const consolidationSystemPrompt = `You are consolidating chat memory.

You MUST call the record_consolidated_memory tool exactly once. Do not answer conversationally.

Extract only information that will help future work. Prefer concrete, reusable facts over chat filler.

Process:
1. Read the whole excerpt once to understand the task and outcome.
2. Identify what a future assistant would need to remember to continue work safely.
3. Separate durable information into the correct fields.
4. Drop anything temporary, emotional, duplicated, or unsupported.
5. If a field has no useful durable information, set it to [].

Keep:
- Project state, file/module names, bugs found, fixes made, tests run, commands that worked, and unresolved blockers.
- User preferences about style, workflow, risk tolerance, tools, or implementation choices.
- Durable goals, accepted decisions, warnings, and repeatable procedures.

Ignore:
- Greetings, thanks, apologies, transient status updates, speculation, duplicate pinned memory, and details not supported by the excerpt.
- Raw tool dumps unless the surrounding conversation explains the durable outcome.

Tool argument rules:
- summary: 3-5 short sentences describing what changed, what was decided, and what remains relevant.
- facts: stable project/user/environment facts, versions, file names, or current states.
- preferences: how the user wants work done, communication style, quality bar, tools, or risk tolerance.
- goals: active objectives that remain useful after this turn.
- procedures: repeatable steps, commands, checks, or runbooks.
- decisions: choices that were accepted or settled.
- warnings: risks, pitfalls, failed approaches, or conditions that require caution.
- facts, preferences, goals, procedures, decisions, and warnings MUST always be JSON arrays of strings, even when there is only one item.
- Every list item must be standalone, specific, and under 300 characters.
- procedures should be actionable steps or commands, not vague descriptions.
- warnings should name the risk and condition that triggers it.
- Use [] when a category has no durable information. Do not invent details.`

const consolidationUserPrompt = `Existing pinned memory (ultra-stable only):
%s

Conversation excerpt:
%s`

const consolidationRetryPrompt = `Your previous response did not call record_consolidated_memory with valid arguments. Call record_consolidated_memory exactly once now.`

var consolidationToolDef = providers.ToolDef{
	Type: "function",
	Function: providers.ToolFunc{
		Name:        consolidationToolName,
		Description: "Persist validated structured memory extracted from a conversation excerpt.",
		Parameters: map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"summary":     map[string]any{"type": "string"},
				"facts":       map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
				"preferences": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
				"goals":       map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
				"procedures":  map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
				"decisions":   map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
				"warnings":    map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			},
			"required": []string{"summary", "facts", "preferences", "goals", "procedures", "decisions", "warnings"},
		},
	},
}

func consolidationToolChoice() any {
	// There is only one consolidation tool, so "required" is equivalent to
	// forcing the named tool but works with more OpenAI-compatible routers.
	return "required"
}

// Consolidator rolls up conversation messages older than the active history
// window into durable memory notes (stored in memory_notes for vector/FTS
// retrieval). It is safe to call MaybeConsolidate after every agent turn;
// it is a no-op when there is nothing to consolidate.
type Consolidator struct {
	DB               *db.DB
	Provider         *providers.Client
	EmbedModel       string
	EmbedFingerprint string
	ChatModel        string
	// WindowSize is the minimum number of consolidatable messages required
	// before a consolidation run is triggered. Default: 10.
	WindowSize int
	// MaxMessages bounds how many messages are processed per consolidation pass.
	// Default: 50.
	MaxMessages int
	// MaxInputChars bounds transcript size passed to the LLM. Default: 12000.
	MaxInputChars int
	// CanonicalPinnedKey is the memory_pinned key used for canonical long-term memory.
	CanonicalPinnedKey string
}

type RunMode struct {
	ArchiveAll bool
}

// MaybeConsolidate checks whether there are enough old messages to warrant a
// consolidation pass and, if so, summarises them into a memory note.
func (c *Consolidator) MaybeConsolidate(ctx context.Context, sessionKey string, historyMax int) error {
	_, err := c.RunOnce(ctx, sessionKey, historyMax, RunMode{})
	return err
}

// ArchiveAll drains all unconsolidated messages in bounded passes.
func (c *Consolidator) ArchiveAll(ctx context.Context, sessionKey string, historyMax int) error {
	const maxPasses = 1024
	for i := 0; i < maxPasses; i++ {
		didWork, err := c.RunOnce(ctx, sessionKey, historyMax, RunMode{ArchiveAll: true})
		if err != nil {
			return err
		}
		if !didWork {
			return nil
		}
	}
	return fmt.Errorf("archive-all exceeded max passes")
}

// ArchiveResetWindow performs one bounded archival pass over the most recent
// session messages. It is intended for /new, where reset latency must be
// predictable and normal background consolidation can continue preserving older
// history over time.
func (c *Consolidator) ArchiveResetWindow(ctx context.Context, sessionKey string, historyMax int) error {
	if c.Provider == nil {
		return nil
	}
	maxMessages := c.MaxMessages
	if maxMessages <= 0 {
		maxMessages = 50
	}
	if historyMax > maxMessages {
		maxMessages = historyMax
	}
	maxInputChars := c.MaxInputChars
	if maxInputChars <= 0 {
		maxInputChars = 12000
	}
	canonicalKey := strings.TrimSpace(c.CanonicalPinnedKey)
	if canonicalKey == "" {
		canonicalKey = defaultCanonicalMemoryKey
	}

	rows, err := c.DB.GetLastMessages(ctx, sessionKey, maxMessages)
	if err != nil {
		return fmt.Errorf("reset archive messages: %w", err)
	}
	if len(rows) == 0 {
		return nil
	}
	msgs := make([]db.ConsolidationMessage, 0, len(rows))
	var lastCandidateID int64
	for _, row := range rows {
		msgs = append(msgs, db.ConsolidationMessage{ID: row.ID, Role: row.Role, Content: row.Content})
		if row.ID > lastCandidateID {
			lastCandidateID = row.ID
		}
	}
	transcript, lastIncludedID := buildConsolidationTranscript(msgs, maxInputChars)
	if transcript == "" {
		return nil
	}
	if lastIncludedID == 0 {
		lastIncludedID = lastCandidateID
	}
	memScope := c.resolveMemoryScope(ctx, sessionKey)
	if err := c.writeConsolidatedTranscript(ctx, sessionKey, memScope, canonicalKey, transcript, lastIncludedID, maxInputChars, "reset_archive"); err != nil {
		if err == errEmptyConsolidationOutput {
			log.Printf("reset archive produced no durable memory for session %q; continuing reset", sessionKey)
			return nil
		}
		return err
	}
	log.Printf("reset-archived %d recent messages for session %q into one memory note", len(msgs), sessionKey)
	return nil
}

// RunOnce performs a single bounded consolidation pass.
func (c *Consolidator) RunOnce(ctx context.Context, sessionKey string, historyMax int, mode RunMode) (bool, error) {
	if c.Provider == nil {
		return false, nil
	}
	windowSize := c.WindowSize
	if windowSize <= 0 {
		windowSize = 10
	}
	maxMessages := c.MaxMessages
	if maxMessages <= 0 {
		maxMessages = 50
	}
	maxInputChars := c.MaxInputChars
	if maxInputChars <= 0 {
		maxInputChars = 12000
	}
	if historyMax <= 0 {
		historyMax = 40
	}
	canonicalKey := strings.TrimSpace(c.CanonicalPinnedKey)
	if canonicalKey == "" {
		canonicalKey = defaultCanonicalMemoryKey
	}

	lastID, oldestActiveID, err := c.DB.GetConsolidationRange(ctx, sessionKey, historyMax)
	if err != nil {
		return false, fmt.Errorf("consolidation range: %w", err)
	}
	beforeID := oldestActiveID
	if mode.ArchiveAll {
		beforeID = 0
	} else if oldestActiveID == 0 || oldestActiveID <= lastID+1 {
		return false, nil
	}

	msgs, err := c.DB.GetConsolidationMessages(ctx, sessionKey, lastID, beforeID, maxMessages)
	if err != nil {
		return false, fmt.Errorf("consolidation messages: %w", err)
	}
	if len(msgs) == 0 {
		return false, nil
	}
	lastCandidateID := msgs[len(msgs)-1].ID

	transcript, lastIncludedID := buildConsolidationTranscript(msgs, maxInputChars)
	memScope := c.resolveMemoryScope(ctx, sessionKey)
	if transcript == "" {
		_, err := c.DB.WriteConsolidation(ctx, db.ConsolidationWrite{
			SessionKey:  sessionKey,
			ScopeKey:    memScope,
			CursorMsgID: lastCandidateID,
		})
		if err != nil {
			return false, fmt.Errorf("consolidation advance cursor: %w", err)
		}
		return true, nil
	}
	shouldConsolidate := mode.ArchiveAll || len(msgs) >= windowSize
	if !shouldConsolidate {
		adaptiveTriggerChars := maxInputChars / canonicalMemoryInputDivisor
		if adaptiveTriggerChars <= 0 {
			adaptiveTriggerChars = 1
		}
		if len(msgs) >= maxMessages || len(transcript) >= adaptiveTriggerChars {
			shouldConsolidate = true
		}
	}
	if !shouldConsolidate {
		return false, nil
	}
	if err := c.writeConsolidatedTranscript(ctx, sessionKey, memScope, canonicalKey, transcript, lastIncludedID, maxInputChars, "consolidation"); err != nil {
		if err == errEmptyConsolidationOutput {
			currentCanonical, _, getErr := c.DB.GetPinnedValue(ctx, memScope, canonicalKey)
			if getErr != nil {
				return false, fmt.Errorf("consolidation get canonical memory: %w", getErr)
			}
			canonicalText := trimTo(currentCanonical, maxInputChars)
			w := db.ConsolidationWrite{
				SessionKey:  sessionKey,
				ScopeKey:    memScope,
				CursorMsgID: lastIncludedID,
			}
			if canonicalText != "" {
				w.CanonicalKey = canonicalKey
				w.CanonicalText = canonicalText
			}
			_, err := c.DB.WriteConsolidation(ctx, w)
			if err != nil {
				return false, fmt.Errorf("consolidation update cursor: %w", err)
			}
			log.Printf("consolidated %d messages for session %q (cursor-only)", len(msgs), sessionKey)
			return true, nil
		}
		return false, err
	}

	log.Printf("consolidated %d messages for session %q", len(msgs), sessionKey)
	return true, nil
}

var errEmptyConsolidationOutput = fmt.Errorf("empty consolidation output")

func buildConsolidationTranscript(msgs []db.ConsolidationMessage, maxInputChars int) (string, int64) {
	var sb strings.Builder
	var lastIncludedID int64
	for _, m := range msgs {
		// Skip tool messages – they're noisy and usually captured by the surrounding turns.
		if m.Role == "tool" {
			continue
		}
		content := strings.TrimSpace(m.Content)
		if content == "" {
			continue
		}
		line := m.Role + ": " + content
		if sb.Len()+len(line)+1 > maxInputChars {
			if sb.Len() == 0 {
				remaining := maxInputChars - len(m.Role) - 3
				if remaining > 0 {
					line = m.Role + ": " + content[:remaining] + "…"
					sb.WriteString(line)
					sb.WriteString("\n")
					lastIncludedID = m.ID
				}
			}
			break
		}
		sb.WriteString(line)
		sb.WriteString("\n")
		lastIncludedID = m.ID
	}
	return strings.TrimSpace(sb.String()), lastIncludedID
}

func (c *Consolidator) resolveMemoryScope(ctx context.Context, sessionKey string) string {
	memScope := sessionKey
	if c.DB != nil && strings.TrimSpace(sessionKey) != "" {
		if resolved, resolveErr := c.DB.ResolveScopeKey(ctx, sessionKey); resolveErr == nil && strings.TrimSpace(resolved) != "" {
			memScope = resolved
		}
	}
	if memScope == "" || memScope == scope.GlobalMemoryScope {
		memScope = scope.GlobalMemoryScope
	}
	return memScope
}

func (c *Consolidator) writeConsolidatedTranscript(ctx context.Context, sessionKey, memScope, canonicalKey, transcript string, lastIncludedID int64, maxInputChars int, noteTags string) error {
	currentCanonical, _, err := c.DB.GetPinnedValue(ctx, memScope, canonicalKey)
	if err != nil {
		return fmt.Errorf("consolidation get canonical memory: %w", err)
	}
	currentCanonical = trimTo(currentCanonical, maxInputChars/canonicalMemoryInputDivisor)

	model := c.ChatModel
	if model == "" {
		model = "gpt-4.1-mini"
	}
	parsed, err := c.requestConsolidationOutput(ctx, model, currentCanonical, transcript)
	if err != nil {
		if err == errEmptyConsolidationOutput {
			return err
		}
		return fmt.Errorf("consolidation structured output: %w", err)
	}
	summary := trimTo(parsed.Summary, maxInputChars/canonicalMemoryInputDivisor)

	// Build canonical pinned memory from ultra-stable items only
	// (preferences + facts), not rolling summaries.
	canonicalText := buildCanonicalPinnedText(currentCanonical, parsed.Preferences, parsed.Facts)
	canonicalText = trimTo(canonicalText, maxInputChars)

	if summary == "" && len(parsed.Facts)+len(parsed.Preferences)+len(parsed.Goals)+len(parsed.Procedures)+len(parsed.Decisions)+len(parsed.Warnings) == 0 {
		return errEmptyConsolidationOutput
	}

	embedModel := c.EmbedModel
	var embedding []byte
	if embedModel != "" && summary != "" {
		vec, embedErr := c.Provider.Embed(ctx, embedModel, summary)
		if embedErr != nil {
			log.Printf("consolidation embed failed: %v", embedErr)
			embedding = make([]byte, 0)
		} else {
			embedding = PackFloat32(vec)
		}
	} else {
		embedding = make([]byte, 0)
	}

	extraNotes := buildExtraNotes(parsed, sql.NullInt64{Int64: lastIncludedID, Valid: lastIncludedID > 0}, c.EmbedFingerprint)
	w := db.ConsolidationWrite{
		SessionKey:       sessionKey,
		ScopeKey:         memScope,
		NoteText:         summary,
		Embedding:        embedding,
		EmbedFingerprint: c.EmbedFingerprint,
		SourceMsgID:      sql.NullInt64{Int64: lastIncludedID, Valid: true},
		NoteTags:         noteTags,
		NoteKind:         db.MemoryKindSummary,
		ExtraNotes:       extraNotes,
		CursorMsgID:      lastIncludedID,
	}
	if canonicalText != "" {
		w.CanonicalKey = canonicalKey
		w.CanonicalText = canonicalText
	}
	_, err = c.DB.WriteConsolidation(ctx, w)
	if err != nil && len(embedding) >= 4 && isMemoryVectorProfileMismatchError(err) {
		wantDims := len(embedding) / 4
		if rebuildErr := c.DB.RebuildMemoryVecIndexWithProfile(ctx, wantDims, c.EmbedFingerprint); rebuildErr != nil {
			return fmt.Errorf("consolidation write: %w (rebuild failed: %v)", err, rebuildErr)
		}
		log.Printf("consolidation memory vectors rebuilt for session %q to %d dims (%s)", sessionKey, wantDims, strings.TrimSpace(c.EmbedFingerprint))
		_, err = c.DB.WriteConsolidation(ctx, w)
	}
	if err != nil {
		return fmt.Errorf("consolidation write: %w", err)
	}

	if _, cleanErr := c.DB.CleanupStaleMemoryNotes(ctx, memScope, db.NowMS(), staleCleanupBatchSize); cleanErr != nil {
		log.Printf("consolidation cleanup stale notes: %v", cleanErr)
	}
	return nil
}

func (c *Consolidator) requestConsolidationOutput(ctx context.Context, model, currentCanonical, transcript string) (consolidationOutput, error) {
	messages := []providers.ChatMessage{
		{Role: "system", Content: consolidationSystemPrompt},
		{Role: "user", Content: fmt.Sprintf(consolidationUserPrompt, currentCanonical, transcript)},
	}
	var lastErr error
	toolChoiceAllowed := true
	for attempt := 0; attempt < 2; attempt++ {
		req := providers.ChatCompletionRequest{
			Model:       model,
			Messages:    messages,
			Tools:       []providers.ToolDef{consolidationToolDef},
			Temperature: 0,
		}
		if toolChoiceAllowed {
			req.ToolChoice = consolidationToolChoice()
		}
		resp, err := c.Provider.Chat(ctx, req)
		if err != nil && toolChoiceAllowed && providerRejectedToolChoice(err) {
			toolChoiceAllowed = false
			req.ToolChoice = nil
			resp, err = c.Provider.Chat(ctx, req)
		}
		if err != nil {
			return consolidationOutput{}, fmt.Errorf("chat: %w", err)
		}
		parsed, parseErr := parseConsolidationResponse(resp)
		if parseErr == nil {
			return parsed, nil
		}
		lastErr = parseErr
		messages = append(messages, providers.ChatMessage{Role: "user", Content: consolidationRetryPrompt})
	}
	log.Printf("consolidation structured output rejected: %v", lastErr)
	return consolidationOutput{}, errEmptyConsolidationOutput
}

func providerRejectedToolChoice(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	if !strings.Contains(msg, "tool_choice") {
		return false
	}
	return strings.Contains(msg, "no endpoints") ||
		strings.Contains(msg, "not support") ||
		strings.Contains(msg, "unsupported") ||
		strings.Contains(msg, "invalid")
}

func isMemoryVectorProfileMismatchError(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "memory vector dims mismatch") ||
		strings.Contains(err.Error(), "memory embedding fingerprint mismatch")
}

// consolidationOutput is the structured JSON shape returned by the LLM.
type consolidationOutput struct {
	Summary     string   `json:"summary"`
	Facts       []string `json:"facts"`
	Preferences []string `json:"preferences"`
	Goals       []string `json:"goals"`
	Procedures  []string `json:"procedures"`
	Decisions   []string `json:"decisions"`
	Warnings    []string `json:"warnings"`
	// Legacy field: accepted as a fallback for old-format responses.
	LegacyCanonical string `json:"canonical_memory"`
}

type flexibleConsolidationOutput struct {
	Summary         string          `json:"summary"`
	Facts           json.RawMessage `json:"facts"`
	Preferences     json.RawMessage `json:"preferences"`
	Goals           json.RawMessage `json:"goals"`
	Procedures      json.RawMessage `json:"procedures"`
	Decisions       json.RawMessage `json:"decisions"`
	Warnings        json.RawMessage `json:"warnings"`
	LegacyCanonical string          `json:"canonical_memory"`
}

func parseConsolidationResponse(resp providers.ChatCompletionResponse) (consolidationOutput, error) {
	if len(resp.Choices) == 0 {
		return consolidationOutput{}, fmt.Errorf("no choices returned")
	}
	for _, tc := range resp.Choices[0].Message.ToolCalls {
		if strings.TrimSpace(tc.Function.Name) != consolidationToolName {
			continue
		}
		return parseConsolidationOutput(tc.Function.Arguments)
	}
	return consolidationOutput{}, fmt.Errorf("missing %s tool call", consolidationToolName)
}

// parseConsolidationOutput parses strict structured consolidation JSON. It
// accepts legacy canonical_memory only when otherwise valid JSON was returned.
// Malformed JSON and plain prose are rejected so they cannot become memory.
func parseConsolidationOutput(raw string) (consolidationOutput, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return consolidationOutput{}, fmt.Errorf("empty consolidation output")
	}

	// Attempt to extract a JSON object even if the model added surrounding text.
	jsonStr := extractJSON(raw)

	var out consolidationOutput
	if jsonStr != "" {
		if parsed, err := decodeConsolidationJSON(jsonStr); err == nil {
			out = parsed
			if out.Summary != "" || len(out.Facts)+len(out.Preferences)+len(out.Goals)+len(out.Procedures)+len(out.Decisions)+len(out.Warnings) > 0 {
				return out, nil
			}
		} else {
			return consolidationOutput{}, fmt.Errorf("invalid consolidation JSON: %w", err)
		}
	}

	return consolidationOutput{}, fmt.Errorf("missing usable consolidation JSON")
}

func decodeConsolidationJSON(jsonStr string) (consolidationOutput, error) {
	var raw flexibleConsolidationOutput
	if err := json.Unmarshal([]byte(jsonStr), &raw); err != nil {
		return consolidationOutput{}, err
	}
	out := consolidationOutput{
		Summary:         strings.TrimSpace(raw.Summary),
		LegacyCanonical: strings.TrimSpace(raw.LegacyCanonical),
	}
	var err error
	if out.Facts, err = parseConsolidationItems(raw.Facts); err != nil {
		return consolidationOutput{}, fmt.Errorf("facts: %w", err)
	}
	if out.Preferences, err = parseConsolidationItems(raw.Preferences); err != nil {
		return consolidationOutput{}, fmt.Errorf("preferences: %w", err)
	}
	if out.Goals, err = parseConsolidationItems(raw.Goals); err != nil {
		return consolidationOutput{}, fmt.Errorf("goals: %w", err)
	}
	if out.Procedures, err = parseConsolidationItems(raw.Procedures); err != nil {
		return consolidationOutput{}, fmt.Errorf("procedures: %w", err)
	}
	if out.Decisions, err = parseConsolidationItems(raw.Decisions); err != nil {
		return consolidationOutput{}, fmt.Errorf("decisions: %w", err)
	}
	if out.Warnings, err = parseConsolidationItems(raw.Warnings); err != nil {
		return consolidationOutput{}, fmt.Errorf("warnings: %w", err)
	}
	// Absorb legacy canonical_memory as a preference if new fields missing.
	if len(out.Facts)+len(out.Preferences)+len(out.Goals)+len(out.Procedures)+len(out.Decisions)+len(out.Warnings) == 0 && out.LegacyCanonical != "" {
		out.Preferences = []string{out.LegacyCanonical}
	}
	return out, nil
}

func parseConsolidationItems(raw json.RawMessage) ([]string, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var items []string
	if err := json.Unmarshal(raw, &items); err == nil {
		return sanitizeItems(items), nil
	}
	var single string
	if err := json.Unmarshal(raw, &single); err == nil {
		return sanitizeItems([]string{single}), nil
	}
	return nil, fmt.Errorf("expected string or []string")
}

// extractJSON attempts to locate and return the first complete JSON object
// in s. This handles models that add prose before or after the JSON.
func extractJSON(s string) string {
	start := strings.Index(s, "{")
	if start < 0 {
		return ""
	}
	depth := 0
	for i := start; i < len(s); i++ {
		switch s[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return s[start : i+1]
			}
		}
	}
	return ""
}

// sanitizeItems trims whitespace from each item, drops empties, caps length,
// and limits total count.
func sanitizeItems(items []string) []string {
	if len(items) == 0 {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if len(item) > maxConsolidationItemLen {
			item = strings.TrimSpace(item[:maxConsolidationItemLen]) + "..."
		}
		out = append(out, item)
		if len(out) >= maxConsolidationItems {
			break
		}
	}
	return out
}

// buildCanonicalPinnedText constructs a compact, bounded string from the
// most durable items (preferences and facts) suitable for pinned memory.
// It does NOT include rolling summaries.
func buildCanonicalPinnedText(existing string, prefs, facts []string) string {
	var sb strings.Builder
	seen := map[string]struct{}{}
	normalizeLine := func(s string) string {
		s = strings.TrimSpace(strings.TrimPrefix(s, "- "))
		s = strings.ToLower(strings.Join(strings.Fields(s), " "))
		return s
	}
	appendLine := func(line string) bool {
		line = strings.TrimSpace(strings.TrimPrefix(line, "- "))
		if line == "" {
			return true
		}
		norm := normalizeLine(line)
		if _, ok := seen[norm]; ok {
			return true
		}
		formatted := "- " + line
		if sb.Len()+len(formatted)+1 > 2500 {
			return false
		}
		if sb.Len() > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString(formatted)
		seen[norm] = struct{}{}
		return true
	}

	// Start from existing and keep it bounded.
	existing = trimTo(existing, 2000)
	if existing != "" {
		for _, line := range strings.Split(existing, "\n") {
			if !appendLine(line) {
				break
			}
		}
	}
	for _, p := range prefs {
		if !appendLine(p) {
			break
		}
	}
	for _, f := range facts {
		if !appendLine(f) {
			break
		}
	}
	return strings.TrimSpace(sb.String())
}

func trimTo(s string, max int) string {
	s = strings.TrimSpace(s)
	if max > 0 && len(s) > max {
		return strings.TrimSpace(s[:max])
	}
	return s
}

// buildExtraNotes converts parsed structured consolidation output into a slice
// of TypedNoteInput ready to be written alongside the summary note.
func buildExtraNotes(parsed consolidationOutput, sourceMsgID sql.NullInt64, embedFingerprint string) []db.TypedNoteInput {
	type kindItems struct {
		kind  string
		items []string
	}
	groups := []kindItems{
		{db.MemoryKindFact, parsed.Facts},
		{db.MemoryKindPreference, parsed.Preferences},
		{db.MemoryKindGoal, parsed.Goals},
		{db.MemoryKindProcedure, parsed.Procedures},
		{db.MemoryKindDecision, parsed.Decisions},
		{db.MemoryKindWarning, parsed.Warnings},
	}
	out := make([]db.TypedNoteInput, 0)
	for _, g := range groups {
		for _, text := range g.items {
			text = strings.TrimSpace(text)
			if text == "" {
				continue
			}
			out = append(out, db.TypedNoteInput{
				Text:             text,
				Embedding:        make([]byte, 0),
				EmbedFingerprint: embedFingerprint,
				SourceMsgID:      sourceMsgID,
				Tags:             "consolidation",
				Kind:             g.kind,
				Status:           db.MemoryStatusActive,
				Importance:       0,
			})
		}
	}
	return out
}
