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
	"runtime"
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

type ApprovalRequiredError struct {
	ToolName  string
	RequestID int64
}

func (e *ApprovalRequiredError) Error() string {
	toolName := strings.TrimSpace(e.ToolName)
	if toolName == "" {
		toolName = "tool"
	}
	if e.RequestID > 0 {
		return fmt.Sprintf("approval required for %s (request %d)", toolName, e.RequestID)
	}
	return fmt.Sprintf("approval required for %s", toolName)
}

const (
	defaultExecStdoutPreviewBytes = 12000
	defaultExecStderrPreviewBytes = 8000
)

func (t *ExecTool) Name() string { return "exec" }
func (t *ExecTool) Description() string {
	return "Run an allowed local program with approval, sandbox, and allowlist controls. Prefer program plus args. When adapting a CLI example from a skill or doc, put the executable in program and each token in args. The legacy command field runs through a shell, changes approval semantics, and may be disabled; avoid command unless shell syntax is explicitly required. After approval-required failures, retry the identical executable and argv. When a workspace restriction is configured, omit cwd to run in that workspace root or pass a cwd inside it."
}
func (t *ExecTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"program":        map[string]any{"type": "string", "description": "Executable name or path to run, such as rg, git, go, npm, node, or gws. For a CLI like `gws tasks tasklists list --format table`, set program to `gws`. Prefer this field over command."},
			"args":           map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Arguments passed directly to program without shell parsing. Put each flag/path as its own array item. For the gws example, use [`tasks`, `tasklists`, `list`, `--format`, `table`]."},
			"command":        map[string]any{"type": "string", "description": "Legacy shell command string. This enables shell parsing, may be rejected by service policy, and changes approval semantics. Do not send a full command line here when program+args can express the same call."},
			"cwd":            map[string]any{"type": "string", "description": "Working directory for the process. Omit to use the current workspace; must satisfy any configured directory restrictions."},
			"timeoutSeconds": map[string]any{"type": "integer", "description": "Optional timeout override in seconds. Use only for commands expected to run longer than the default."},
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

func (t *ExecTool) CapabilityForContextParams(ctx context.Context, params map[string]any) CapabilityLevel {
	if strings.TrimSpace(fmt.Sprint(params["command"])) != "" && fmt.Sprint(params["command"]) != "<nil>" {
		if toolsRequestIsService(ctx) {
			return CapabilityGuarded
		}
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

func resolveExecWorkingDir(requested string, restrictDir string) (string, error) {
	requested = strings.TrimSpace(requested)
	if requested == "" || requested == "<nil>" {
		if strings.TrimSpace(restrictDir) != "" {
			return restrictDir, nil
		}
		return os.Getwd()
	}
	if filepath.IsAbs(requested) {
		return requested, nil
	}
	base := strings.TrimSpace(restrictDir)
	if base == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return "", err
		}
		base = cwd
	}
	return filepath.Join(base, requested), nil
}

func (t *ExecTool) Execute(ctx context.Context, params map[string]any) (string, error) {
	program := strings.TrimSpace(fmt.Sprint(params["program"]))
	if program == "<nil>" {
		program = ""
	}
	legacyCommand, _ := params["command"].(string)
	legacyCommand = strings.TrimSpace(legacyCommand)
	if toolsRequestIsService(ctx) && legacyCommand != "" {
		parsedProgram, parsedArgs, err := parseServiceDirectCommand(legacyCommand)
		if err != nil {
			return "", fmt.Errorf("shell command execution disabled for service requests; use program + args: %w", err)
		}
		if program == "" {
			program = parsedProgram
		} else if !serviceDirectProgramMatches(program, parsedProgram) {
			return "", fmt.Errorf("service exec program/command mismatch: program=%q command executable=%q", program, parsedProgram)
		}
		params = cloneParamsWithArgs(params, append(parsedArgs, stringArgs(params["args"])...))
		legacyCommand = ""
	}
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
	cwd, err := resolveExecWorkingDir(fmt.Sprint(params["cwd"]), t.RestrictDir)
	if err != nil {
		return "", err
	}
	childEnv := BuildChildEnv(os.Environ(), t.ChildEnvAllowlist, EnvFromContext(ctx), t.PathAppend)
	if t.RestrictDir != "" {
		abs, err := filepath.Abs(cwd)
		if err != nil {
			return "", err
		}
		abs, err = CanonicalizePath(abs)
		if err != nil {
			return "", err
		}
		root, err := CanonicalizeRoot(t.RestrictDir)
		if err != nil {
			return "", err
		}
		rel, err := filepath.Rel(root, abs)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return "", fmt.Errorf("cwd outside allowed directory: %s (allowed root: %s)", abs, root)
		}
	}
	if toolsRequestIsService(ctx) && legacyCommand == "" && strings.EqualFold(program, "which") {
		return executeServiceWhich(params, cwd, childEnv)
	}
	if program != "" {
		resolvedProgram, err := resolveExecutable(program, cwd, childEnv)
		if err != nil {
			return "", err
		}
		if len(t.AllowedPrograms) > 0 && !allowedProgram(program, resolvedProgram, t.AllowedPrograms) {
			return "", fmt.Errorf("program not allowed: %s", program)
		}
		program = resolvedProgram
	}

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
				return "", &ApprovalRequiredError{ToolName: t.Name(), RequestID: decision.RequestID}
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

func parseServiceDirectCommand(command string) (string, []string, error) {
	command = strings.TrimSpace(command)
	if command == "" {
		return "", nil, errors.New("empty command")
	}
	if strings.ContainsAny(command, ";&|<>\n\r`()$") {
		return "", nil, errors.New("shell syntax is not allowed in service exec")
	}
	parts, err := splitDirectCommand(command)
	if err != nil {
		return "", nil, err
	}
	if len(parts) == 0 || strings.TrimSpace(parts[0]) == "" {
		return "", nil, errors.New("missing program")
	}
	return parts[0], parts[1:], nil
}

func splitDirectCommand(command string) ([]string, error) {
	var parts []string
	var current strings.Builder
	var quote rune
	escaped := false
	for _, r := range command {
		if escaped {
			current.WriteRune(r)
			escaped = false
			continue
		}
		if r == '\\' {
			escaped = true
			continue
		}
		if quote != 0 {
			if r == quote {
				quote = 0
				continue
			}
			current.WriteRune(r)
			continue
		}
		if r == '\'' || r == '"' {
			quote = r
			continue
		}
		if r == ' ' || r == '\t' {
			if current.Len() > 0 {
				parts = append(parts, current.String())
				current.Reset()
			}
			continue
		}
		current.WriteRune(r)
	}
	if escaped {
		return nil, errors.New("unfinished escape in command")
	}
	if quote != 0 {
		return nil, errors.New("unterminated quote in command")
	}
	if current.Len() > 0 {
		parts = append(parts, current.String())
	}
	return parts, nil
}

func cloneParamsWithArgs(params map[string]any, args []string) map[string]any {
	cloned := make(map[string]any, len(params)+1)
	for key, value := range params {
		if key == "command" {
			continue
		}
		cloned[key] = value
	}
	values := make([]any, 0, len(args))
	for _, arg := range args {
		values = append(values, arg)
	}
	cloned["args"] = values
	return cloned
}

func serviceDirectProgramMatches(program string, parsedProgram string) bool {
	program = strings.TrimSpace(program)
	parsedProgram = strings.TrimSpace(parsedProgram)
	if program == "" || parsedProgram == "" {
		return false
	}
	if program == parsedProgram {
		return true
	}
	return filepath.Base(program) == filepath.Base(parsedProgram)
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

func resolveExecutable(program string, cwd string, env []string) (string, error) {
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
	resolved, err := lookPathWithEnv(program, env)
	if err != nil {
		return "", err
	}
	return canonicalExecutablePath(resolved)
}

func executeServiceWhich(params map[string]any, cwd string, env []string) (string, error) {
	targets := stringArgs(params["args"])
	if len(targets) == 0 {
		return "", errors.New("which requires at least one program name")
	}
	found := make([]string, 0, len(targets))
	missing := make([]string, 0)
	for _, target := range targets {
		resolved, err := resolveExecutable(target, cwd, env)
		if err != nil {
			missing = append(missing, target)
			continue
		}
		found = append(found, resolved)
	}
	previewParts := make([]string, 0, 2)
	if len(found) > 0 {
		previewParts = append(previewParts, strings.Join(found, "\n"))
	}
	if len(missing) > 0 {
		previewParts = append(previewParts, fmt.Sprintf("not found: %s", strings.Join(missing, ", ")))
	}
	summary := "Executable lookup on the effective child PATH"
	if len(missing) > 0 {
		summary = "Some executables were not found on the effective child PATH"
	}
	return EncodeToolResult(ToolResult{
		Kind:    "exec_lookup",
		OK:      len(missing) == 0,
		Summary: summary,
		Preview: strings.TrimSpace(strings.Join(previewParts, "\n\n")),
	}), nil
}

func lookPathWithEnv(program string, env []string) (string, error) {
	pathValue := envValue(env, "PATH")
	if strings.TrimSpace(pathValue) == "" {
		return exec.LookPath(program)
	}
	dirs := filepath.SplitList(pathValue)
	if runtime.GOOS == "windows" {
		return lookPathWindows(program, dirs, envValue(env, "PATHEXT"))
	}
	for _, dir := range dirs {
		if dir = strings.TrimSpace(dir); dir == "" {
			continue
		}
		candidate := filepath.Join(dir, program)
		if isExecutablePath(candidate) {
			return candidate, nil
		}
	}
	return "", &exec.Error{Name: program, Err: exec.ErrNotFound}
}

func lookPathWindows(program string, dirs []string, pathExt string) (string, error) {
	extensions := []string{""}
	if filepath.Ext(program) == "" {
		for _, ext := range filepath.SplitList(strings.ReplaceAll(pathExt, ";", string(os.PathListSeparator))) {
			ext = strings.TrimSpace(ext)
			if ext == "" {
				continue
			}
			if !strings.HasPrefix(ext, ".") {
				ext = "." + ext
			}
			extensions = append(extensions, ext)
		}
		if len(extensions) == 1 {
			extensions = append(extensions, ".com", ".exe", ".bat", ".cmd")
		}
	}
	for _, dir := range dirs {
		if dir = strings.TrimSpace(dir); dir == "" {
			continue
		}
		for _, ext := range extensions {
			candidate := filepath.Join(dir, program) + ext
			if isExecutablePath(candidate) {
				return candidate, nil
			}
		}
	}
	return "", &exec.Error{Name: program, Err: exec.ErrNotFound}
}

func isExecutablePath(path string) bool {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false
	}
	if runtime.GOOS == "windows" {
		return true
	}
	return info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0
}

func envValue(env []string, key string) string {
	for _, raw := range env {
		name, value, ok := strings.Cut(raw, "=")
		if ok && name == key {
			return value
		}
	}
	return ""
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
