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

type stubMCPRegistrar struct {
	register func(reg *tools.Registry) int
}

func (s stubMCPRegistrar) RegisterTools(reg *tools.Registry) int {
	if s.register == nil {
		return 0
	}
	return s.register(reg)
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

	reg1 := buildToolRegistry(cfg, d, provider, channelManager, &inv, nil, stubSpawnManager{}, nil)
	reg2 := buildToolRegistry(cfg, d, provider, channelManager, &inv, nil, stubSpawnManager{}, nil)

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

func TestBuildToolRegistry_RegistersMCPTools(t *testing.T) {
	cfg := config.Default()
	cfg.WorkspaceDir = t.TempDir()

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

	mcpRegistrar := stubMCPRegistrar{
		register: func(reg *tools.Registry) int {
			reg.Register(&stubTool{name: "mcp_demo_echo"})
			return 1
		},
	}
	reg := buildToolRegistry(cfg, d, provider, channelManager, &inv, nil, nil, mcpRegistrar)
	if reg.Get("mcp_demo_echo") == nil {
		t.Fatal("expected MCP tool to be registered")
	}
}

func TestBuildBackgroundToolRegistry_OmitsMessagingAndSpawn(t *testing.T) {
	cfg := config.Default()
	cfg.WorkspaceDir = t.TempDir()

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

	reg := buildBackgroundToolRegistry(cfg, d, provider, channelManager, &inv, nil, nil)
	if reg.Get("send_message") != nil {
		t.Fatal("expected send_message to be omitted from background registry")
	}
	if reg.Get("spawn_subagent") != nil {
		t.Fatal("expected spawn_subagent to be omitted from background registry")
	}
	if reg.Get("read_file") == nil {
		t.Fatal("expected background registry to retain work tools")
	}
}

type stubTool struct {
	tools.Base
	name string
}

func (t *stubTool) Name() string        { return t.name }
func (t *stubTool) Description() string { return "stub" }
func (t *stubTool) Parameters() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{}}
}
func (t *stubTool) Execute(ctx context.Context, params map[string]any) (string, error) {
	return "ok", nil
}
func (t *stubTool) Schema() map[string]any {
	return t.SchemaFor(t.Name(), t.Description(), t.Parameters())
}
