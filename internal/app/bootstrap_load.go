package app

import (
	"os"
	"path/filepath"
	"strings"

	"or3-intern/internal/agent"
	"or3-intern/internal/config"
)

// LoadRunnerBootstrapContext reads workspace bootstrap files for runner prompts.
func LoadRunnerBootstrapContext(cfg config.Config) RunnerBootstrapContext {
	return RunnerBootstrapContext{
		Soul:              loadBootstrapFile(cfg.SoulFile, cfg.WorkspaceDir, "SOUL.md", agent.DefaultSoul),
		AgentInstructions: loadBootstrapFile(cfg.AgentsFile, cfg.WorkspaceDir, "AGENTS.md", agent.DefaultAgentInstructions),
		ToolNotes:         loadBootstrapFile(cfg.ToolsFile, cfg.WorkspaceDir, "TOOLS.md", agent.DefaultToolNotes),
		IdentityText:      loadBootstrapFile(cfg.IdentityFile, cfg.WorkspaceDir, "IDENTITY.md", ""),
		StaticMemory:      loadBootstrapFile(cfg.MemoryFile, cfg.WorkspaceDir, "MEMORY.md", ""),
		HeartbeatTasks:    loadBootstrapFile(cfg.Heartbeat.TasksFile, cfg.WorkspaceDir, "HEARTBEAT.md", ""),
	}
}

func loadBootstrapFile(configPath, workspaceDir, baseName, fallback string) string {
	paths := []string{}
	if strings.TrimSpace(workspaceDir) != "" {
		paths = append(paths,
			filepath.Join(workspaceDir, baseName),
			filepath.Join(workspaceDir, strings.ToLower(baseName)),
		)
	}
	if strings.TrimSpace(configPath) != "" {
		paths = append(paths, configPath)
	}
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err == nil && len(strings.TrimSpace(string(data))) > 0 {
			return string(data)
		}
	}
	return fallback
}
