// Package config defines the persisted runtime configuration and validation
// rules for or3-intern.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

func Save(path string, cfg Config) error {
	if path == "" {
		path = DefaultPath()
	}
	normalizeProviderRouting(&cfg)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := marshalJSON(cfg)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return err
	}
	return os.Chmod(path, 0o600)
}

func marshalJSON(v any) ([]byte, error) {
	return json.MarshalIndent(v, "", "  ")
}
