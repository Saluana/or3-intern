package main

import (
	"context"
	"time"

	"or3-intern/internal/app"
	"or3-intern/internal/approval"
	"or3-intern/internal/bus"
	"or3-intern/internal/config"
	"or3-intern/internal/cron"
	"or3-intern/internal/cronrunner"
	"or3-intern/internal/db"
	"or3-intern/internal/jobs"
	"or3-intern/internal/memory"
	"or3-intern/internal/providers"
	"or3-intern/internal/runners"
)

func buildServiceJobRegistry(cmd string) *jobs.Registry {
	if cmd != "service" {
		return nil
	}
	return jobs.NewRegistry(0, 0)
}

func buildRuntimeRunnerManager(cfg config.Config, database *db.DB, jobs *jobs.Registry) *runners.Manager {
	return &runners.Manager{
		DB:                          database,
		Jobs:                        jobs,
		Cfg:                         cfg.Runners,
		OpenCodeExternalDirectories: runners.OpenCodeExternalDirectoriesFromConfig(cfg),
		MaxConcurrent:               cfg.Runners.MaxConcurrent,
		MaxQueued:                   cfg.Runners.MaxQueued,
		TaskTimeout:                 time.Duration(cfg.Runners.DefaultTimeoutSeconds) * time.Second,
		Registry:                    runners.NewDefaultRegistry(),
		Runtimes:                    runners.NewDefaultRuntimeRegistry(),
		RestrictDir:                 allowedRoot(cfg),
	}
}

func startRuntimeRunnerManager(ctx context.Context, manager *runners.Manager) error {
	if manager == nil {
		return nil
	}
	return manager.Start(ctx)
}

func buildRuntimeCronService(cfg config.Config, events *bus.Bus, runnerManager *runners.Manager, turnOrchestrator *app.RunnerTurnOrchestrator) *cron.Service {
	if !cfg.Cron.Enabled {
		return nil
	}
	return cron.New(cfg.Cron.StorePath, cronrunner.NewWithPreparer(events, cfg.DefaultSessionKey, runnerManager, turnOrchestrator))
}

func buildRuntimeChatManager(cfg config.Config, database *db.DB, manager *runners.Manager, jobs *jobs.Registry, broker *approval.Broker) *runners.ChatManager {
	if manager == nil || database == nil {
		return nil
	}
	return &runners.ChatManager{DB: database, Manager: manager, Jobs: jobs, Broker: broker}
}

func buildRunnerTurnOrchestrator(cfg config.Config, chatManager *runners.ChatManager, database *db.DB, mem *memory.Retriever, docs *memory.DocRetriever, embed *providers.Client) *app.RunnerTurnOrchestrator {
	embedRole := cfg.ModelRole(config.ModelRoleEmbeddings)
	deps := app.RunnerContextDeps{
		DB:               database,
		Mem:              mem,
		Docs:             docs,
		Embed:            embed,
		EmbedModel:       embedRole.Primary.Model,
		EmbedFingerprint: currentEmbedFingerprint(cfg),
		VectorK:          cfg.VectorK,
		FTSK:             cfg.FTSK,
		TopK:             cfg.MemoryRetrieve,
		DocLimit:         0,
		Cache:            runners.NewRunnerContextCache(0),
	}
	return app.NewRunnerTurnOrchestrator(cfg, chatManager, app.LoadRunnerBootstrapContext(cfg), deps)
}
