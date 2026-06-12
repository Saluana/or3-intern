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
)

func TestApplyLiveConfigRefreshesRunnerRuntime(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "service.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })

	cfg := config.Default()
	cfg.Runners.Default = "opencode"
	jobs := jobs.NewRegistry(time.Minute, 16)
	srv := &serviceServer{
		config:   cfg,
		database: database,
		jobs:     jobs,
		appSvc:   app.NewServiceAppWithRunnerTurns(cfg, jobs, nil, nil, nil),
	}
	srv.applyLiveConfig(cfg)
	if srv.runnerManager == nil || srv.chatManager == nil || srv.turnOrchestrator == nil {
		t.Fatalf("expected live runner runtime, got manager=%v chat=%v orchestrator=%v", srv.runnerManager, srv.chatManager, srv.turnOrchestrator)
	}
	t.Cleanup(func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = srv.runnerManager.Stop(stopCtx)
	})

	next := cfg
	next.Runners.Default = "codex"
	next.Runners.Disabled = []string{"gemini"}
	srv.applyLiveConfig(next)
	if got := srv.runnerManager.Cfg.Default; got != "codex" {
		t.Fatalf("expected default runner refreshed, got %q", got)
	}
	if len(srv.runnerManager.Cfg.Disabled) != 1 || srv.runnerManager.Cfg.Disabled[0] != "gemini" {
		t.Fatalf("expected disabled runners refreshed, got %#v", srv.runnerManager.Cfg.Disabled)
	}
}
