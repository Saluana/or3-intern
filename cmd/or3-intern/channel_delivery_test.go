package main

import (
	"context"
	"testing"

	"or3-intern/internal/app"
	"or3-intern/internal/bus"
	rootchannels "or3-intern/internal/channels"
	"or3-intern/internal/config"
	"or3-intern/internal/db"
	"or3-intern/internal/runners"
)

func TestDeliverChannelTurnResultSendsFinalRunnerText(t *testing.T) {
	ctx := context.Background()
	database := openChannelCommandTestDB(t)
	sess, err := database.CreateOrGetRunnerChatSession(ctx, db.RunnerChatSession{
		ID:               "rcs-telegram",
		AppSessionKey:    "telegram:123",
		RunnerID:         string(runners.RunnerOpenCode),
		ContinuationMode: string(runners.ContinuationReplay),
	})
	if err != nil {
		t.Fatalf("CreateOrGetRunnerChatSession: %v", err)
	}
	turn, err := database.CreateRunnerChatTurn(ctx, db.RunnerChatTurn{
		ID:               "rct-telegram",
		SessionID:        sess.ID,
		Status:           db.RunnerChatTurnStatusQueued,
		UserMessage:      "Hey",
		ContinuationMode: string(runners.ContinuationReplay),
	})
	if err != nil {
		t.Fatalf("CreateRunnerChatTurn: %v", err)
	}
	if err := database.FinalizeRunnerChatTurn(ctx, turn.ID, db.RunnerChatTurnFinalize{
		Status:      db.RunnerChatTurnStatusSucceeded,
		FinalText:   "Hey. What are we working on?",
		CompletedAt: db.NowMS(),
	}); err != nil {
		t.Fatalf("FinalizeRunnerChatTurn: %v", err)
	}

	channelManager := rootchannels.NewManager()
	capture := &captureChannel{name: "telegram"}
	if err := channelManager.Register(capture); err != nil {
		t.Fatalf("Register: %v", err)
	}
	turnOrchestrator := app.NewRunnerTurnOrchestrator(config.Default(), &runners.ChatManager{DB: database}, app.RunnerBootstrapContext{}, app.RunnerContextDeps{})
	deliverChannelTurnResult(ctx, channelManager, bus.Event{
		Type:       bus.EventUserMessage,
		Channel:    "telegram",
		SessionKey: "telegram:123",
		From:       "456",
		Meta:       map[string]any{"chat_id": "123", "reply_to_message_id": int64(44)},
	}, turnOrchestrator, app.RunnerTurnResult{RunnerChatSessionID: sess.ID, RunnerChatTurnID: turn.ID})

	if got := capture.lastText(); got != "Hey. What are we working on?" {
		t.Fatalf("unexpected delivered text: %q", got)
	}
	if len(capture.targets) != 1 || capture.targets[0] != "123" {
		t.Fatalf("unexpected delivery target: %#v", capture.targets)
	}
	if len(capture.metas) != 1 || capture.metas[0]["reply_to_message_id"] != int64(44) {
		t.Fatalf("expected reply metadata, got %#v", capture.metas)
	}
}

func TestChannelTurnDeliveryTextSurfacesTerminalError(t *testing.T) {
	got := channelTurnDeliveryText(app.RunnerTurnFinalResult{Status: db.RunnerChatTurnStatusFailed, ErrorMessage: "model is unavailable"})
	if got != "Runner turn failed: model is unavailable" {
		t.Fatalf("unexpected delivery text: %q", got)
	}
}
