package tools

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type ExecTool struct {
	Base
	Timeout         time.Duration
	RestrictDir     string // if non-empty, cwd must be inside
	PathAppend      string
	ChildEnvAllowlist []string
	AllowedPrograms []string
	DisableShell    bool
	OutputMaxBytes  int
	BlockedPatterns []string
}

const defaultExecOutputMaxBytes = 10000

func (t *ExecTool) Name() string { return "exec" }
func (t *ExecTool) Description() string {
	return "Run a program with safety limits. Output is truncated. Legacy shell commands are optional."
}
func (t *ExecTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"program":        map[string]any{"type": "string", "description": "Program to run"},
			"args":           map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Program arguments"},
			"command":        map[string]any{"type": "string", "description": "Legacy shell command to run (privileged)"},
			"cwd":            map[string]any{"type": "string", "description": "Working directory (optional)"},
			"timeoutSeconds": map[string]any{"type": "integer", "description": "Override timeout (optional)"},
		},
		"required": []string{},
	}
}
func (t *ExecTool) Schema() map[string]any {
	return t.SchemaFor(t.Name(), t.Description(), t.Parameters())
}

func (t *ExecTool) CapabilityForParams(params map[string]any) CapabilityLevel {
	if strings.TrimSpace(fmt.Sprint(params["command"])) != "" && fmt.Sprint(params["command"]) != "<nil>" {
		return CapabilityPrivileged
	}
	return CapabilityGuarded
}

var defaultBlockedPatterns = []string{
	"rm -rf", "mkfs", "dd ", "shutdown", "reboot", "poweroff", ":(){", ">|", "chown -R /", "chmod -R 777 /",
}

func (t *ExecTool) Execute(ctx context.Context, params map[string]any) (string, error) {
	program := strings.TrimSpace(fmt.Sprint(params["program"]))
	if program == "<nil>" {
		program = ""
	}
	legacyCommand, _ := params["command"].(string)
	legacyCommand = strings.TrimSpace(legacyCommand)
	if program == "" && legacyCommand == "" {
		return "", errors.New("missing program or command")
	}
	if legacyCommand != "" {
		if t.DisableShell {
			return "", errors.New("shell command execution disabled")
		}
		lc := strings.ToLower(legacyCommand)
		patterns := t.BlockedPatterns
		if len(patterns) == 0 {
			patterns = defaultBlockedPatterns
		}
		for _, b := range patterns {
			if strings.Contains(lc, b) {
				return "", fmt.Errorf("blocked command pattern: %q", b)
			}
		}
	}
	cwd, _ := params["cwd"].(string)
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	if t.RestrictDir != "" {
		abs, err := filepath.Abs(cwd)
		if err != nil {
			return "", err
		}
		abs, err = canonicalizePath(abs)
		if err != nil {
			return "", err
		}
		root, err := canonicalizeRoot(t.RestrictDir)
		if err != nil {
			return "", err
		}
		rel, err := filepath.Rel(root, abs)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return "", fmt.Errorf("cwd outside allowed directory")
		}
	}
	if program != "" && len(t.AllowedPrograms) > 0 && !allowedProgram(program, t.AllowedPrograms) {
		return "", fmt.Errorf("program not allowed: %s", program)
	}

	to := t.Timeout
	if v, ok := params["timeoutSeconds"].(float64); ok && v > 0 {
		to = time.Duration(int(v)) * time.Second
	}
	cctx, cancel := context.WithTimeout(ctx, to)
	defer cancel()

	var c *exec.Cmd
	if legacyCommand != "" {
		c = exec.CommandContext(cctx, "bash", "-lc", legacyCommand)
	} else {
		c = exec.CommandContext(cctx, program, stringArgs(params["args"])...)
	}
	c.Dir = cwd
	c.Env = BuildChildEnv(os.Environ(), t.ChildEnvAllowlist, EnvFromContext(ctx), t.PathAppend)
	var stdout, stderr bytes.Buffer
	c.Stdout = &stdout
	c.Stderr = &stderr
	err := c.Run()
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

func allowedProgram(program string, allowed []string) bool {
	program = strings.TrimSpace(program)
	if program == "" {
		return false
	}
	base := filepath.Base(program)
	for _, candidate := range allowed {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		if candidate == program || candidate == base {
			return true
		}
	}
	return false
}
