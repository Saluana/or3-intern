package memory

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"strings"

	"or3-intern/internal/db"
	"or3-intern/internal/providers"
	"or3-intern/internal/scope"
)

const consolidationPrompt = `Summarize the following conversation excerpt in 3-5 sentences. Capture key facts, decisions, topics discussed, and any context useful for future reference. Be concise and specific.

CONVERSATION:
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
}

// MaybeConsolidate checks whether there are enough old messages to warrant a
// consolidation pass and, if so, summarises them into a memory note.
// historyMax is the number of recent messages kept in the active prompt window.
// Errors are logged but never fatal – consolidation is best-effort.
func (c *Consolidator) MaybeConsolidate(ctx context.Context, sessionKey string, historyMax int) error {
	if c.Provider == nil {
		return nil
	}
	windowSize := c.WindowSize
	if windowSize <= 0 {
		windowSize = 10
	}
	if historyMax <= 0 {
		historyMax = 40
	}

	lastID, oldestActiveID, err := c.DB.GetConsolidationRange(ctx, sessionKey, historyMax)
	if err != nil {
		return fmt.Errorf("consolidation range: %w", err)
	}
	// No active window yet (fewer messages than historyMax) or nothing new to consolidate.
	if oldestActiveID == 0 || oldestActiveID <= lastID+1 {
		return nil
	}

	msgs, err := c.DB.GetMessagesForConsolidation(ctx, sessionKey, lastID, oldestActiveID)
	if err != nil {
		return fmt.Errorf("consolidation messages: %w", err)
	}
	if len(msgs) < windowSize {
		return nil
	}

	// Build a plain-text conversation transcript.
	var sb strings.Builder
	for _, m := range msgs {
		// Skip tool messages – they're noisy and usually captured by the surrounding turns.
		if m.Role == "tool" {
			continue
		}
		sb.WriteString(m.Role)
		sb.WriteString(": ")
		sb.WriteString(strings.TrimSpace(m.Content))
		sb.WriteString("\n")
	}
	transcript := strings.TrimSpace(sb.String())
	if transcript == "" {
		return nil
	}

	// Ask the LLM for a summary.
	model := c.ChatModel
	if model == "" {
		model = "gpt-4.1-mini"
	}
	req := providers.ChatCompletionRequest{
		Model: model,
		Messages: []providers.ChatMessage{
			{Role: "user", Content: fmt.Sprintf(consolidationPrompt, transcript)},
		},
		Temperature: 0,
	}
	resp, err := c.Provider.Chat(ctx, req)
	if err != nil {
		return fmt.Errorf("consolidation chat: %w", err)
	}
	if len(resp.Choices) == 0 {
		return fmt.Errorf("consolidation: no choices returned")
	}
	summary := strings.TrimSpace(contentToStr(resp.Choices[0].Message.Content))
	if summary == "" {
		return nil
	}

	// Embed the summary.
	embedModel := c.EmbedModel
	var embedding []byte
	if embedModel != "" {
		vec, embedErr := c.Provider.Embed(ctx, embedModel, summary)
		if embedErr != nil {
			log.Printf("consolidation embed failed: %v", embedErr)
			// Store the note without an embedding – FTS will still work.
			embedding = make([]byte, 0)
		} else {
			embedding = PackFloat32(vec)
		}
	} else {
		embedding = make([]byte, 0)
	}

	// Determine the memory scope: use the global scope for the default session,
	// otherwise use the session's own scope so retrieval stays session-local.
	memScope := sessionKey
	if memScope == "" || memScope == scope.GlobalMemoryScope {
		memScope = scope.GlobalMemoryScope
	}

	// Persist the summary note.
	lastMsgID := msgs[len(msgs)-1].ID
	_, err = c.DB.InsertMemoryNote(ctx, memScope, summary, embedding,
		sql.NullInt64{Int64: lastMsgID, Valid: true}, "consolidation")
	if err != nil {
		return fmt.Errorf("consolidation insert note: %w", err)
	}

	// Advance the consolidated cursor to the last message we processed.
	if err := c.DB.SetLastConsolidatedID(ctx, sessionKey, lastMsgID); err != nil {
		return fmt.Errorf("consolidation update cursor: %w", err)
	}

	log.Printf("consolidated %d messages for session %q into memory note", len(msgs), sessionKey)
	return nil
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
