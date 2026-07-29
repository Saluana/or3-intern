package connect

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func DefaultStateDir() string {
	if override := strings.TrimSpace(os.Getenv("OR3_CONNECT_HOME")); override != "" {
		return override
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".or3-intern", "connect")
}

func StatePath(dir string) string {
	return filepath.Join(dir, "state.json")
}

func TunnelTokenPath(dir string) string {
	return filepath.Join(dir, "cloudflared.token")
}

func LoadState(dir string) (State, error) {
	body, err := os.ReadFile(StatePath(dir))
	if err != nil {
		return State{}, err
	}
	var state State
	if err := json.Unmarshal(body, &state); err != nil {
		return State{}, fmt.Errorf("read remote connection state: %w", err)
	}
	if state.Version != StateVersion {
		return State{}, fmt.Errorf("unsupported remote connection state version %d", state.Version)
	}
	return state, nil
}

func SaveState(dir string, state State, tunnelToken string) error {
	if strings.TrimSpace(tunnelToken) == "" {
		return errors.New("tunnel credential is missing")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return err
	}
	tokenPath := TunnelTokenPath(dir)
	if err := atomicWrite(tokenPath, []byte(strings.TrimSpace(tunnelToken)+"\n"), 0o600); err != nil {
		return fmt.Errorf("save tunnel credential: %w", err)
	}
	return writeState(dir, state, tokenPath)
}

func UpdateState(dir string, state State) error {
	tokenPath := TunnelTokenPath(dir)
	if _, err := os.Stat(tokenPath); err != nil {
		return fmt.Errorf("load tunnel credential: %w", err)
	}
	return writeState(dir, state, tokenPath)
}

func writeState(dir string, state State, tokenPath string) error {
	state.Version = StateVersion
	state.TunnelTokenFile = tokenPath
	body, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	if err := atomicWrite(StatePath(dir), append(body, '\n'), 0o600); err != nil {
		return fmt.Errorf("save remote connection state: %w", err)
	}
	return nil
}

func RemoveState(dir string) error {
	for _, path := range []string{StatePath(dir), TunnelTokenPath(dir)} {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

func atomicWrite(path string, body []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".or3-connect-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(body); err != nil {
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
	return os.Rename(tmpPath, path)
}
