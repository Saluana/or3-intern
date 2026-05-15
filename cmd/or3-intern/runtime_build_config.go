package main

import (
	"os"
	"strings"

	"or3-intern/internal/config"
)

type runtimeConfigLoadResult struct {
	Config config.Config
}

func loadRuntimeConfig(cfgPath string) (runtimeConfigLoadResult, error) {
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return runtimeConfigLoadResult{}, err
	}
	applyRuntimeConfigDefaults(&cfg)
	return runtimeConfigLoadResult{Config: cfg}, nil
}

func applyRuntimeConfigDefaults(cfg *config.Config) {
	if cfg == nil {
		return
	}
	if cfg.Tools.RestrictToWorkspace && strings.TrimSpace(cfg.WorkspaceDir) == "" {
		if cwd, err := os.Getwd(); err == nil {
			cfg.WorkspaceDir = cwd
		}
	}
}

func validateRuntimeStartupCommand(cmd string, cfg config.Config, unsafeDev bool) error {
	return validateStartupCommand(cmd, cfg, unsafeDev)
}
