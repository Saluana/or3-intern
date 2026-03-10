package tools

import (
	"context"
	"fmt"
	"os"
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
	resolvedCommand, err := resolveExecutable(command[0], cwd)
	if err != nil {
		return nil, err
	}
	command = append([]string{resolvedCommand}, command[1:]...)

	args := []string{"--die-with-parent", "--new-session", "--unshare-pid", "--proc", "/proc", "--dev", "/dev", "--tmpfs", "/"}
	created := map[string]struct{}{}
	if !cfg.AllowNetwork {
		args = append(args, "--unshare-net")
	}
	for _, path := range sandboxReadOnlyPaths() {
		appendSandboxBind(&args, created, "--ro-bind", path, path)
	}
	for _, path := range cfg.WritablePaths {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		clean := filepath.Clean(path)
		if !filepath.IsAbs(clean) {
			return nil, fmt.Errorf("sandbox writable path must be absolute: %s", path)
		}
		appendSandboxBind(&args, created, "--bind", clean, clean)
	}
	if strings.TrimSpace(cwd) != "" {
		cleanCWD := filepath.Clean(cwd)
		if !filepath.IsAbs(cleanCWD) {
			return nil, fmt.Errorf("sandbox cwd must be absolute: %s", cwd)
		}
		if !sandboxPathCovered(cleanCWD, "", cfg.WritablePaths) {
			appendSandboxBind(&args, created, "--ro-bind", cleanCWD, cleanCWD)
		}
		args = append(args, "--chdir", cleanCWD)
	}
	commandDir := filepath.Dir(resolvedCommand)
	if !sandboxPathCovered(commandDir, cwd, cfg.WritablePaths) {
		appendSandboxBind(&args, created, "--ro-bind", commandDir, commandDir)
	}
	args = append(args, "--")
	args = append(args, command...)
	return exec.CommandContext(ctx, bwrap, args...), nil
}

func sandboxReadOnlyPaths() []string {
	return []string{
		"/bin",
		"/sbin",
		"/usr",
		"/lib",
		"/lib64",
		"/etc",
		"/opt",
		"/run/current-system/sw",
	}
}

func appendSandboxBind(args *[]string, created map[string]struct{}, bindFlag string, src string, dst string) {
	if _, err := os.Lstat(src); err != nil {
		return
	}
	appendSandboxParents(args, created, filepath.Dir(dst))
	*args = append(*args, bindFlag, src, dst)
}

func appendSandboxParents(args *[]string, created map[string]struct{}, dir string) {
	dir = filepath.Clean(dir)
	if dir == "." || dir == "/" {
		return
	}
	parts := strings.Split(strings.TrimPrefix(dir, "/"), "/")
	current := ""
	for _, part := range parts {
		if part == "" {
			continue
		}
		current = current + "/" + part
		if _, ok := created[current]; ok {
			continue
		}
		*args = append(*args, "--dir", current)
		created[current] = struct{}{}
	}
}

func sandboxPathCovered(path string, cwd string, writable []string) bool {
	path = filepath.Clean(strings.TrimSpace(path))
	if path == "" {
		return false
	}
	coveredRoots := make([]string, 0, len(writable)+1)
	if trimmed := strings.TrimSpace(cwd); trimmed != "" {
		coveredRoots = append(coveredRoots, filepath.Clean(trimmed))
	}
	for _, root := range writable {
		if trimmed := strings.TrimSpace(root); trimmed != "" {
			coveredRoots = append(coveredRoots, filepath.Clean(trimmed))
		}
	}
	for _, root := range coveredRoots {
		if rel, err := filepath.Rel(root, path); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return true
		}
	}
	return false
}
