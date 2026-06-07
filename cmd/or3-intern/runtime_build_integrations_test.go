package main

import (
	"context"
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
