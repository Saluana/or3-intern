package main

import (
	"context"
	"path/filepath"
	"testing"

	"or3-intern/internal/bus"
	"or3-intern/internal/config"
	"or3-intern/internal/db"
)

func openChannelCommandTestDB(t *testing.T) *db.DB {
	t.Helper()
	database, err := db.Open(filepath.Join(t.TempDir(), "channel-commands.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	return database
}

func TestParseChannelCommandAllowsTelegramBotSuffix(t *testing.T) {
	cmd, args, ok := parseChannelCommand("/runner@or3_bot opencode")
	if !ok || cmd != "runner" || len(args) != 1 || args[0] != "opencode" {
		t.Fatalf("unexpected parse result: cmd=%q args=%#v ok=%v", cmd, args, ok)
	}
}

func TestChannelCommandHandlerPersistsAndInjectsPreferences(t *testing.T) {
	ctx := context.Background()
	cfg := config.Default()
	cfg.Runners.Default = "opencode"
	database := openChannelCommandTestDB(t)
	handler := &channelCommandHandler{Config: cfg, DB: database}
	ev := bus.Event{Type: bus.EventUserMessage, Channel: "telegram", SessionKey: "telegram:123", From: "123", Message: "/runner opencode", Meta: map[string]any{"chat_id": "123"}}
	if _, handled, err := handler.Handle(ctx, ev); !handled || err != nil {
		t.Fatalf("expected /runner handled without error, handled=%v err=%v", handled, err)
	}
	meta, err := database.GetChatSessionMeta(ctx, "telegram:123")
	if err != nil {
		t.Fatalf("GetChatSessionMeta: %v", err)
	}
	if meta.RunnerID != "opencode" || meta.RunnerModel != "" {
		t.Fatalf("unexpected saved preference: %#v", meta)
	}
	next, handled, err := handler.Handle(ctx, bus.Event{Type: bus.EventUserMessage, Channel: "telegram", SessionKey: "telegram:123", Message: "hello", Meta: map[string]any{}})
	if handled || err != nil {
		t.Fatalf("expected normal turn to continue, handled=%v err=%v", handled, err)
	}
	if next.Meta["runner_id"] != "opencode" {
		t.Fatalf("expected runner metadata injection, got %#v", next.Meta)
	}
}

func TestChannelCommandHandlerResetClearsPreferences(t *testing.T) {
	ctx := context.Background()
	cfg := config.Default()
	cfg.Runners.Default = "opencode"
	database := openChannelCommandTestDB(t)
	if _, err := database.SetChatSessionRunnerPreference(ctx, "telegram:123", "opencode", "OpenCode", "gpt-5"); err != nil {
		t.Fatalf("SetChatSessionRunnerPreference: %v", err)
	}
	handler := &channelCommandHandler{Config: cfg, DB: database}
	if _, handled, err := handler.Handle(ctx, bus.Event{Type: bus.EventUserMessage, Channel: "telegram", SessionKey: "telegram:123", Message: "/reset"}); !handled || err != nil {
		t.Fatalf("expected /reset handled without error, handled=%v err=%v", handled, err)
	}
	meta, err := database.GetChatSessionMeta(ctx, "telegram:123")
	if err != nil {
		t.Fatalf("GetChatSessionMeta: %v", err)
	}
	if meta.RunnerID != "" || meta.RunnerLabel != "" || meta.RunnerModel != "" {
		t.Fatalf("expected cleared preference, got %#v", meta)
	}
}

func TestChannelCommandHandlerDoesNotInterceptApprovalCommands(t *testing.T) {
	handler := &channelCommandHandler{Config: config.Default(), DB: openChannelCommandTestDB(t)}
	_, handled, err := handler.Handle(context.Background(), bus.Event{Type: bus.EventUserMessage, Channel: "telegram", SessionKey: "telegram:123", Message: "/approve req_123"})
	if handled || err != nil {
		t.Fatalf("expected approval command to pass through, handled=%v err=%v", handled, err)
	}
}
