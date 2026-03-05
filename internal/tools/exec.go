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
	Timeout time.Duration
	RestrictDir string // if non-empty, cwd must be inside
	PathAppend string
	OutputMaxBytes int
	BlockedPatterns []string
}

const defaultExecOutputMaxBytes = 10000

func (t *ExecTool) Name() string { return "exec" }
func (t *ExecTool) Description() string {
	return "Run a shell command with safety limits. Output is truncated."
}
func (t *ExecTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"command": map[string]any{"type": "string", "description": "Shell command to run"},
			"cwd": map[string]any{"type": "string", "description": "Working directory (optional)"},
			"timeoutSeconds": map[string]any{"type": "integer", "description": "Override timeout (optional)"},
		},
		"required": []string{"command"},
	}
}
func (t *ExecTool) Schema() map[string]any { return t.SchemaFor(t.Name(), t.Description(), t.Parameters()) }

var defaultBlockedPatterns = []string{
	"rm -rf", "mkfs", "dd ", "shutdown", "reboot", "poweroff", ":(){", ">|", "chown -R /", "chmod -R 777 /",
}

func (t *ExecTool) Execute(ctx context.Context, params map[string]any) (string, error) {
	cmdS, _ := params["command"].(string)
	if strings.TrimSpace(cmdS) == "" { return "", errors.New("missing command") }
	lc := strings.ToLower(cmdS)
	patterns := t.BlockedPatterns
	if len(patterns) == 0 { patterns = defaultBlockedPatterns }
	for _, b := range patterns {
		if strings.Contains(lc, b) {
			return "", fmt.Errorf("blocked command pattern: %q", b)
		}
	}
	cwd, _ := params["cwd"].(string)
	if cwd == "" { cwd, _ = os.Getwd() }
	if t.RestrictDir != "" {
		abs, _ := filepath.Abs(cwd)
		root, _ := filepath.Abs(t.RestrictDir)
		rel, err := filepath.Rel(root, abs)
		if err != nil || strings.HasPrefix(rel, "..") {
			return "", fmt.Errorf("cwd outside allowed directory")
		}
	}

	to := t.Timeout
	if v, ok := params["timeoutSeconds"].(float64); ok && v > 0 {
		to = time.Duration(int(v)) * time.Second
	}
	cctx, cancel := context.WithTimeout(ctx, to)
	defer cancel()

	c := exec.CommandContext(cctx, "bash", "-lc", cmdS)
	c.Dir = cwd
	if t.PathAppend != "" {
		env := os.Environ()
		env = append(env, "PATH="+os.Getenv("PATH")+string(os.PathListSeparator)+t.PathAppend)
		c.Env = env
	}
	var stdout, stderr bytes.Buffer
	c.Stdout = &stdout
	c.Stderr = &stderr
	err := c.Run()
	out := stdout.String()
	er := stderr.String()
	max := t.OutputMaxBytes
	if max <= 0 { max = defaultExecOutputMaxBytes }
	if len(out) > max { out = out[:max] + "\n...[truncated]\n" }
	if len(er) > max { er = er[:max] + "\n...[truncated]\n" }
	if err != nil {
		return fmt.Sprintf("exit error: %v\n\nstdout:\n%s\n\nstderr:\n%s", err, out, er), nil
	}
	if strings.TrimSpace(er) != "" {
		return fmt.Sprintf("stdout:\n%s\n\nstderr:\n%s", out, er), nil
	}
	return out, nil
}
