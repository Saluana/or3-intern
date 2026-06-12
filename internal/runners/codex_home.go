package runners

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"or3-intern/internal/config"
)

var codexSharedDirectories = []string{
	"sessions",
	"archived_sessions",
	"sqlite",
	"shell_snapshots",
	"worktrees",
	"skills",
	"plugins",
	"cache",
	"logs",
}

var codexPrivateEntries = map[string]bool{
	"auth.json":         true,
	"models_cache.json": true,
}

var codexShadowLocalEntries = map[string]bool{
	"log":      true,
	"memories": true,
	"tmp":      true,
}

type codexHomeLayout struct {
	sharedHome    string
	effectiveHome string
	shadow        bool
}

func codexHomeEnv(cfg config.RunnersConfig) (map[string]string, error) {
	layout, ok, err := resolveCodexHomeLayout(cfg)
	if err != nil || !ok {
		return nil, err
	}
	if layout.shadow {
		if err := materializeCodexShadowHome(layout); err != nil {
			return nil, err
		}
	}
	return map[string]string{"CODEX_HOME": layout.effectiveHome}, nil
}

func resolveCodexHomeLayout(cfg config.RunnersConfig) (codexHomeLayout, bool, error) {
	home := strings.TrimSpace(cfg.CodexHomePath)
	shadow := strings.TrimSpace(cfg.CodexShadowHomePath)
	if home == "" && shadow == "" {
		return codexHomeLayout{}, false, nil
	}
	if home == "" {
		return codexHomeLayout{}, false, fmt.Errorf("runners.codexShadowHomePath requires runners.codexHomePath")
	}
	sharedHome, err := expandHomePath(home)
	if err != nil {
		return codexHomeLayout{}, false, err
	}
	if shadow == "" {
		return codexHomeLayout{sharedHome: sharedHome, effectiveHome: sharedHome}, true, nil
	}
	shadowHome, err := expandHomePath(shadow)
	if err != nil {
		return codexHomeLayout{}, false, err
	}
	if sharedHome == shadowHome {
		return codexHomeLayout{}, false, fmt.Errorf("runners.codexShadowHomePath must be different from runners.codexHomePath")
	}
	return codexHomeLayout{sharedHome: sharedHome, effectiveHome: shadowHome, shadow: true}, true, nil
}

func expandHomePath(value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", nil
	}
	if trimmed == "~" || strings.HasPrefix(trimmed, "~/") {
		home, err := os.UserHomeDir()
		if err != nil || home == "" {
			return "", fmt.Errorf("resolve home directory: %w", err)
		}
		if trimmed == "~" {
			trimmed = home
		} else {
			trimmed = filepath.Join(home, strings.TrimPrefix(trimmed, "~/"))
		}
	}
	abs, err := filepath.Abs(trimmed)
	if err != nil {
		return "", err
	}
	return filepath.Clean(abs), nil
}

func materializeCodexShadowHome(layout codexHomeLayout) error {
	if err := os.MkdirAll(layout.sharedHome, 0o700); err != nil {
		return fmt.Errorf("create Codex shared home: %w", err)
	}
	if err := os.MkdirAll(layout.effectiveHome, 0o700); err != nil {
		return fmt.Errorf("create Codex shadow home: %w", err)
	}
	for _, directory := range codexSharedDirectories {
		if err := os.MkdirAll(filepath.Join(layout.sharedHome, directory), 0o700); err != nil {
			return fmt.Errorf("create Codex shared directory %q: %w", directory, err)
		}
	}
	entries, err := os.ReadDir(layout.sharedHome)
	if err != nil {
		return fmt.Errorf("read Codex shared home: %w", err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if codexPrivateEntries[name] || codexShadowLocalEntries[name] {
			if err := removeShadowSymlink(filepath.Join(layout.effectiveHome, name)); err != nil {
				return err
			}
			continue
		}
		if err := ensureCodexShadowSymlink(filepath.Join(layout.sharedHome, name), filepath.Join(layout.effectiveHome, name)); err != nil {
			return err
		}
	}
	for name := range codexPrivateEntries {
		if err := removeShadowSymlink(filepath.Join(layout.effectiveHome, name)); err != nil {
			return err
		}
	}
	return nil
}

func ensureCodexShadowSymlink(target, link string) error {
	if existing, err := os.Readlink(link); err == nil {
		if existing == target {
			return nil
		}
		if err := os.Remove(link); err != nil {
			return fmt.Errorf("replace Codex shadow link %q: %w", link, err)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("cannot create Codex shadow home because %q already exists and is not a symlink", link)
	}
	if err := os.Symlink(target, link); err != nil {
		return fmt.Errorf("create Codex shadow link %q: %w", link, err)
	}
	return nil
}

func removeShadowSymlink(link string) error {
	if _, err := os.Readlink(link); err == nil {
		if err := os.Remove(link); err != nil {
			return fmt.Errorf("remove private Codex shadow symlink %q: %w", link, err)
		}
	} else if !os.IsNotExist(err) {
		if _, statErr := os.Lstat(link); statErr == nil {
			return nil
		}
		return fmt.Errorf("inspect private Codex shadow entry %q: %w", link, err)
	}
	return nil
}
