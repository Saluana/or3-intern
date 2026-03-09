package tools

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"or3-intern/internal/skills"
)

type RunSkillScript struct {
	Base
	Inventory      *skills.Inventory
	Timeout        time.Duration
	ChildEnvAllowlist []string
	OutputMaxBytes int
}

func (t *RunSkillScript) Capability() CapabilityLevel { return CapabilityPrivileged }

func (t *RunSkillScript) Name() string { return "run_skill_script" }

func (t *RunSkillScript) Description() string {
	return "Run a skill-local script or declared entrypoint without shell interpolation."
}

func (t *RunSkillScript) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"skill":      map[string]any{"type": "string", "description": "Skill name from inventory"},
			"path":       map[string]any{"type": "string", "description": "Bundle-relative script path"},
			"entrypoint": map[string]any{"type": "string", "description": "Named skill.json entrypoint"},
			"args": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "Optional argument list",
			},
			"stdin":          map[string]any{"type": "string", "description": "Optional stdin text"},
			"timeoutSeconds": map[string]any{"type": "integer", "description": "Optional timeout override"},
		},
		"required": []string{"skill"},
	}
}

func (t *RunSkillScript) Schema() map[string]any {
	return t.SchemaFor(t.Name(), t.Description(), t.Parameters())
}

func (t *RunSkillScript) Execute(ctx context.Context, params map[string]any) (string, error) {
	if t.Inventory == nil {
		return "", fmt.Errorf("skills inventory not configured")
	}
	skillName := strings.TrimSpace(fmt.Sprint(params["skill"]))
	if skillName == "" {
		return "", fmt.Errorf("missing skill")
	}
	skill, ok := t.Inventory.Get(skillName)
	if !ok {
		return "", fmt.Errorf("skill not found: %s", skillName)
	}

	cmd, err := t.commandForSkill(skill, params)
	if err != nil {
		return "", err
	}
	timeout := t.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	if v, ok := params["timeoutSeconds"].(float64); ok && v > 0 {
		timeout = time.Duration(int(v)) * time.Second
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	command := exec.CommandContext(runCtx, cmd[0], cmd[1:]...)
	command.Dir = skill.Dir
	command.Env = BuildChildEnv(os.Environ(), t.ChildEnvAllowlist, EnvFromContext(ctx), "")
	if stdin := strings.TrimSpace(fmt.Sprint(params["stdin"])); stdin != "" {
		command.Stdin = strings.NewReader(stdin)
	}
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	err = command.Run()

	out := stdout.String()
	er := stderr.String()
	max := t.OutputMaxBytes
	if max <= 0 {
		max = defaultExecOutputMaxBytes
	}
	if len(out) > max {
		out = out[:max] + "\n...[truncated]\n"
	}
	if len(er) > max {
		er = er[:max] + "\n...[truncated]\n"
	}
	if err != nil {
		return fmt.Sprintf("exit error: %v\n\nstdout:\n%s\n\nstderr:\n%s", err, out, er), nil
	}
	if strings.TrimSpace(er) != "" {
		return fmt.Sprintf("stdout:\n%s\n\nstderr:\n%s", out, er), nil
	}
	return out, nil
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
