package app

import (
	"context"
	"strings"
	"testing"

	"or3-intern/internal/agentcli"
	"or3-intern/internal/bus"
	"or3-intern/internal/config"
)

func TestRunnerTurnRequestFromBusEvent(t *testing.T) {
	cfg := config.Default()
	ev := bus.Event{
		Type:       bus.EventHeartbeat,
		SessionKey: "heartbeat:default",
		Channel:    "heartbeat",
		Message:    "run tasks",
		Meta: map[string]any{
			"runner_id": "codex",
			"model":     "gpt-test",
		},
	}
	req := RunnerTurnRequestFromBusEvent(cfg, ev)
	if req.TriggerKind != "heartbeat" {
		t.Fatalf("trigger=%q", req.TriggerKind)
	}
	if req.RunnerID != "codex" {
		t.Fatalf("runner=%q", req.RunnerID)
	}
	if req.Model != "gpt-test" {
		t.Fatalf("model=%q", req.Model)
	}
}

func TestRunnerTurnRequestFromBusEventDefaultRunner(t *testing.T) {
	cfg := config.Default()
	req := RunnerTurnRequestFromBusEvent(cfg, bus.Event{Type: bus.EventUserMessage, SessionKey: "cli:default", Message: "hi"})
	if req.RunnerID != string(agentcli.RunnerOpenCode) {
		t.Fatalf("expected default runner opencode, got %q", req.RunnerID)
	}
}

func TestCompileRunnerChatPromptIncludesSoulInReplayEnvelope(t *testing.T) {
	cfg := config.Default()
	compiler := NewRunnerPromptCompiler(cfg, RunnerBootstrapContext{
		Soul:              "SOUL.md content",
		AgentInstructions: "AGENTS.md content",
	}, RunnerContextDeps{})
	o := &RunnerTurnOrchestrator{promptCompiler: compiler}
	out := o.CompileRunnerChatPrompt(context.Background(), "cli:test", "hello", "user_message", nil)
	if !strings.Contains(out.CompiledPrompt, "SOUL.md content") {
		t.Fatalf("expected soul in compiled prompt: %q", out.CompiledPrompt)
	}
	if !strings.Contains(out.CompiledPrompt, "<trusted_or3_system_instructions>") {
		t.Fatalf("expected trusted envelope: %q", out.CompiledPrompt)
	}
}

func TestBuildRunnerPromptOrderingInOrchestratorBootstrap(t *testing.T) {
	b := RunnerBootstrapContext{Soul: "soul", HeartbeatTasks: "task"}
	blocks := b.contextBlocks("heartbeat")
	if len(blocks) != 1 || !strings.Contains(blocks[0], "task") {
		t.Fatalf("expected heartbeat block, got %#v", blocks)
	}
	if len(b.contextBlocks("user_message")) != 0 {
		t.Fatal("heartbeat tasks should not appear for normal user turns")
	}
}
