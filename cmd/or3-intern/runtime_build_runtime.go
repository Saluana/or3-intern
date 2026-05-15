package main

import (
	"context"
	"time"

	"or3-intern/internal/agent"
	"or3-intern/internal/agentcli"
	"or3-intern/internal/bus"
	"or3-intern/internal/config"
	"or3-intern/internal/cron"
	"or3-intern/internal/cronrunner"
	"or3-intern/internal/db"
)

func buildServiceJobRegistry(cmd string) *agent.JobRegistry {
	if cmd != "service" {
		return nil
	}
	return agent.NewJobRegistry(0, 0)
}

func buildRuntimeAgentCLIManager(cfg config.Config, database *db.DB, jobs *agent.JobRegistry) *agentcli.Manager {
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
		RestrictDir:                 allowedRoot(cfg),
	}
}

func startRuntimeAgentCLIManager(ctx context.Context, manager *agentcli.Manager) error {
	if manager == nil {
		return nil
	}
	return manager.Start(ctx)
}

func buildRuntimeCronService(cfg config.Config, events *bus.Bus, agentCLIManager *agentcli.Manager) *cron.Service {
	if !cfg.Cron.Enabled {
		return nil
	}
	return cron.New(cfg.Cron.StorePath, cronrunner.New(events, cfg.DefaultSessionKey, agentCLIManager))
}
