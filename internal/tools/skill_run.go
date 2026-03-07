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

// RunSkill executes a declared entrypoint from a skill manifest.
// It does NOT accept arbitrary command strings; it only runs
// manifest-declared argv, preventing shell injection.
type RunSkill struct {
	Base
	Inventory      *skills.Inventory
	DefaultTimeout time.Duration
	RestrictDir    string
	OutputMaxBytes int
}

func (t *RunSkill) Name() string { return "run_skill" }

func (t *RunSkill) Description() string {
	return "Execute a declared entrypoint from a skill manifest. Only entrypoints declared in skill.json are allowed; arbitrary commands are not accepted."
}

func (t *RunSkill) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"skill": map[string]any{
				"type":        "string",
				"description": "Skill name from inventory",
			},
			"entrypoint": map[string]any{
				"type":        "string",
				"description": "Entrypoint name declared in the skill manifest (default: first entrypoint)",
			},
			"stdin": map[string]any{
				"type":        "string",
				"description": "Optional stdin input (only if entrypoint declares acceptsStdin: true)",
			},
		},
		"required": []string{"skill"},
	}
}

func (t *RunSkill) Schema() map[string]any {
	return t.SchemaFor(t.Name(), t.Description(), t.Parameters())
}

func (t *RunSkill) Execute(ctx context.Context, params map[string]any) (string, error) {
	if t.Inventory == nil {
		return "", fmt.Errorf("skills inventory not configured")
	}
	skillName := strings.TrimSpace(fmt.Sprint(params["skill"]))
	if skillName == "" {
		return "", fmt.Errorf("missing skill name")
	}
	meta, ok := t.Inventory.Get(skillName)
	if !ok {
		return "", fmt.Errorf("skill not found: %s", skillName)
	}
	if len(meta.Entrypoints) == 0 {
		return "", fmt.Errorf("skill %q has no declared entrypoints", skillName)
	}

	// select entrypoint
	epName := ""
	if v, ok := params["entrypoint"]; ok && v != nil {
		epName = strings.TrimSpace(fmt.Sprint(v))
	}
	var ep *skills.SkillEntry
	if epName == "" {
		ep = &meta.Entrypoints[0]
	} else {
		for i := range meta.Entrypoints {
			if meta.Entrypoints[i].Name == epName {
				ep = &meta.Entrypoints[i]
				break
			}
		}
	}
	if ep == nil {
		return "", fmt.Errorf("entrypoint %q not found in skill %q", epName, skillName)
	}
	if len(ep.Command) == 0 {
		return "", fmt.Errorf("entrypoint %q has empty command", ep.Name)
	}

	// validate all command parts are non-empty (no shell expansion)
	for _, part := range ep.Command {
		if strings.TrimSpace(part) == "" {
			return "", fmt.Errorf("entrypoint command contains empty part")
		}
	}

	// working directory: skill's directory, verified to be inside RestrictDir if set
	cwd := filepath.Dir(meta.Path)
	if t.RestrictDir != "" {
		absRestrict, _ := filepath.Abs(t.RestrictDir)
		absCwd, _ := filepath.Abs(cwd)
		rel, err := filepath.Rel(absRestrict, absCwd)
		if err != nil || strings.HasPrefix(rel, "..") {
			return "", fmt.Errorf("skill directory outside allowed path")
		}
	}

	// resolve timeout
	timeout := t.DefaultTimeout
	if ep.TimeoutSeconds > 0 {
		timeout = time.Duration(ep.TimeoutSeconds) * time.Second
	}
	if timeout <= 0 {
		timeout = 30 * time.Second
	}

	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Direct argv execution - no shell
	cmd := exec.CommandContext(cctx, ep.Command[0], ep.Command[1:]...)
	cmd.Dir = cwd
	cmd.Env = os.Environ()

	// stdin
	stdinText := ""
	if ep.AcceptsStdin {
		if v, ok := params["stdin"].(string); ok {
			stdinText = v
		}
	}
	if stdinText != "" {
		cmd.Stdin = strings.NewReader(stdinText)
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	runErr := cmd.Run()
	out := stdout.String()
	er := stderr.String()

	maxBytes := t.OutputMaxBytes
	if maxBytes <= 0 {
		maxBytes = 10000
	}
	if len(out) > maxBytes {
		out = out[:maxBytes] + "\n...[truncated]\n"
	}
	if len(er) > maxBytes {
		er = er[:maxBytes] + "\n...[truncated]\n"
	}

	if runErr != nil {
		return fmt.Sprintf("exit error: %v\n\nstdout:\n%s\n\nstderr:\n%s", runErr, out, er), nil
	}
	if strings.TrimSpace(er) != "" {
		return fmt.Sprintf("stdout:\n%s\n\nstderr:\n%s", out, er), nil
	}
	return out, nil
}
