package main

import (
	"errors"
	"testing"

	"or3-intern/internal/app"
	"or3-intern/internal/config"
)

func TestRunnerFirstRunTurnRequiresOrchestrator(t *testing.T) {
	cfg := config.Default()
	cfg.AgentCLI.Enabled = true
	svc := app.NewServiceAppWithRunnerTurns(cfg, nil, nil, nil, nil)
	_, err := svc.RunTurn(t.Context(), app.TurnRequest{SessionKey: "s", Message: "hi"})
	if !errors.Is(err, app.ErrRunnerTurnsDisabled) {
		t.Fatalf("expected ErrRunnerTurnsDisabled, got %v", err)
	}
}
