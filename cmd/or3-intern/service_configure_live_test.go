package main

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"or3-intern/internal/app"
	"or3-intern/internal/config"
	"or3-intern/internal/db"
	"or3-intern/internal/jobs"
	"or3-intern/internal/runnerfirst"
)

func TestApplyLiveConfigUpdatesRunnerFirstFlag(t *testing.T) {
	previous := runnerfirst.Enabled()
	t.Cleanup(func() { runnerfirst.SetEnabled(previous) })

	srv := &serviceServer{}
	enabled := config.Config{AgentCLI: config.AgentCLIConfig{Enabled: true}}
	srv.applyLiveConfig(enabled)
	if !runnerfirst.Enabled() {
		t.Fatal("expected runner-first flag enabled after live config apply")
	}

	disabled := config.Config{AgentCLI: config.AgentCLIConfig{Enabled: false}}
	srv.applyLiveConfig(disabled)
	if runnerfirst.Enabled() {
		t.Fatal("expected runner-first flag disabled after live config apply")
	}
}

func TestApplyLiveConfigRefreshesRunnerRuntime(t *testing.T) {
	previous := runnerfirst.Enabled()
	t.Cleanup(func() { runnerfirst.SetEnabled(previous) })

	database, err := db.Open(filepath.Join(t.TempDir(), "service.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })

	cfg := config.Default()
	cfg.AgentCLI.DefaultRunner = "opencode"
	jobs := jobs.NewRegistry(time.Minute, 16)
	srv := &serviceServer{
		config:   cfg,
		database: database,
		jobs:     jobs,
		appSvc:   app.NewServiceAppWithRunnerTurns(cfg, jobs, nil, nil, nil),
	}
	srv.applyLiveConfig(cfg)
	if srv.agentCLIManager == nil || srv.chatManager == nil || srv.turnOrchestrator == nil {
		t.Fatalf("expected live runner runtime, got manager=%v chat=%v orchestrator=%v", srv.agentCLIManager, srv.chatManager, srv.turnOrchestrator)
	}
	t.Cleanup(func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = srv.agentCLIManager.Stop(stopCtx)
	})

	next := cfg
	next.AgentCLI.DefaultRunner = "codex"
	next.AgentCLI.DisabledRunners = []string{"gemini"}
	srv.applyLiveConfig(next)
	if got := srv.agentCLIManager.Cfg.DefaultRunner; got != "codex" {
		t.Fatalf("expected default runner refreshed, got %q", got)
	}
	if len(srv.agentCLIManager.Cfg.DisabledRunners) != 1 || srv.agentCLIManager.Cfg.DisabledRunners[0] != "gemini" {
		t.Fatalf("expected disabled runners refreshed, got %#v", srv.agentCLIManager.Cfg.DisabledRunners)
	}
}
