package tools

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"or3-intern/internal/approval"
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
	if !t.Enabled {
		return "", fmt.Errorf("skill execution disabled")
	}
	skillName := strings.TrimSpace(fmt.Sprint(params["skill"]))
	if skillName == "" {
		return "", fmt.Errorf("missing skill")
	}
	skill, ok := t.Inventory.Get(skillName)
	if !ok {
		return "", fmt.Errorf("skill not found: %s", skillName)
	}
	if skill.PermissionState == "blocked" {
		return "", fmt.Errorf("skill blocked: %s", strings.Join(skill.PermissionNotes, "; "))
	}
	if skill.PermissionState != "approved" {
		return "", fmt.Errorf("skill requires approval before execution: %s", skill.Name)
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
	childEnv := BuildChildEnv(os.Environ(), t.ChildEnvAllowlist, EnvFromContext(ctx), "")
	var skillSubjectHash string
	if t.ApprovalBroker != nil {
		identity := RequesterIdentityFromContext(ctx)
		decision, err := t.ApprovalBroker.EvaluateSkillExec(ctx, approval.SkillEvaluation{
			SkillID:        skill.Name,
			Version:        skill.InstalledVersion,
			Origin:         skill.Registry,
			TrustState:     skill.PermissionState,
			ScriptHash:     skillCommandHash(cmd),
			EnvBindingHash: hashEnvBinding(childEnv),
			TimeoutSeconds: int(timeout / time.Second),
			AgentID:        firstRequester(identity.Actor),
			SessionID:      SessionFromContext(ctx),
			ApprovalToken:  ApprovalTokenFromContext(ctx),
		})
		if err != nil {
			return "", err
		}
		if !decision.Allowed {
			if decision.RequiresApproval {
				t.ApprovalBroker.AuditExecEvent(ctx, "skill_exec.blocked", decision.SubjectHash, map[string]any{"reason": "approval_required", "request_id": decision.RequestID})
				return "", fmt.Errorf("approval required for skill execution (request %d)", decision.RequestID)
			}
			t.ApprovalBroker.AuditExecEvent(ctx, "skill_exec.blocked", decision.SubjectHash, map[string]any{"reason": decision.Reason})
			return "", fmt.Errorf("skill execution blocked: %s", decision.Reason)
		}
		skillSubjectHash = decision.SubjectHash
		t.ApprovalBroker.AuditExecEvent(ctx, "skill_exec.start", skillSubjectHash, nil)
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	command, err := commandWithSandbox(runCtx, t.Sandbox, skill.Dir, cmd)
	if err != nil {
		return "", err
	}
	if command == nil {
		command = exec.CommandContext(runCtx, cmd[0], cmd[1:]...)
	}
	command.Dir = skill.Dir
	command.Env = childEnv
	if stdin := strings.TrimSpace(fmt.Sprint(params["stdin"])); stdin != "" {
		command.Stdin = strings.NewReader(stdin)
	}
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	err = command.Run()

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
		if t.ApprovalBroker != nil && skillSubjectHash != "" {
			t.ApprovalBroker.AuditExecEvent(ctx, "skill_exec.fail", skillSubjectHash, map[string]any{"error": err.Error()})
		}
		return resultOutput, fmt.Errorf("exec failed: %w", err)
	}
	if t.ApprovalBroker != nil && skillSubjectHash != "" {
		t.ApprovalBroker.AuditExecEvent(ctx, "skill_exec.complete", skillSubjectHash, nil)
	}
	return resultOutput, nil
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
	if info, err := os.Stat(cmd[len(cmd)-1]); err == nil && !info.IsDir() {
		if blob, readErr := os.ReadFile(cmd[len(cmd)-1]); readErr == nil {
			sum := sha256.Sum256(blob)
			return fmt.Sprintf("%x", sum[:])
		}
	}
	sum := sha256.Sum256([]byte(strings.Join(cmd, "\x00")))
	return fmt.Sprintf("%x", sum[:])
}
