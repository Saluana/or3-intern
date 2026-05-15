package main

import (
	"context"
	"log"
	"path/filepath"

	"or3-intern/internal/config"
	"or3-intern/internal/mcp"
	"or3-intern/internal/skills"
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

func buildRuntimeSkillsInventory(ctx context.Context, cfg config.Config, cfgPath string, manager *mcp.Manager) skills.Inventory {
	builtin := filepath.Join(filepath.Dir(cfgPathOrDefault(cfgPath)), "builtin_skills")
	toolNames := loadAvailableToolNamesWithManager(ctx, cfg, manager)
	return buildSkillsInventory(cfg, builtin, toolNames)
}
