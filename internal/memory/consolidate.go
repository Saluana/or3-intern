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

const consolidationPrompt = `You are consolidating chat memory.

Return ONLY JSON with this exact shape:
{"summary":"...", "canonical_memory":"..."}

Rules:
- summary: 3-5 concise sentences describing key facts, decisions, and context from the excerpt.
- canonical_memory: concise markdown bullet list of durable facts/preferences. Start from Existing canonical memory, keep still-true facts, and merge new stable facts.
- If no durable facts changed, canonical_memory may equal Existing canonical memory.

Existing canonical memory:
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
	summary, canonical := parseConsolidationOutput(contentToStr(resp.Choices[0].Message.Content))
	summary = trimTo(summary, maxInputChars/canonicalMemoryInputDivisor)
	canonical = trimTo(canonical, maxInputChars)
	if canonical == "" {
		canonical = currentCanonical
	}

	if summary == "" {
		w := db.ConsolidationWrite{
			SessionKey:  sessionKey,
			ScopeKey:    memScope,
			CursorMsgID: lastIncludedID,
		}
		if canonical != "" {
			w.CanonicalKey = canonicalKey
			w.CanonicalText = canonical
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
	if embedModel != "" {
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

	w := db.ConsolidationWrite{
		SessionKey:  sessionKey,
		ScopeKey:    memScope,
		NoteText:    summary,
		Embedding:   embedding,
		SourceMsgID: sql.NullInt64{Int64: lastIncludedID, Valid: true},
		NoteTags:    "consolidation",
		CursorMsgID: lastIncludedID,
	}
	if canonical != "" {
		w.CanonicalKey = canonicalKey
		w.CanonicalText = canonical
	}
	_, err = c.DB.WriteConsolidation(ctx, w)
	if err != nil {
		return false, fmt.Errorf("consolidation write: %w", err)
	}

	log.Printf("consolidated %d messages for session %q into memory note", len(msgs), sessionKey)
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

type consolidationOutput struct {
	Summary   string `json:"summary"`
	Canonical string `json:"canonical_memory"`
}

func parseConsolidationOutput(raw string) (summary string, canonical string) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", ""
	}
	var out consolidationOutput
	if err := json.Unmarshal([]byte(raw), &out); err == nil {
		return strings.TrimSpace(out.Summary), strings.TrimSpace(out.Canonical)
	}
	return raw, ""
}

func trimTo(s string, max int) string {
	s = strings.TrimSpace(s)
	if max > 0 && len(s) > max {
		return strings.TrimSpace(s[:max])
	}
	return s
}
