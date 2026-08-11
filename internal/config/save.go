// Package config defines the persisted runtime configuration and validation
// rules for or3-intern.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"syscall"
)

var saveMu sync.Mutex

func Save(path string, cfg Config) error {
	if path == "" {
		path = DefaultPath()
	}
	path = filepath.Clean(path)
	// Normalization updates nested routing maps. Work from an independent copy so
	// persisting a snapshot never mutates configuration still in use by a caller.
	cfg = Clone(cfg)
	normalizeProviderRouting(&cfg)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := marshalJSON(cfg)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	// A config file is a recovery boundary. Serialize same-process writes and
	// replace it atomically so a crash, full disk, or reader never observes a
	// partially written JSON document.
	saveMu.Lock()
	defer saveMu.Unlock()
	return saveAtomically(path, data)
}

func saveAtomically(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".config-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}
	cleanup = false
	if err := syncConfigDirectory(dir); err != nil {
		return err
	}
	return nil
}

func syncConfigDirectory(dir string) error {
	f, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := f.Sync(); err != nil && !errors.Is(err, syscall.EINVAL) && !errors.Is(err, syscall.ENOTSUP) {
		return err
	}
	return nil
}

func marshalJSON(v any) ([]byte, error) {
	return json.MarshalIndent(v, "", "  ")
}
