package agent

import (
	"context"
	"strings"
	"testing"

	"or3-intern/internal/db"
)

func TestBuildWithOptions_CurrentTurnSurvivesCompaction(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()
	firstID, err := d.AppendMessage(ctx, "sess-turn", "user", "old context", nil)
	if err != nil {
		t.Fatalf("append first: %v", err)
	}
	cutoffID, err := d.AppendMessage(ctx, "sess-turn", "assistant", "old answer", nil)
	if err != nil {
		t.Fatalf("append second: %v", err)
	}
	currentID, err := d.AppendMessage(ctx, "sess-turn", "user", "current user request", nil)
	if err != nil {
		t.Fatalf("append current: %v", err)
	}
	if err := d.UpsertContextCompaction(ctx, db.ContextCompaction{
		ScopeKey:        "sess-turn",
		SessionKey:      "sess-turn",
		Summary:         "older context resolved",
		CutoffMessageID: cutoffID,
	}); err != nil {
		t.Fatalf("UpsertContextCompaction: %v", err)
	}
	_ = firstID

	b := &Builder{DB: d, HistoryMax: 20}
	pp, _, err := b.BuildWithOptions(ctx, BuildOptions{
		SessionKey:      "sess-turn",
		UserMessage:     "current user request",
		UserMessageID:   currentID,
	})
	if err != nil {
		t.Fatalf("BuildWithOptions: %v", err)
	}
	sys := systemPromptText(pp.System[0].Content)
	if !strings.Contains(sys, "<current_user_request") || !strings.Contains(sys, "current user request") {
		t.Fatalf("expected current user request envelope, got %s", sys)
	}
	for _, msg := range pp.History {
		if msg.Role == "user" && strings.Contains(contentToString(msg.Content), "current user request") {
			t.Fatalf("expected current request removed from history when rendered in current_user_request")
		}
	}
	for _, msg := range pp.History {
		if strings.Contains(contentToString(msg.Content), "old answer") {
			t.Fatalf("expected compacted history to drop old answer, got %#v", pp.History)
		}
	}
}
