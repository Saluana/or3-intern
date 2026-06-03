package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	"or3-intern/internal/config"
)

func TestRunTurnPrefersOrchestratorWhenConfigured(t *testing.T) {
	app := &ServiceApp{
		cfg:              config.Default(),
		turnOrchestrator: &RunnerTurnOrchestrator{cfg: config.Default()},
	}
	_, err := app.RunTurn(context.Background(), TurnRequest{SessionKey: "s", Message: "hi"})
	if err == nil {
		t.Fatal("expected orchestrator failure without chat manager")
	}
	if !strings.Contains(err.Error(), "orchestrator") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunTurnFailsClosedWhenRunnerFirstHasNoOrchestrator(t *testing.T) {
	cfg := config.Default()
	cfg.AgentCLI.Enabled = true
	app := &ServiceApp{cfg: cfg}
	_, err := app.RunTurn(context.Background(), TurnRequest{SessionKey: "s", Message: "hi"})
	if !errors.Is(err, ErrRunnerTurnsDisabled) {
		t.Fatalf("expected runner turns disabled error, got %v", err)
	}
}

func TestNewRunnerTurnOrchestratorRequiresChatManager(t *testing.T) {
	if o := NewRunnerTurnOrchestrator(config.Default(), nil, RunnerBootstrapContext{}, RunnerContextDeps{}); o != nil {
		t.Fatal("expected nil orchestrator without chat manager")
	}
}
