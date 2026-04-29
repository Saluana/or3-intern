package tools

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"or3-intern/internal/approval"
)

type ExecTool struct {
	Base
	Timeout           time.Duration
	RestrictDir       string // if non-empty, cwd must be inside
	PathAppend        string
	ChildEnvAllowlist []string
	AllowedPrograms   []string
	Sandbox           BubblewrapConfig
	EnableLegacyShell bool
	DisableShell      bool
	OutputMaxBytes    int
	BlockedPatterns   []string
	ApprovalBroker    *approval.Broker
}

const (
	defaultExecStdoutPreviewBytes = 12000
	defaultExecStderrPreviewBytes = 8000
)

func (t *ExecTool) Name() string { return "exec" }
func (t *ExecTool) Description() string {
	return "Run an allowed program with approval, sandbox, and allowlist controls. Output is truncated. Legacy shell commands require explicit opt-in; blocked shell patterns are only a safety net."
}
func (t *ExecTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"program":        map[string]any{"type": "string", "description": "Program to run"},
			"args":           map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Program arguments"},
			"command":        map[string]any{"type": "string", "description": "Legacy shell command to run (privileged, explicit opt-in only)"},
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

// defaultBlockedPatterns is a legacy-shell tripwire, not a security boundary.
// Keep exec policy in the approval broker, program allowlist, sandbox, service
// shell ban, and runtime profile controls; shell substring matching is easy to
// bypass and only catches common accidents.
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
	if toolsRequestIsService(ctx) {
		if len(t.AllowedPrograms) == 0 {
			return "", errors.New("exec has no allowed programs configured")
		}
		identity := RequesterIdentityFromContext(ctx)
		if !serviceExecRoleAllowed(identity.Role) {
			return "", errors.New("exec unavailable for this requester role")
		}
		if strings.TrimSpace(ApprovalTokenFromContext(ctx)) == "" {
			return "", errors.New("service exec requires approval token")
		}
		if legacyCommand != "" {
			return "", errors.New("shell command execution disabled for service requests")
		}
	}
	if legacyCommand != "" {
		if !t.EnableLegacyShell || t.DisableShell {
			return "", errors.New("shell command execution disabled; use program + args or explicitly enable legacy shell mode")
		}
		lc := strings.ToLower(legacyCommand)
		patterns := t.BlockedPatterns
		if len(patterns) == 0 {
			patterns = defaultBlockedPatterns
		}
		for _, b := range patterns {
			if strings.Contains(lc, b) {
				return "", fmt.Errorf("blocked legacy shell safety-net pattern: %q", b)
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
	if program != "" {
		resolvedProgram, err := resolveExecutable(program, cwd)
		if err != nil {
			return "", err
		}
		if len(t.AllowedPrograms) > 0 && !allowedProgram(program, resolvedProgram, t.AllowedPrograms) {
			return "", fmt.Errorf("program not allowed: %s", program)
		}
		program = resolvedProgram
	}

	childEnv := BuildChildEnv(os.Environ(), t.ChildEnvAllowlist, EnvFromContext(ctx), t.PathAppend)
	var execSubjectHash string
	if t.ApprovalBroker != nil {
		identity := RequesterIdentityFromContext(ctx)
		evaluation := approval.ExecEvaluation{
			ExecutablePath: program,
			Argv:           execArgv(program, legacyCommand, params),
			WorkingDir:     cwd,
			EnvBindingHash: hashEnvBinding(childEnv),
			ScriptHash:     legacyCommandHash(legacyCommand),
			AgentID:        firstRequester(identity.Actor),
			SessionID:      SessionFromContext(ctx),
			ToolName:       t.Name(),
			AccessProfile:  ActiveProfileFromContext(ctx).Name,
			ApprovalToken:  ApprovalTokenFromContext(ctx),
		}
		decision, err := t.ApprovalBroker.EvaluateExec(ctx, evaluation)
		if err != nil {
			return "", err
		}
		if !decision.Allowed {
			if decision.RequiresApproval {
				t.ApprovalBroker.AuditExecEvent(ctx, "exec.blocked", decision.SubjectHash, map[string]any{"reason": "approval_required", "request_id": decision.RequestID})
				return "", fmt.Errorf("approval required for exec (request %d)", decision.RequestID)
			}
			t.ApprovalBroker.AuditExecEvent(ctx, "exec.blocked", decision.SubjectHash, map[string]any{"reason": decision.Reason})
			return "", fmt.Errorf("exec blocked: %s", decision.Reason)
		}
		execSubjectHash = decision.SubjectHash
		t.ApprovalBroker.AuditExecEvent(ctx, "exec.start", execSubjectHash, nil)
	}

	to := t.Timeout
	if v, ok := params["timeoutSeconds"].(float64); ok && v > 0 {
		to = time.Duration(int(v)) * time.Second
	}
	cctx, cancel := context.WithTimeout(ctx, to)
	defer cancel()

	var c *exec.Cmd
	var err error
	if legacyCommand != "" {
		c, err = commandWithSandbox(cctx, t.Sandbox, cwd, []string{"bash", "-lc", legacyCommand})
		if err != nil {
			return "", err
		}
		if c == nil {
			c = exec.CommandContext(cctx, "bash", "-lc", legacyCommand)
		}
	} else {
		c = exec.CommandContext(cctx, program, stringArgs(params["args"])...)
	}
	c.Dir = cwd
	c.Env = childEnv
	var stdout, stderr bytes.Buffer
	c.Stdout = &stdout
	c.Stderr = &stderr
	err = c.Run()
	out := stdout.String()
	er := stderr.String()
	stdoutMax := defaultExecStdoutPreviewBytes
	stderrMax := defaultExecStderrPreviewBytes
	if t.OutputMaxBytes > 0 {
		stdoutMax = t.OutputMaxBytes
		stderrMax = t.OutputMaxBytes
	}
	stdoutPreview, stdoutTruncated := PreviewString(out, stdoutMax)
	stderrPreview, stderrTruncated := PreviewString(er, stderrMax)
	resultOutput := formatExecResult(stdoutPreview, stderrPreview, stdoutTruncated, stderrTruncated, len(out), len(er))
	if err != nil {
		if t.ApprovalBroker != nil && execSubjectHash != "" {
			t.ApprovalBroker.AuditExecEvent(ctx, "exec.fail", execSubjectHash, map[string]any{"error": err.Error()})
		}
		return resultOutput, fmt.Errorf("exec failed: %w", err)
	}
	if t.ApprovalBroker != nil && execSubjectHash != "" {
		t.ApprovalBroker.AuditExecEvent(ctx, "exec.complete", execSubjectHash, nil)
	}
	return resultOutput, nil
}

func toolsRequestIsService(ctx context.Context) bool {
	return RequestSourceFromContext(ctx) == RequestSourceService
}

func serviceExecRoleAllowed(role string) bool {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "operator", "admin":
		return true
	default:
		return false
	}
}

func allowedProgram(program string, resolved string, allowed []string) bool {
	program = strings.TrimSpace(program)
	resolved = strings.TrimSpace(resolved)
	if program == "" || resolved == "" {
		return false
	}
	programHasPath := hasPathSeparator(program)
	for _, candidate := range allowed {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		if hasPathSeparator(candidate) {
			resolvedCandidate, err := canonicalExecutablePath(candidate)
			if err == nil && resolvedCandidate == resolved {
				return true
			}
			continue
		}
		if !programHasPath && candidate == program {
			return true
		}
	}
	return false
}

func resolveExecutable(program string, cwd string) (string, error) {
	program = strings.TrimSpace(program)
	if program == "" {
		return "", fmt.Errorf("missing program")
	}
	if hasPathSeparator(program) {
		if !filepath.IsAbs(program) {
			base := strings.TrimSpace(cwd)
			if base == "" {
				var err error
				base, err = os.Getwd()
				if err != nil {
					return "", err
				}
			}
			program = filepath.Join(base, program)
		}
		return canonicalExecutablePath(program)
	}
	resolved, err := exec.LookPath(program)
	if err != nil {
		return "", err
	}
	return canonicalExecutablePath(resolved)
}

func canonicalExecutablePath(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err == nil {
		return resolved, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	return abs, nil
}

func hasPathSeparator(path string) bool {
	return strings.ContainsRune(path, filepath.Separator) || (filepath.Separator != '/' && strings.ContainsRune(path, '/'))
}

func formatCommandOutput(stdout, stderr string) string {
	return fmt.Sprintf("stdout:\n%s\n\nstderr:\n%s", stdout, stderr)
}

func formatExecResult(stdout, stderr string, stdoutTruncated, stderrTruncated bool, stdoutBytes, stderrBytes int) string {
	preview := strings.TrimSpace(formatCommandOutput(stdout, stderr))
	if strings.TrimSpace(stdout) == "" && strings.TrimSpace(stderr) == "" {
		preview = formatCommandOutput("", "")
	}
	return EncodeToolResult(ToolResult{
		Kind:    "exec",
		OK:      true,
		Summary: "Command completed with bounded stdout/stderr previews",
		Preview: preview,
		Stats: map[string]any{
			"stdout_bytes":     stdoutBytes,
			"stderr_bytes":     stderrBytes,
			"stdout_truncated": stdoutTruncated,
			"stderr_truncated": stderrTruncated,
		},
	})
}

func execArgv(program string, legacyCommand string, params map[string]any) []string {
	if strings.TrimSpace(legacyCommand) != "" {
		return []string{"bash", "-lc", legacyCommand}
	}
	argv := []string{program}
	argv = append(argv, stringArgs(params["args"])...)
	return argv
}

func legacyCommandHash(command string) string {
	command = strings.TrimSpace(command)
	if command == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(command))
	return fmt.Sprintf("%x", sum[:])
}

func hashEnvBinding(env []string) string {
	if len(env) == 0 {
		return ""
	}
	cloned := append([]string{}, env...)
	sort.Strings(cloned)
	sum := sha256.Sum256([]byte(strings.Join(cloned, "\n")))
	return fmt.Sprintf("%x", sum[:])
}

func firstRequester(actor string) string {
	if strings.TrimSpace(actor) != "" {
		return strings.TrimSpace(actor)
	}
	return "local"
}
