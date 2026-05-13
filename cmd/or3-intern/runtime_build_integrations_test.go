package main

import (
	"context"
	"path/filepath"
	"testing"

	"or3-intern/internal/config"
)

func TestBuildRuntimeMCPManagerDisabled(t *testing.T) {
	cfg := config.Default()
	cfg.Tools.MCPServers = nil
	if manager := buildRuntimeMCPManager(context.Background(), cfg); manager != nil {
		t.Fatalf("expected nil MCP manager when no servers configured")
	}
}

func TestBuildRuntimeSkillsInventoryUsesConfigPathBuiltinDir(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "config.json")
	cfg := config.Default()
	inv := buildRuntimeSkillsInventory(context.Background(), cfg, cfgPath, nil)
	if inv.Skills == nil {
		t.Fatalf("expected initialized skills slice")
	}
}
