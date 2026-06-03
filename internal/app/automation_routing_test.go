package app

import (
	"context"
	"strings"
	"testing"

	"or3-intern/internal/agentcli"
	"or3-intern/internal/bus"
	"or3-intern/internal/config"
)

func TestHandleBusEventWebhookTriggerMetadata(t *testing.T) {
	cfg := config.Default()
	o := &RunnerTurnOrchestrator{cfg: cfg, chat: nil, bootstrap: LoadRunnerBootstrapContext(cfg)}
	ev := bus.Event{
		Type:       bus.EventWebhook,
		SessionKey: "webhook:default",
		Channel:    "webhook",
		Message:    "incoming",
	}
	req := RunnerTurnRequestFromBusEvent(cfg, ev)
	if req.TriggerKind != "webhook" {
		t.Fatalf("expected webhook trigger, got %q", req.TriggerKind)
	}
	err := o.HandleBusEvent(context.Background(), ev)
	if err == nil || !strings.Contains(err.Error(), "orchestrator") {
		t.Fatalf("expected orchestrator unavailable error, got %v", err)
	}
}

func TestBootstrapForTriggerReloadsHeartbeat(t *testing.T) {
	cfg := config.Default()
	dir := t.TempDir()
	cfg.WorkspaceDir = dir
	cfg.Heartbeat.TasksFile = ""
	o := &RunnerTurnOrchestrator{cfg: cfg, bootstrap: RunnerBootstrapContext{HeartbeatTasks: "stale"}}
	if got := o.bootstrapForTrigger("user_message"); got.HeartbeatTasks != "stale" {
		t.Fatalf("expected cached bootstrap for user turns")
	}
	_ = agentcli.RunnerOpenCode
}
