package main

import (
	"context"
	"log"

	"or3-intern/internal/config"
	"or3-intern/internal/mcp"
)

func buildRuntimeMCPManager(ctx context.Context, cfg config.Config) *mcp.Manager {
	if len(cfg.Tools.MCPServers) == 0 {
		return nil
	}
	manager := mcp.NewManager(cfg.Tools.MCPServers)
	manager.SetLogger(log.Printf)
	manager.SetHostPolicy(buildHostPolicy(cfg))
	if err := manager.Connect(ctx); err != nil {
		log.Printf("mcp setup failed: %v", err)
	}
	return manager
}
