package tools

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"or3-intern/internal/approval"
	"or3-intern/internal/db"
	"or3-intern/internal/skills"
)

type RunSkillScript struct {
	Base
	Inventory         *skills.Inventory
	Enabled           bool
	Timeout           time.Duration
	ChildEnvAllowlist []string
	Sandbox           BubblewrapConfig
	OutputMaxBytes    int
	ApprovalBroker    *approval.Broker
	DB                *db.DB
	SensitiveDataKey  []byte
}

func (t *RunSkillScript) Capability() CapabilityLevel { return CapabilityPrivileged }

func (t *RunSkillScript) Name() string { return "run_skill_script" }

func (t *RunSkillScript) Description() string {
	return "Legacy low-level skill execution tool. Runs an approved skill-local script or declared entrypoint without shell interpolation. Prefer run_skill for plan-based approval and resumable execution."
}

func (t *RunSkillScript) Parameters() map[string]any {
	return skillRunParameters()
}

func (t *RunSkillScript) Schema() map[string]any {
	return t.SchemaFor(t.Name(), t.Description(), t.Parameters())
}

func (t *RunSkillScript) Execute(ctx context.Context, params map[string]any) (string, error) {
	return t.executeNamed(ctx, params, t.Name())
}

func (t *RunSkillScript) commandForSkill(skill skills.SkillMeta, params map[string]any) ([]string, error) {
	entrypoint := strings.TrimSpace(fmt.Sprint(params["entrypoint"]))
	if entrypoint == "<nil>" {
		entrypoint = ""
	}
	if entrypoint != "" {
		for _, candidate := range skill.Entrypoints {
			if candidate.Name != entrypoint {
				continue
			}
			cmd, err := t.entrypointCommand(skill, candidate)
			if err != nil {
				return nil, err
			}
			return append(cmd, stringArgs(params["args"])...), nil
		}
		return nil, fmt.Errorf("entrypoint not found: %s", entrypoint)
	}

	relPath := strings.TrimSpace(fmt.Sprint(params["path"]))
	if relPath == "<nil>" {
		relPath = ""
	}
	if relPath == "" {
		return nil, fmt.Errorf("missing path or entrypoint")
	}
	resolved, err := t.Inventory.ResolveBundlePath(skill.Name, relPath)
	if err != nil {
		return nil, err
	}
	base, err := scriptCommand(resolved)
	if err != nil {
		return nil, err
	}
	return append(base, stringArgs(params["args"])...), nil
}

func (t *RunSkillScript) entrypointCommand(skill skills.SkillMeta, entry skills.SkillEntry) ([]string, error) {
	if len(entry.Command) == 0 {
		return nil, fmt.Errorf("entrypoint has no command: %s", entry.Name)
	}
	cmd := make([]string, 0, len(entry.Command))
	for _, token := range entry.Command {
		token = strings.ReplaceAll(token, "{baseDir}", skill.Dir)
		cmd = append(cmd, token)
	}
	if len(cmd) == 0 {
		return nil, fmt.Errorf("entrypoint has no command: %s", entry.Name)
	}
	if strings.HasPrefix(cmd[0], ".") || strings.Contains(cmd[0], string(filepath.Separator)) {
		resolved, err := t.Inventory.ResolveBundlePath(skill.Name, cmd[0])
		if err != nil {
			return nil, err
		}
		cmd[0] = resolved
	}
	return cmd, nil
}

func scriptCommand(path string) ([]string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.IsDir() {
		return nil, fmt.Errorf("bundle path is a directory: %s", path)
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".sh":
		return []string{"bash", path}, nil
	case ".py":
		if _, err := exec.LookPath("python3"); err == nil {
			return []string{"python3", path}, nil
		}
		if _, err := exec.LookPath("python"); err == nil {
			return []string{"python", path}, nil
		}
		return nil, fmt.Errorf("python interpreter not found")
	default:
		if info.Mode()&0o111 != 0 {
			return []string{path}, nil
		}
		return nil, fmt.Errorf("unsupported script type: %s", filepath.Ext(path))
	}
}

func stringArgs(raw any) []string {
	values, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		text := strings.TrimSpace(fmt.Sprint(value))
		if text != "" {
			out = append(out, text)
		}
	}
	return out
}

func skillCommandHash(cmd []string) string {
	if len(cmd) == 0 {
		return ""
	}
	if scriptPath := skillCommandHashSource(cmd); scriptPath != "" {
		if blob, readErr := os.ReadFile(scriptPath); readErr == nil {
			sum := sha256.Sum256(blob)
			return fmt.Sprintf("%x", sum[:])
		}
	}
	sum := sha256.Sum256([]byte(strings.Join(cmd, "\x00")))
	return fmt.Sprintf("%x", sum[:])
}

func skillCommandHashSource(cmd []string) string {
	for _, token := range cmd {
		token = strings.TrimSpace(token)
		if token == "" {
			continue
		}
		if !hasPathSeparator(token) && filepath.Ext(token) == "" {
			continue
		}
		info, err := os.Stat(token)
		if err != nil || info.IsDir() {
			continue
		}
		return token
	}
	return ""
}
