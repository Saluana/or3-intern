package agent

import (
	"context"
	"log"
	"strings"

)

// cleanupActiveTurnTask clears ephemeral turn state after a finished assistant turn.
// Structured plans with unfinished tasks stay active; resolved or idle work is completed.
func (r *Runtime) cleanupActiveTurnTask(ctx context.Context, sessionKey string) {
	if r == nil || r.DB == nil || strings.TrimSpace(sessionKey) == "" {
		return
	}
	if r.Builder != nil && r.Builder.DisableTaskCard {
		return
	}
	card, ok, err := loadTaskCard(ctx, r.DB, sessionKey)
	if err != nil || !ok {
		return
	}
	clearActiveTurnMetadata(&card.Metadata)
	if activePlanHasOpenWork(card.Metadata) {
		scopeKey := resolveTaskCardScope(ctx, r.DB, sessionKey, "")
		if err := saveTaskCard(ctx, r.DB, sessionKey, scopeKey, card); err != nil {
			log.Printf("cleanup active turn task (save) failed: session=%s err=%v", sessionKey, err)
		}
		return
	}
	if err := r.DB.CompleteActiveTaskState(ctx, sessionKey); err != nil {
		log.Printf("cleanup active turn task failed: session=%s err=%v", sessionKey, err)
	}
}
