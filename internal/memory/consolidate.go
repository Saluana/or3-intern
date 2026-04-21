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

const consolidationPrompt = `You are consolidating chat memory.

Return ONLY a single JSON object with this exact shape:
{
  "summary": "...",
  "facts": ["...", "..."],
  "preferences": ["...", "..."],
  "goals": ["...", "..."],
  "procedures": ["...", "..."]
}

Rules:
- summary: 3-5 concise sentences covering key decisions, outcomes, and context.
- facts: stable factual items extracted from the conversation (names, versions, states). Keep each item under 300 characters.
- preferences: durable user preferences or working-style notes. Keep each item under 300 characters.
- goals: ongoing or stated objectives. Keep each item under 300 characters.
- procedures: step-by-step processes or runbooks mentioned. Keep each item under 300 characters.
- Any list may be empty ([]) when nothing relevant was observed.
- Existing pinned memory contains only ultra-stable facts and preferences. Do not repeat those items.

Existing pinned memory (ultra-stable only):
%s

Conversation excerpt:
%s`

// Consolidator rolls up conversation messages older than the active history
// window into durable memory notes (stored in memory_notes for vector/FTS
// retrieval). It is safe to call MaybeConsolidate after every agent turn;
// it is a no-op when there is nothing to consolidate.
type Consolidator struct {
	DB         *db.DB
	Provider   *providers.Client
	EmbedModel string
	ChatModel  string
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

	// Build a plain-text conversation transcript.
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
	transcript := strings.TrimSpace(sb.String())
	memScope := sessionKey
	if c.DB != nil && strings.TrimSpace(sessionKey) != "" {
		if resolved, resolveErr := c.DB.ResolveScopeKey(ctx, sessionKey); resolveErr == nil && strings.TrimSpace(resolved) != "" {
			memScope = resolved
		}
	}
	if memScope == "" || memScope == scope.GlobalMemoryScope {
		memScope = scope.GlobalMemoryScope
	}
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

	currentCanonical, _, err := c.DB.GetPinnedValue(ctx, memScope, canonicalKey)
	if err != nil {
		return false, fmt.Errorf("consolidation get canonical memory: %w", err)
	}
	currentCanonical = trimTo(currentCanonical, maxInputChars/canonicalMemoryInputDivisor)

	model := c.ChatModel
	if model == "" {
		model = "gpt-4.1-mini"
	}
	req := providers.ChatCompletionRequest{
		Model: model,
		Messages: []providers.ChatMessage{
			{Role: "user", Content: fmt.Sprintf(consolidationPrompt, currentCanonical, transcript)},
		},
		Temperature: 0,
	}
	resp, err := c.Provider.Chat(ctx, req)
	if err != nil {
		return false, fmt.Errorf("consolidation chat: %w", err)
	}
	if len(resp.Choices) == 0 {
		return false, fmt.Errorf("consolidation: no choices returned")
	}
	parsed := parseConsolidationOutput(contentToStr(resp.Choices[0].Message.Content))
	summary := trimTo(parsed.Summary, maxInputChars/canonicalMemoryInputDivisor)

	// Build canonical pinned memory from ultra-stable items only
	// (preferences + facts), not rolling summaries.
	canonicalText := buildCanonicalPinnedText(currentCanonical, parsed.Preferences, parsed.Facts)
	canonicalText = trimTo(canonicalText, maxInputChars)

	if summary == "" && len(parsed.Facts)+len(parsed.Preferences)+len(parsed.Goals)+len(parsed.Procedures) == 0 {
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

	// Build extra typed notes from structured output.
	extraNotes := buildExtraNotes(parsed, sql.NullInt64{Int64: lastIncludedID, Valid: lastIncludedID > 0})

	w := db.ConsolidationWrite{
		SessionKey:  sessionKey,
		ScopeKey:    memScope,
		NoteText:    summary,
		Embedding:   embedding,
		SourceMsgID: sql.NullInt64{Int64: lastIncludedID, Valid: true},
		NoteTags:    "consolidation",
		NoteKind:    db.MemoryKindSummary,
		ExtraNotes:  extraNotes,
		CursorMsgID: lastIncludedID,
	}
	if canonicalText != "" {
		w.CanonicalKey = canonicalKey
		w.CanonicalText = canonicalText
	}
	_, err = c.DB.WriteConsolidation(ctx, w)
	if err != nil && len(embedding) >= 4 && isMemoryVectorDimMismatchError(err) {
		wantDims := len(embedding) / 4
		if rebuildErr := c.DB.RebuildMemoryVecIndexWithDim(ctx, wantDims); rebuildErr != nil {
			return false, fmt.Errorf("consolidation write: %w (rebuild failed: %v)", err, rebuildErr)
		}
		log.Printf("consolidation memory vectors rebuilt for session %q to %d dims", sessionKey, wantDims)
		_, err = c.DB.WriteConsolidation(ctx, w)
	}
	if err != nil {
		return false, fmt.Errorf("consolidation write: %w", err)
	}

	// Bounded stale-summary cleanup: mark old, never-used summaries as stale.
	if _, cleanErr := c.DB.CleanupStaleMemoryNotes(ctx, memScope, db.NowMS(), staleCleanupBatchSize); cleanErr != nil {
		log.Printf("consolidation cleanup stale notes: %v", cleanErr)
	}

	log.Printf("consolidated %d messages for session %q into memory note (+%d typed notes)", len(msgs), sessionKey, len(extraNotes))
	return true, nil
}

// contentToStr converts a ChatMessage Content (string or other) to a plain string.
func contentToStr(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", v)
}

func isMemoryVectorDimMismatchError(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "memory vector dims mismatch")
}

