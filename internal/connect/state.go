package connect

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
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

func TunnelConfigPath(dir string) string {
	return filepath.Join(dir, "cloudflared.yml")
}

func TunnelCredentialsPath(dir string) string {
	return filepath.Join(dir, "cloudflared.json")
}

// RuntimeConfigBackupPath is a Connect-owned, owner-only backup of an
// external runtime configuration. The command package owns its schema; the
// state package owns its lifetime so connection cleanup cannot orphan it.
func RuntimeConfigBackupPath(dir string) string {
	return filepath.Join(dir, "runtime-config-backup.json")
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
	if state.Version == 1 {
		// Version 1 did not retain Connect-owned service settings. It remains
		// readable so existing installs can still be revoked and removed.
		state.Version = StateVersion
	}
	if state.Version != StateVersion {
		return State{}, fmt.Errorf("unsupported remote connection state version %d", state.Version)
	}
	return state, nil
}

func SaveState(dir string, state State, tunnel TunnelCredential) error {
	legacyToken := strings.TrimSpace(tunnel.Token)
	hasLocalCredential := strings.TrimSpace(tunnel.AccountTag) != "" &&
		strings.TrimSpace(tunnel.TunnelID) != "" &&
		strings.TrimSpace(tunnel.TunnelSecret) != "" &&
		strings.TrimSpace(tunnel.Hostname) != ""
	if legacyToken == "" && !hasLocalCredential {
		return errors.New("tunnel credential is missing")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return err
	}
	if hasLocalCredential {
		target := strings.TrimSpace(state.LocalOrigin)
		if target == "" {
			target = "http://127.0.0.1:9100"
		}
		if err := validateLoopbackTarget(target); err != nil {
			return fmt.Errorf("save tunnel configuration: %w", err)
		}
		credentialsPath := TunnelCredentialsPath(dir)
		credentialsBody, err := json.MarshalIndent(map[string]string{
			"AccountTag":   strings.TrimSpace(tunnel.AccountTag),
			"TunnelID":     strings.TrimSpace(tunnel.TunnelID),
			"TunnelSecret": strings.TrimSpace(tunnel.TunnelSecret),
		}, "", "  ")
		if err != nil {
			return fmt.Errorf("encode tunnel credential: %w", err)
		}
		if err := atomicWrite(credentialsPath, append(credentialsBody, '\n'), 0o600); err != nil {
			return fmt.Errorf("save tunnel credential: %w", err)
		}
		configPath := TunnelConfigPath(dir)
		configBody := fmt.Sprintf(
			"tunnel: %s\ncredentials-file: %s\ningress:\n  - hostname: %s\n    service: %s\n    originRequest:\n      httpHostHeader: 127.0.0.1\n  - service: http_status:404\n",
			strconv.Quote(strings.TrimSpace(tunnel.TunnelID)),
			strconv.Quote(credentialsPath),
			strconv.Quote(strings.TrimSpace(tunnel.Hostname)),
			target,
		)
		if err := atomicWrite(configPath, []byte(configBody), 0o600); err != nil {
			return fmt.Errorf("save tunnel configuration: %w", err)
		}
		state.TunnelConfigFile = configPath
		state.TunnelCredentialsFile = credentialsPath
		state.TunnelTokenFile = ""
		return writeState(dir, state)
	}
	tokenPath := TunnelTokenPath(dir)
	if err := atomicWrite(tokenPath, []byte(legacyToken+"\n"), 0o600); err != nil {
		return fmt.Errorf("save tunnel credential: %w", err)
	}
	state.TunnelTokenFile = tokenPath
	return writeState(dir, state)
}

func validateLoopbackTarget(value string) error {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || parsed.Scheme != "http" || parsed.Hostname() != "127.0.0.1" || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return errors.New("the runtime target must be an http://127.0.0.1 origin")
	}
	if parsed.Port() == "" {
		return errors.New("the runtime target must include a loopback port")
	}
	return nil
}

func UpdateState(dir string, state State) error {
	// Callers commonly retain the pre-save State value. Recover the durable
	// credential paths so a status-only update cannot accidentally downgrade a
	// locally managed tunnel back to the legacy token path.
	if strings.TrimSpace(state.TunnelConfigFile) == "" {
		if _, err := os.Stat(TunnelConfigPath(dir)); err == nil {
			state.TunnelConfigFile = TunnelConfigPath(dir)
			state.TunnelCredentialsFile = TunnelCredentialsPath(dir)
			state.TunnelTokenFile = ""
		}
	}
	if strings.TrimSpace(state.TunnelConfigFile) != "" {
		if _, err := os.Stat(state.TunnelConfigFile); err != nil {
			return fmt.Errorf("load tunnel configuration: %w", err)
		}
		if _, err := os.Stat(state.TunnelCredentialsFile); err != nil {
			return fmt.Errorf("load tunnel credential: %w", err)
		}
	} else {
		if _, err := os.Stat(state.TunnelTokenFile); err != nil {
			return fmt.Errorf("load tunnel credential: %w", err)
		}
	}
	return writeState(dir, state)
}

func writeState(dir string, state State) error {
	state.Version = StateVersion
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
	for _, path := range []string{
		TunnelTokenPath(dir),
		TunnelConfigPath(dir),
		TunnelCredentialsPath(dir),
		RuntimeConfigBackupPath(dir),
		// Remove state last. If an owned artifact cannot be removed, the state
		// remains as a durable retry handle instead of orphaning credentials.
		StatePath(dir),
	} {
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
