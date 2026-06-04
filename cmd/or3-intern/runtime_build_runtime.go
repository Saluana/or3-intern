package main

import (
	"context"
	"time"

	"or3-intern/internal/agentcli"
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
)

func buildServiceJobRegistry(cmd string) *jobs.Registry {
	if cmd != "service" {
		return nil
	}
	return jobs.NewRegistry(0, 0)
}

func buildRuntimeAgentCLIManager(cfg config.Config, database *db.DB, jobs *jobs.Registry) *agentcli.Manager {
	if !cfg.AgentCLI.Enabled {
		return nil
	}
	return &agentcli.Manager{
		DB:                          database,
		Jobs:                        jobs,
		Cfg:                         cfg.AgentCLI,
		OpenCodeExternalDirectories: agentcli.OpenCodeExternalDirectoriesFromConfig(cfg),
		MaxConcurrent:               cfg.AgentCLI.MaxConcurrent,
		MaxQueued:                   cfg.AgentCLI.MaxQueued,
		TaskTimeout:                 time.Duration(cfg.AgentCLI.DefaultTimeoutSeconds) * time.Second,
		Registry:                    agentcli.NewDefaultRegistry(),
		Runtimes:                    agentcli.NewDefaultRuntimeRegistry(),
		RestrictDir:                 allowedRoot(cfg),
	}
}

func startRuntimeAgentCLIManager(ctx context.Context, manager *agentcli.Manager) error {
	if manager == nil {
		return nil
	}
	return manager.Start(ctx)
}

func buildRuntimeCronService(cfg config.Config, events *bus.Bus, agentCLIManager *agentcli.Manager, turnOrchestrator *app.RunnerTurnOrchestrator) *cron.Service {
	if !cfg.Cron.Enabled {
		return nil
	}
	return cron.New(cfg.Cron.StorePath, cronrunner.NewWithPreparer(events, cfg.DefaultSessionKey, agentCLIManager, turnOrchestrator, cfg.AgentCLI.Enabled))
}

func buildRuntimeChatManager(cfg config.Config, database *db.DB, manager *agentcli.Manager, jobs *jobs.Registry, broker *approval.Broker) *agentcli.ChatManager {
	if manager == nil || database == nil {
		return nil
	}
	return &agentcli.ChatManager{DB: database, Manager: manager, Jobs: jobs, Broker: broker}
}

func buildRunnerTurnOrchestrator(cfg config.Config, chatManager *agentcli.ChatManager, database *db.DB, mem *memory.Retriever, docs *memory.DocRetriever, embed *providers.Client) *app.RunnerTurnOrchestrator {
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
		DocLimit:         cfg.DocIndex.RetrieveLimit,
		Cache:            agentcli.NewRunnerContextCache(0),
	}
	return app.NewRunnerTurnOrchestrator(cfg, chatManager, app.LoadRunnerBootstrapContext(cfg), deps)
}
