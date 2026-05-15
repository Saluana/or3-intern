package db

import (
	"context"
	"strings"
	"testing"
)

func TestChatSessionStoreListRenameArchiveAndMessages(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()
	meta, err := d.UpsertChatSessionMeta(ctx, ChatSessionMeta{SessionKey: "sess-1", Title: "First", RunnerID: "or3-intern"})
	if err != nil {
		t.Fatalf("UpsertChatSessionMeta: %v", err)
	}
	if meta.Title != "First" {
		t.Fatalf("unexpected title %q", meta.Title)
	}
	mustAppendMessage(t, d, ctx, "sess-1", "user", "hello", map[string]any{"safe": true})
	page, err := d.ListChatMessages(ctx, "sess-1", 0, 10)
	if err != nil || len(page.Messages) != 1 || page.Messages[0].Content != "hello" {
		t.Fatalf("ListChatMessages got %#v err=%v", page, err)
	}
	if err := d.RenameChatSession(ctx, "sess-1", "Renamed"); err != nil {
		t.Fatalf("RenameChatSession: %v", err)
	}
	if err := d.ArchiveChatSession(ctx, "sess-1", true); err != nil {
		t.Fatalf("ArchiveChatSession: %v", err)
	}
	items, err := d.ListChatSessions(ctx, ChatSessionListFilter{IncludeArchive: true})
	if err != nil || len(items) != 1 || items[0].Title != "Renamed" || !items[0].Archived {
		t.Fatalf("ListChatSessions got %#v err=%v", items, err)
	}
}

func TestChatSessionForkSanitizesPayload(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()
	if _, err := d.UpsertChatSessionMeta(ctx, ChatSessionMeta{SessionKey: "source", Title: "Source", RunnerID: "codex"}); err != nil {
		t.Fatalf("UpsertChatSessionMeta: %v", err)
	}
	anchor := mustAppendMessage(t, d, ctx, "source", "assistant", "answer", map[string]any{
		"approval_token": "secret",
		"runner_output":  "raw",
		"child_env":      []string{"SECRET=1"},
		"safe":           "kept",
	})
	meta, copied, err := d.ForkChatSession(ctx, ForkChatSessionRequest{
		SourceSessionKey: "source",
		NewSessionKey:    "forked",
		AnchorMessageID:  anchor,
		TargetRunnerID:   "opencode",
		Title:            "Forked",
	})
	if err != nil {
		t.Fatalf("ForkChatSession: %v", err)
	}
	if meta.SessionKey != "forked" || meta.ParentSessionKey != "source" || meta.RunnerID != "opencode" {
		t.Fatalf("unexpected fork meta: %#v", meta)
	}
	if len(copied) != 1 {
		t.Fatalf("expected one copied message, got %d", len(copied))
	}
	page, err := d.ListChatMessages(ctx, "forked", 0, 10)
	if err != nil || len(page.Messages) != 1 {
		t.Fatalf("ListChatMessages forked got %#v err=%v", page, err)
	}
	payload := page.Messages[0].PayloadJSON
	for _, forbidden := range []string{"approval_token", "runner_output", "child_env", "secret"} {
		if strings.Contains(strings.ToLower(payload), forbidden) {
			t.Fatalf("payload contains forbidden %q: %s", forbidden, payload)
		}
	}
	if !strings.Contains(payload, "safe") {
		t.Fatalf("safe payload key stripped: %s", payload)
	}
}
