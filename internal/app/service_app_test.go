package app

import (
	"context"
	"errors"
	"testing"

	"or3-intern/internal/config"
)

func TestDetectAgentCLIRunnersWithoutManager(t *testing.T) {
	svc := NewServiceAppWithAgentCLI(config.Default(), nil, nil, nil)
	runners, err := svc.DetectAgentCLIRunners(context.Background())
	if err != nil {
		t.Fatalf("DetectAgentCLIRunners: %v", err)
	}
	if len(runners) == 0 {
		t.Fatal("expected default registry to detect at least one runner without manager")
	}
}

func TestResumeApprovedRequest_DisabledInRunnerFirst(t *testing.T) {
	cfg := config.Default()
	cfg.AgentCLI.Enabled = true
	app := NewServiceApp(cfg, nil, nil)
	_, err := app.ResumeApprovedRequest(context.Background(), ResumeApprovedRequest{})
	if !errors.Is(err, ErrLegacyToolReplayDisabled) {
		t.Fatalf("expected ErrLegacyToolReplayDisabled, got %v", err)
	}
}

func TestReplayToolCall_ReturnsLegacyDisabled(t *testing.T) {
	cfg := config.Default()
	cfg.AgentCLI.Enabled = true
	app := NewServiceApp(cfg, nil, nil)
	_, err := app.ReplayToolCall(context.Background(), ReplayToolCallRequest{ToolName: "exec"})
	if !errors.Is(err, ErrLegacyToolReplayDisabled) {
		t.Fatalf("expected ErrLegacyToolReplayDisabled, got %v", err)
	}
}

func TestStartSubagent_ReturnsLegacyRemoved(t *testing.T) {
	cfg := config.Default()
	cfg.AgentCLI.Enabled = true
	app := NewServiceApp(cfg, nil, nil)
	_, err := app.StartSubagent(context.Background(), SubagentRequest{Task: "background"})
	if !errors.Is(err, ErrLegacySubagentsRemoved) {
		t.Fatalf("expected ErrLegacySubagentsRemoved, got %v", err)
	}
}
