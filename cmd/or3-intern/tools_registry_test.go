package main

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"or3-intern/internal/artifacts"
	"or3-intern/internal/channels/cli"
	"or3-intern/internal/config"
	"or3-intern/internal/db"
	"or3-intern/internal/providers"
	"or3-intern/internal/skills"
	"or3-intern/internal/tools"
)

type stubSpawnManager struct{}

func (stubSpawnManager) Enqueue(ctx context.Context, req tools.SpawnRequest) (tools.SpawnJob, error) {
	return tools.SpawnJob{ID: "job-1", ChildSessionKey: "child"}, nil
}

func TestBuildToolRegistry_ReturnsFreshToolInstances(t *testing.T) {
	cfg := config.Default()
	cfg.WorkspaceDir = t.TempDir()
	cfg.Tools.RestrictToWorkspace = true

	d, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer d.Close()

	provider := providers.New("http://example.invalid", "key", time.Second)
	channelManager, err := buildChannelManager(cfg, cli.Deliverer{}, &artifacts.Store{Dir: t.TempDir(), DB: d}, cfg.MaxMediaBytes)
	if err != nil {
		t.Fatalf("buildChannelManager: %v", err)
	}
	inv := skills.Inventory{}

	reg1 := buildToolRegistry(cfg, d, provider, channelManager, &inv, nil, stubSpawnManager{})
	reg2 := buildToolRegistry(cfg, d, provider, channelManager, &inv, nil, stubSpawnManager{})

	for _, name := range []string{"read_file", "memory_search", "send_message", "spawn_subagent"} {
		tool1 := reg1.Get(name)
		tool2 := reg2.Get(name)
		if tool1 == nil || tool2 == nil {
			t.Fatalf("expected tool %q in both registries", name)
		}
		if fmt.Sprintf("%p", tool1) == fmt.Sprintf("%p", tool2) {
			t.Fatalf("expected fresh instance for %q", name)
		}
	}
}
