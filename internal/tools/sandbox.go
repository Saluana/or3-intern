package tools

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

type BubblewrapConfig struct {
	Enabled        bool
	BubblewrapPath string
	AllowNetwork   bool
	WritablePaths  []string
}

func commandWithSandbox(ctx context.Context, cfg BubblewrapConfig, cwd string, command []string) (*exec.Cmd, error) {
	if !cfg.Enabled {
		return nil, nil
	}
	if len(command) == 0 || strings.TrimSpace(command[0]) == "" {
		return nil, fmt.Errorf("sandbox command missing executable")
	}
	bwrap := strings.TrimSpace(cfg.BubblewrapPath)
	if bwrap == "" {
		bwrap = "bwrap"
	}
	if _, err := exec.LookPath(bwrap); err != nil {
		return nil, fmt.Errorf("bubblewrap unavailable: %w", err)
	}
	args := []string{"--die-with-parent", "--new-session", "--proc", "/proc", "--dev", "/dev", "--ro-bind", "/", "/"}
	if !cfg.AllowNetwork {
		args = append(args, "--unshare-net")
	}
	for _, path := range cfg.WritablePaths {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		clean := filepath.Clean(path)
		args = append(args, "--bind", clean, clean)
	}
	if strings.TrimSpace(cwd) != "" {
		cleanCWD := filepath.Clean(cwd)
		args = append(args, "--bind", cleanCWD, cleanCWD, "--chdir", cleanCWD)
	}
	args = append(args, "--")
	args = append(args, command...)
	return exec.CommandContext(ctx, bwrap, args...), nil
}
