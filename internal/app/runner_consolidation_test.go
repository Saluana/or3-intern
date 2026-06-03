package app

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"or3-intern/internal/db"
)

func TestRunnerChatMessagesAreEligibleForConsolidation(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "consolidate.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	ctx := context.Background()
	sessionKey := "runner:consolidate"
	payload, _ := json.Marshal(map[string]any{
		"transport":              "runner_chat",
		"runner_id":              "opencode",
		"runner_chat_session_id": "rcs_test",
		"runner_chat_turn_id":    "rct_test",
	})
	if _, err := database.AppendMessage(ctx, sessionKey, "user", "hello from runner", json.RawMessage(payload)); err != nil {
		t.Fatalf("append user: %v", err)
	}
	if _, err := database.AppendMessage(ctx, sessionKey, "assistant", "reply from runner", json.RawMessage(payload)); err != nil {
		t.Fatalf("append assistant: %v", err)
	}
	msgs, err := database.GetLastMessages(ctx, sessionKey, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) < 2 {
		t.Fatalf("expected messages for consolidation window, got %d", len(msgs))
	}
}
