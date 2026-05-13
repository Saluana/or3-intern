package main

import (
	"fmt"
	"os"
	"path/filepath"

	"or3-intern/internal/agent"
	"or3-intern/internal/config"
	"or3-intern/internal/db"
)

func prepareRuntimeStorage(cfg config.Config) error {
	if err := os.MkdirAll(filepath.Dir(cfg.DBPath), 0o755); err != nil {
		return fmt.Errorf("mkdir db dir: %w", err)
	}
	if err := os.MkdirAll(cfg.ArtifactsDir, 0o755); err != nil {
		return fmt.Errorf("mkdir artifacts dir: %w", err)
	}
	if err := ensureFileIfMissing(cfg.SoulFile, agent.DefaultSoul); err != nil {
		return fmt.Errorf("bootstrap soul file: %w", err)
	}
	if err := ensureFileIfMissing(cfg.AgentsFile, agent.DefaultAgentInstructions); err != nil {
		return fmt.Errorf("bootstrap agents file: %w", err)
	}
	if err := ensureFileIfMissing(cfg.ToolsFile, agent.DefaultToolNotes); err != nil {
		return fmt.Errorf("bootstrap tools file: %w", err)
	}
	if cfg.IdentityFile != "" {
		_ = ensureFileIfMissing(cfg.IdentityFile, "# Identity\n")
	}
	if cfg.MemoryFile != "" {
		_ = ensureFileIfMissing(cfg.MemoryFile, "# Static Memory\n")
	}
	return nil
}

func openRuntimeDatabase(cfg config.Config) (*db.DB, error) {
	opened, err := db.Open(cfg.DBPath)
	if err != nil {
		return nil, fmt.Errorf("db: %w", err)
	}
	return opened, nil
}
