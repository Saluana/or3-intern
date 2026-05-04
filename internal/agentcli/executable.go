package agentcli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// ResolveExecutable resolves a runner binary against the environment that will
// be passed to the child process, not only the service process PATH.
func ResolveExecutable(binary string, env []string) (string, error) {
	binary = strings.TrimSpace(binary)
	if binary == "" {
		return "", exec.ErrNotFound
	}
	if filepath.IsAbs(binary) || strings.ContainsAny(binary, `/\`) {
		return exec.LookPath(binary)
	}

	pathValue := envValue(env, "PATH")
	if strings.TrimSpace(pathValue) == "" {
		if env != nil {
			return "", fmt.Errorf("%w: %s", exec.ErrNotFound, binary)
		}
		return exec.LookPath(binary)
	}
	for _, dir := range filepath.SplitList(pathValue) {
		dir = strings.TrimSpace(dir)
		if dir == "" {
			continue
		}
		for _, candidate := range executableCandidates(filepath.Join(dir, binary), env) {
			if isExecutableFile(candidate) {
				return candidate, nil
			}
		}
	}
	return "", fmt.Errorf("%w: %s", exec.ErrNotFound, binary)
}

func envValue(env []string, key string) string {
	for _, raw := range env {
		k, v, ok := strings.Cut(raw, "=")
		if ok && strings.EqualFold(k, key) {
			return v
		}
	}
	return ""
}

func executableCandidates(path string, env []string) []string {
	if runtime.GOOS != "windows" || filepath.Ext(path) != "" {
		return []string{path}
	}
	pathext := envValue(env, "PATHEXT")
	if strings.TrimSpace(pathext) == "" {
		pathext = ".COM;.EXE;.BAT;.CMD"
	}
	parts := strings.Split(pathext, ";")
	candidates := make([]string, 0, len(parts)+1)
	candidates = append(candidates, path)
	for _, ext := range parts {
		ext = strings.TrimSpace(ext)
		if ext == "" {
			continue
		}
		if !strings.HasPrefix(ext, ".") {
			ext = "." + ext
		}
		candidates = append(candidates, path+ext)
	}
	return candidates
}

func isExecutableFile(path string) bool {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false
	}
	if runtime.GOOS == "windows" {
		return true
	}
	return info.Mode()&0o111 != 0
}