// consolidationOutput is the structured JSON shape returned by the LLM.
type consolidationOutput struct {
	Summary     string   `json:"summary"`
	Facts       []string `json:"facts"`
	Preferences []string `json:"preferences"`
	Goals       []string `json:"goals"`
	Procedures  []string `json:"procedures"`
	// Legacy field: accepted as a fallback for old-format responses.
	LegacyCanonical string `json:"canonical_memory"`
}

// parseConsolidationOutput parses the LLM response into a structured output.
// It first tries the new structured format, then falls back gracefully:
//   - If the new format is present (has summary), use it.
//   - If only the legacy {"summary","canonical_memory"} fields are present,
//     treat canonical_memory as a preference item.
//   - On any parse failure, return a minimal output with the raw text as summary.
func parseConsolidationOutput(raw string) consolidationOutput {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return consolidationOutput{}
	}

	// Attempt to extract a JSON object even if the model added surrounding text.
	jsonStr := extractJSON(raw)

	var out consolidationOutput
	if jsonStr != "" {
		if err := json.Unmarshal([]byte(jsonStr), &out); err == nil {
			out.Summary = strings.TrimSpace(out.Summary)
			out.Facts = sanitizeItems(out.Facts)
			out.Preferences = sanitizeItems(out.Preferences)
			out.Goals = sanitizeItems(out.Goals)
			out.Procedures = sanitizeItems(out.Procedures)
			// Absorb legacy canonical_memory as a preference if new fields missing.
			if len(out.Facts)+len(out.Preferences)+len(out.Goals)+len(out.Procedures) == 0 &&
				strings.TrimSpace(out.LegacyCanonical) != "" {
				out.Preferences = []string{strings.TrimSpace(out.LegacyCanonical)}
			}
			if out.Summary != "" || len(out.Facts)+len(out.Preferences)+len(out.Goals)+len(out.Procedures) > 0 {
				return out
			}
		}
	}

	// Final fallback: treat the entire raw text as a summary.
	return consolidationOutput{Summary: trimTo(raw, maxConsolidationItemLen*5)}
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
func buildExtraNotes(parsed consolidationOutput, sourceMsgID sql.NullInt64) []db.TypedNoteInput {
	type kindItems struct {
		kind  string
		items []string
	}
	groups := []kindItems{
		{db.MemoryKindFact, parsed.Facts},
		{db.MemoryKindPreference, parsed.Preferences},
		{db.MemoryKindGoal, parsed.Goals},
		{db.MemoryKindProcedure, parsed.Procedures},
	}
	out := make([]db.TypedNoteInput, 0)
	for _, g := range groups {
		for _, text := range g.items {
			text = strings.TrimSpace(text)
			if text == "" {
				continue
			}
			out = append(out, db.TypedNoteInput{
				Text:        text,
				Embedding:   make([]byte, 0),
				SourceMsgID: sourceMsgID,
				Tags:        "consolidation",
				Kind:        g.kind,
				Status:      db.MemoryStatusActive,
				Importance:  0,
			})
		}
	}
	return out
}
