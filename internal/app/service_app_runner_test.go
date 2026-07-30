package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	"or3-intern/internal/config"
	"or3-intern/internal/runners"
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
	app := &ServiceApp{cfg: cfg}
	_, err := app.RunTurn(context.Background(), TurnRequest{SessionKey: "s", Message: "hi"})
	if !errors.Is(err, ErrRunnerRuntimeUnavailable) {
		t.Fatalf("expected runner runtime unavailable error, got %v", err)
	}
}

func TestServiceAppPrepareRunnerRunUsesOrchestratorCompiler(t *testing.T) {
	cfg := config.Default()
	compiler := NewRunnerPromptCompiler(cfg, RunnerBootstrapContext{Soul: "service soul"}, RunnerContextDeps{})
	app := &ServiceApp{
		turnOrchestrator: &RunnerTurnOrchestrator{promptCompiler: compiler},
	}
	req := app.turnOrchestrator.PrepareRunnerRunRequest(context.Background(), runners.RunnerRunRequest{Task: "do work"})
	if !strings.Contains(req.Task, "service soul") {
		t.Fatalf("expected compiled OR3 context from service path, got %q", req.Task)
	}
}

func TestNewRunnerTurnOrchestratorRequiresChatManager(t *testing.T) {
	if o := NewRunnerTurnOrchestrator(config.Default(), nil, RunnerBootstrapContext{}, RunnerContextDeps{}); o != nil {
		t.Fatal("expected nil orchestrator without chat manager")
	}
}
