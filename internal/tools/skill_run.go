package tools

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	crand "crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"or3-intern/internal/approval"
	"or3-intern/internal/db"
	"or3-intern/internal/skills"
)

type RunSkill struct {
	RunSkillScript
}

type skillRunHashInput struct {
	SkillID        string   `json:"skill_id"`
	Version        string   `json:"version,omitempty"`
	Origin         string   `json:"origin,omitempty"`
	TrustState     string   `json:"trust_state,omitempty"`
	SkillDir       string   `json:"skill_dir"`
	RelativePath   string   `json:"relative_path,omitempty"`
	Entrypoint     string   `json:"entrypoint,omitempty"`
	Args           []string `json:"args,omitempty"`
	StdinText      string   `json:"stdin_text,omitempty"`
	TimeoutSeconds int      `json:"timeout_seconds"`
	Command        []string `json:"command"`
	ScriptHash     string   `json:"script_hash"`
	EnvBindingHash string   `json:"env_binding_hash"`
}

type preparedSkillRun struct {
	plan      db.SkillRunPlanRecord
	skill     skills.SkillMeta
	command   []string
	childEnv  []string
	timeout   time.Duration
	stdinText string
}

func (t *RunSkill) Name() string { return "run_skill" }

func (t *RunSkill) Description() string {
	return "Run an approved skill through a frozen SkillRunPlan. The tool preflights the command, persists an immutable plan, returns structured pending/preflight/result states, and can resume after approval using either the same arguments or a returned plan_id."
}

func (t *RunSkill) Parameters() map[string]any { return skillRunParameters() }

func (t *RunSkill) Schema() map[string]any {
	return t.SchemaFor(t.Name(), t.Description(), t.Parameters())
}

func (t *RunSkill) Execute(ctx context.Context, params map[string]any) (string, error) {
	return t.executeNamed(ctx, params, t.Name())
}

func skillRunParameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"plan_id":        map[string]any{"type": "string", "description": "Optional frozen skill-run plan ID returned by an earlier pending_approval or preflight result. When provided, the tool resumes that exact plan instead of creating a new one."},
			"skill":          map[string]any{"type": "string", "description": "Skill name exactly as listed in the inventory. Required when plan_id is omitted."},
			"path":           map[string]any{"type": "string", "description": "Bundle-relative script path to run. Use either path or entrypoint, not both."},
			"entrypoint":     map[string]any{"type": "string", "description": "Named skill.json entrypoint to run. Use this when the skill declares an entrypoint instead of a raw script path."},
			"args":           map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Optional argument list passed without shell interpolation. Put each flag/path as its own array item."},
			"stdin":          map[string]any{"type": "string", "description": "Optional stdin text for the script or entrypoint."},
			"timeoutSeconds": map[string]any{"type": "integer", "description": "Optional timeout override in seconds."},
		},
		"required": []string{},
	}
}

func (t *RunSkillScript) executeNamed(ctx context.Context, params map[string]any, toolName string) (string, error) {
	if strings.TrimSpace(toolName) == "run_skill" && t.DB == nil {
		result := encodeSkillRunResult(db.SkillRunStatusPreflightFailed, false, db.SkillRunPlanRecord{}, "run_skill requires persistent plan storage", "", 0, nil)
		return result, fmt.Errorf("skill run plans not configured")
	}
	if planID := optionalSkillRunString(params, "plan_id"); planID != "" {
		prepared, cached, out, err := t.loadPreparedSkillRun(ctx, planID)
		if cached || err != nil {
			return out, err
		}
		return t.authorizeAndRunSkill(ctx, prepared, toolName)
	}

	prepared, out, err := t.prepareSkillRun(ctx, params)
	if err != nil {
		return out, err
	}
	if t.DB != nil {
		stored, reused, err := t.DB.GetOrCreateActiveSkillRunPlan(ctx, prepared.plan)
		if err != nil {
			return "", err
		}
		prepared.plan = stored
		if reused {
			preparedExisting, cached, existingOut, loadErr := t.loadPreparedSkillRunByRecord(ctx, stored)
			if cached || loadErr != nil {
				return existingOut, loadErr
			}
			return t.authorizeAndRunSkill(ctx, preparedExisting, toolName)
		}
	}
	return t.authorizeAndRunSkill(ctx, prepared, toolName)
}

func (t *RunSkillScript) prepareSkillRun(ctx context.Context, params map[string]any) (preparedSkillRun, string, error) {
	if t.Inventory == nil {
		return preparedSkillRun{}, encodeSkillRunResult(db.SkillRunStatusPreflightFailed, false, db.SkillRunPlanRecord{}, "skills inventory not configured", "", 0, nil), fmt.Errorf("skills inventory not configured")
	}
	if !t.Enabled {
		return preparedSkillRun{}, encodeSkillRunResult(db.SkillRunStatusPreflightFailed, false, db.SkillRunPlanRecord{}, "skill execution disabled", "", 0, nil), fmt.Errorf("skill execution disabled")
	}
	skillName := optionalSkillRunString(params, "skill")
	if skillName == "" {
		return preparedSkillRun{}, encodeSkillRunResult(db.SkillRunStatusPreflightFailed, false, db.SkillRunPlanRecord{}, "missing skill", "", 0, nil), fmt.Errorf("missing skill")
	}
	skill, ok := t.Inventory.Get(skillName)
	if !ok {
		return preparedSkillRun{}, encodeSkillRunResult(db.SkillRunStatusPreflightFailed, false, db.SkillRunPlanRecord{SkillID: skillName}, fmt.Sprintf("skill not found: %s", skillName), "", 0, nil), fmt.Errorf("skill not found: %s", skillName)
	}
	if skill.PermissionState == "blocked" {
		reason := fmt.Sprintf("skill blocked: %s", strings.Join(skill.PermissionNotes, "; "))
		return preparedSkillRun{}, encodeSkillRunResult(db.SkillRunStatusPreflightFailed, false, db.SkillRunPlanRecord{SkillID: skill.Name}, reason, "", 0, nil), errors.New(reason)
	}
	if skill.PermissionState != "approved" {
		reason := fmt.Sprintf("skill requires approval before execution: %s", skill.Name)
		return preparedSkillRun{}, encodeSkillRunResult(db.SkillRunStatusPreflightFailed, false, db.SkillRunPlanRecord{SkillID: skill.Name}, reason, "", 0, nil), errors.New(reason)
	}
	cmd, err := t.commandForSkill(skill, params)
	if err != nil {
		return preparedSkillRun{}, encodeSkillRunResult(db.SkillRunStatusPreflightFailed, false, db.SkillRunPlanRecord{SkillID: skill.Name}, err.Error(), "", 0, nil), err
	}
	timeout := t.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	if v, ok := params["timeoutSeconds"].(float64); ok && v > 0 {
		timeout = time.Duration(int(v)) * time.Second
	}
	childEnv := BuildChildEnv(os.Environ(), t.ChildEnvAllowlist, EnvFromContext(ctx), "")
	args := stringArgs(params["args"])
	stdinText := optionalSkillRunString(params, "stdin")
	argsJSON, err := json.Marshal(args)
	if err != nil {
		return preparedSkillRun{}, "", err
	}
	commandJSON, err := json.Marshal(cmd)
	if err != nil {
		return preparedSkillRun{}, "", err
	}
	persistedStdinText, stdinNonce, stdinHash, err := t.persistableSkillRunStdin(stdinText)
	if err != nil {
		return preparedSkillRun{}, encodeSkillRunResult(db.SkillRunStatusPreflightFailed, false, db.SkillRunPlanRecord{SkillID: skill.Name}, err.Error(), "", 0, nil), err
	}
	planHashInput := skillRunHashInput{
		SkillID:        skill.Name,
		Version:        skill.InstalledVersion,
		Origin:         skill.Registry,
		TrustState:     skill.PermissionState,
		SkillDir:       skill.Dir,
		RelativePath:   optionalSkillRunString(params, "path"),
		Entrypoint:     optionalSkillRunString(params, "entrypoint"),
		Args:           args,
		StdinText:      stdinText,
		TimeoutSeconds: int(timeout / time.Second),
		Command:        append([]string{}, cmd...),
		ScriptHash:     skillCommandHash(cmd),
		EnvBindingHash: hashEnvBinding(childEnv),
	}
	sh, err := approval.CanonicalSubjectHash(planHashInput)
	if err != nil {
		return preparedSkillRun{}, "", err
	}
	planHash := sh.Hash
	identity := RequesterIdentityFromContext(ctx)
	plan := db.SkillRunPlanRecord{
		SkillID:            skill.Name,
		Version:            skill.InstalledVersion,
		Origin:             skill.Registry,
		TrustState:         skill.PermissionState,
		SkillDir:           skill.Dir,
		RelativePath:       optionalSkillRunString(params, "path"),
		Entrypoint:         optionalSkillRunString(params, "entrypoint"),
		ArgsJSON:           string(argsJSON),
		StdinText:          persistedStdinText,
		StdinNonce:         append([]byte{}, stdinNonce...),
		StdinSHA256:        stdinHash,
		TimeoutSeconds:     int(timeout / time.Second),
		CommandJSON:        string(commandJSON),
		ScriptHash:         planHashInput.ScriptHash,
		EnvBindingHash:     planHashInput.EnvBindingHash,
		PlanHash:           planHash,
		RequesterAgentID:   firstRequester(identity.Actor),
		RequesterSessionID: SessionFromContext(ctx),
		ExecutionHostID:    skillRunExecutionHostID(t.ApprovalBroker),
		Status:             db.SkillRunStatusPlanned,
		CreatedAt:          db.NowMS(),
		UpdatedAt:          db.NowMS(),
	}
	return preparedSkillRun{plan: plan, skill: skill, command: cmd, childEnv: childEnv, timeout: timeout, stdinText: stdinText}, "", nil
}

func (t *RunSkillScript) loadPreparedSkillRun(ctx context.Context, planID string) (preparedSkillRun, bool, string, error) {
	if t.DB == nil {
		return preparedSkillRun{}, false, encodeSkillRunResult(db.SkillRunStatusPreflightFailed, false, db.SkillRunPlanRecord{ID: planID}, "skill run plans not configured", "", 0, nil), fmt.Errorf("skill run plans not configured")
	}
	plan, err := t.DB.GetSkillRunPlan(ctx, planID)
	if err != nil {
		message := fmt.Sprintf("skill run plan not found: %s", planID)
		if !errors.Is(err, sql.ErrNoRows) {
			message = err.Error()
		}
		return preparedSkillRun{}, false, encodeSkillRunResult(db.SkillRunStatusPreflightFailed, false, db.SkillRunPlanRecord{ID: planID}, message, "", 0, nil), errors.New(message)
	}
	return t.loadPreparedSkillRunByRecord(ctx, plan)
}

func (t *RunSkillScript) loadPreparedSkillRunByRecord(ctx context.Context, plan db.SkillRunPlanRecord) (preparedSkillRun, bool, string, error) {
	if plan.Status == db.SkillRunStatusRunning {
		return t.runningSkillRunState(ctx, plan)
	}
	if isTerminalSkillRunStatus(plan.Status) {
		if strings.TrimSpace(plan.ResultJSON) != "" {
			return preparedSkillRun{}, true, plan.ResultJSON, nil
		}
		result := encodeSkillRunResult(plan.Status, plan.Status == db.SkillRunStatusSucceeded, plan, firstNonEmptySkillRunSummary(plan), "", plan.ApprovalRequestID, nil)
		return preparedSkillRun{}, true, result, nil
	}
	if t.Inventory == nil {
		return t.preflightFailureForPlan(ctx, plan, "skills inventory not configured")
	}
	skill, ok := t.Inventory.Get(plan.SkillID)
	if !ok {
		return t.preflightFailureForPlanStatus(ctx, plan, db.SkillRunStatusStalePlan, fmt.Sprintf("skill not found: %s", plan.SkillID))
	}
	if skill.PermissionState == "blocked" {
		return t.preflightFailureForPlanStatus(ctx, plan, db.SkillRunStatusStalePlan, fmt.Sprintf("skill blocked: %s", strings.Join(skill.PermissionNotes, "; ")))
	}
	if skill.PermissionState != "approved" {
		return t.preflightFailureForPlanStatus(ctx, plan, db.SkillRunStatusStalePlan, fmt.Sprintf("skill requires approval before execution: %s", skill.Name))
	}
	if skill.Dir != plan.SkillDir || skill.InstalledVersion != plan.Version || skill.Registry != plan.Origin {
		return t.preflightFailureForPlanStatus(ctx, plan, db.SkillRunStatusStalePlan, "skill metadata changed since the plan was frozen")
	}
	var command []string
	if err := json.Unmarshal([]byte(plan.CommandJSON), &command); err != nil || len(command) == 0 {
		return t.preflightFailureForPlan(ctx, plan, "stored skill run command is invalid")
	}
	childEnv := BuildChildEnv(os.Environ(), t.ChildEnvAllowlist, EnvFromContext(ctx), "")
	if hashEnvBinding(childEnv) != plan.EnvBindingHash {
		return t.preflightFailureForPlanStatus(ctx, plan, db.SkillRunStatusStalePlan, "environment binding changed since the plan was frozen")
	}
	if skillCommandHash(command) != plan.ScriptHash {
		return t.preflightFailureForPlanStatus(ctx, plan, db.SkillRunStatusStalePlan, "script content changed since the plan was frozen")
	}
	stdinText, err := t.loadSkillRunStdin(plan)
	if err != nil {
		return t.preflightFailureForPlan(ctx, plan, err.Error())
	}
	timeout := time.Duration(plan.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = t.Timeout
	}
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return preparedSkillRun{plan: plan, skill: skill, command: command, childEnv: childEnv, timeout: timeout, stdinText: stdinText}, false, "", nil
}

func (t *RunSkillScript) authorizeAndRunSkill(ctx context.Context, prepared preparedSkillRun, toolName string) (string, error) {
	var subjectHash string
	if t.ApprovalBroker != nil {
		decision, err := t.ApprovalBroker.EvaluateSkillExec(ctx, approval.SkillEvaluation{
			SkillID:        prepared.plan.SkillID,
			Version:        prepared.plan.Version,
			Origin:         prepared.plan.Origin,
			TrustState:     prepared.plan.TrustState,
			ToolName:       toolName,
			PlanID:         prepared.plan.ID,
			PlanHash:       prepared.plan.PlanHash,
			ScriptHash:     prepared.plan.ScriptHash,
			EnvBindingHash: prepared.plan.EnvBindingHash,
			TimeoutSeconds: prepared.plan.TimeoutSeconds,
			AgentID:        prepared.plan.RequesterAgentID,
			SessionID:      prepared.plan.RequesterSessionID,
			ApprovalToken:  ApprovalTokenFromContext(ctx),
		})
		if err != nil {
			return "", err
		}
		prepared.plan.SubjectHash = decision.SubjectHash
		if decision.RequestID > 0 {
			prepared.plan.ApprovalRequestID = decision.RequestID
		}
		if !decision.Allowed {
			if decision.RequiresApproval {
				result := encodeSkillRunResult(db.SkillRunStatusPendingApproval, false, prepared.plan, "approval required before skill execution can continue", "", decision.RequestID, nil)
				if err := t.persistSkillRunResult(ctx, prepared.plan, db.SkillRunStatusPendingApproval, result, "approval required"); err != nil {
					return result, err
				}
				t.ApprovalBroker.AuditExecEvent(ctx, "skill_exec.blocked", decision.SubjectHash, map[string]any{"reason": "approval_required", "request_id": decision.RequestID, "plan_id": prepared.plan.ID})
				return result, &ApprovalRequiredError{ToolName: toolName, RequestID: decision.RequestID}
			}
			reason := firstNonEmptySkillRun(decision.Reason, "skill execution blocked")
			result := encodeSkillRunResult(db.SkillRunStatusBlockedByPolicy, false, prepared.plan, reason, "", decision.RequestID, nil)
			if err := t.persistSkillRunResult(ctx, prepared.plan, db.SkillRunStatusBlockedByPolicy, result, reason); err != nil {
				return result, err
			}
			t.ApprovalBroker.AuditExecEvent(ctx, "skill_exec.blocked", decision.SubjectHash, map[string]any{"reason": decision.Reason, "plan_id": prepared.plan.ID})
			return result, fmt.Errorf("skill execution blocked: %s", decision.Reason)
		}
		subjectHash = decision.SubjectHash
	}
	if t.DB != nil && strings.TrimSpace(prepared.plan.ID) != "" {
		if err := t.persistSkillRunResult(ctx, prepared.plan, db.SkillRunStatusApproved, "", ""); err != nil {
			result := encodeSkillRunResult(db.SkillRunStatusPreflightFailed, false, prepared.plan, err.Error(), "", prepared.plan.ApprovalRequestID, nil)
			return result, err
		}
		prepared.plan.Status = db.SkillRunStatusApproved
	}
	return t.runPreparedSkillRun(ctx, prepared, subjectHash)
}

func (t *RunSkillScript) runPreparedSkillRun(ctx context.Context, prepared preparedSkillRun, subjectHash string) (string, error) {
	if t.DB != nil && strings.TrimSpace(prepared.plan.ID) != "" {
		claimed, err := t.DB.ClaimSkillRunPlan(ctx, prepared.plan.ID, db.NowMS())
		if err != nil {
			result := encodeSkillRunResult(db.SkillRunStatusPreflightFailed, false, prepared.plan, err.Error(), "", prepared.plan.ApprovalRequestID, nil)
			return result, err
		}
		if !claimed {
			return t.recoverClaimedSkillRun(ctx, prepared.plan.ID)
		}
		prepared.plan.Status = db.SkillRunStatusRunning
	}
	runCtx, cancel := context.WithTimeout(ctx, prepared.timeout)
	defer cancel()

	var heartbeatStop chan struct{}
	if t.DB != nil && strings.TrimSpace(prepared.plan.ID) != "" {
		heartbeatStop = make(chan struct{})
		defer close(heartbeatStop)
		go func() {
			ticker := time.NewTicker(30 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					_ = t.DB.TouchSkillRunPlan(context.Background(), prepared.plan.ID)
				case <-heartbeatStop:
					return
				}
			}
		}()
	}

	command, err := commandWithSandbox(runCtx, t.Sandbox, prepared.skill.Dir, prepared.command)
	if err != nil {
		if !errors.Is(err, errSandboxNotEnabled) {
			result := encodeSkillRunResult(db.SkillRunStatusPreflightFailed, false, prepared.plan, err.Error(), "", prepared.plan.ApprovalRequestID, nil)
			if persistErr := t.persistSkillRunResult(ctx, prepared.plan, db.SkillRunStatusPreflightFailed, result, err.Error()); persistErr != nil {
				return result, persistErr
			}
			return result, err
		}
		command = nil
	}
	if command == nil {
		command = exec.CommandContext(runCtx, prepared.command[0], prepared.command[1:]...)
	}
	command.Dir = prepared.skill.Dir
	command.Env = prepared.childEnv
	if prepared.stdinText != "" {
		command.Stdin = strings.NewReader(prepared.stdinText)
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
	status := db.SkillRunStatusSucceeded
	if err != nil {
		status = classifySkillRunExecutionStatus(runCtx.Err(), err)
	}
	result := encodeSkillRunExecutionResult(prepared.plan, status, err == nil, stdoutPreview, stderrPreview, stdoutTruncated, stderrTruncated, len(out), len(er))
	if err != nil {
		if persistErr := t.persistSkillRunResult(ctx, prepared.plan, status, result, err.Error()); persistErr != nil {
			return result, persistErr
		}
		if t.ApprovalBroker != nil && subjectHash != "" {
			t.ApprovalBroker.AuditExecEvent(ctx, "skill_exec.fail", subjectHash, map[string]any{"error": err.Error(), "plan_id": prepared.plan.ID})
		}
		return result, fmt.Errorf("exec failed: %w", err)
	}
	if err := t.persistSkillRunResult(ctx, prepared.plan, status, result, ""); err != nil {
		return result, err
	}
	if t.ApprovalBroker != nil && subjectHash != "" {
		t.ApprovalBroker.AuditExecEvent(ctx, "skill_exec.complete", subjectHash, map[string]any{"plan_id": prepared.plan.ID})
	}
	return result, nil
}

func (t *RunSkillScript) preflightFailureForPlan(ctx context.Context, plan db.SkillRunPlanRecord, message string) (preparedSkillRun, bool, string, error) {
	return t.preflightFailureForPlanStatus(ctx, plan, db.SkillRunStatusPreflightFailed, message)
}

func (t *RunSkillScript) preflightFailureForPlanStatus(ctx context.Context, plan db.SkillRunPlanRecord, status, message string) (preparedSkillRun, bool, string, error) {
	result := encodeSkillRunResult(status, false, plan, strings.TrimSpace(message), "", plan.ApprovalRequestID, nil)
	if err := t.persistSkillRunResult(ctx, plan, status, result, strings.TrimSpace(message)); err != nil {
		return preparedSkillRun{}, false, result, err
	}
	return preparedSkillRun{}, false, result, errors.New(strings.TrimSpace(message))
}

func (t *RunSkillScript) persistSkillRunResult(ctx context.Context, plan db.SkillRunPlanRecord, status, result, lastError string) error {
	if t.DB == nil || strings.TrimSpace(plan.ID) == "" {
		return nil
	}
	status = firstNonEmptySkillRun(status, plan.Status)
	requestID := plan.ApprovalRequestID
	if !t.canPersistSkillRunApprovalLink() {
		requestID = 0
	}
	if plan.SubjectHash != "" || plan.ApprovalRequestID > 0 {
		if err := t.DB.UpdateSkillRunPlanApproval(ctx, plan.ID, requestID, plan.SubjectHash, status, db.NowMS()); err != nil {
			return err
		}
	}
	if err := t.DB.UpdateSkillRunPlanResult(ctx, plan.ID, status, result, lastError, db.NowMS()); err != nil {
		return err
	}
	if db.IsTerminalSkillRunStatus(status) {
		return t.DB.ClearSkillRunPlanStdin(ctx, plan.ID, db.NowMS())
	}
	return nil
}

func encodeSkillRunExecutionResult(plan db.SkillRunPlanRecord, status string, ok bool, stdout, stderr string, stdoutTruncated, stderrTruncated bool, stdoutBytes, stderrBytes int) string {
	preview := strings.TrimSpace(formatCommandOutput(stdout, stderr))
	if strings.TrimSpace(stdout) == "" && strings.TrimSpace(stderr) == "" {
		preview = formatCommandOutput("", "")
	}
	summary := "Skill run completed with bounded stdout/stderr previews"
	if status == db.SkillRunStatusTimedOut {
		summary = "Skill run timed out with bounded stdout/stderr previews"
	} else if !ok {
		summary = "Skill run failed with bounded stdout/stderr previews"
	}
	return encodeSkillRunResult(status, ok, plan, summary, preview, plan.ApprovalRequestID, map[string]any{
		"stdout_bytes":     stdoutBytes,
		"stderr_bytes":     stderrBytes,
		"stdout_truncated": stdoutTruncated,
		"stderr_truncated": stderrTruncated,
	})
}

func encodeSkillRunResult(status string, ok bool, plan db.SkillRunPlanRecord, summary, preview string, requestID int64, stats map[string]any) string {
	if stats == nil {
		stats = map[string]any{}
	}
	if strings.TrimSpace(plan.SkillID) != "" {
		stats["skill"] = plan.SkillID
	}
	if strings.TrimSpace(plan.Entrypoint) != "" {
		stats["entrypoint"] = plan.Entrypoint
	}
	if strings.TrimSpace(plan.RelativePath) != "" {
		stats["path"] = plan.RelativePath
	}
	if plan.TimeoutSeconds > 0 {
		stats["timeout_seconds"] = plan.TimeoutSeconds
	}
	return EncodeToolResult(ToolResult{
		Kind:      "skill_run",
		OK:        ok,
		Status:    strings.TrimSpace(status),
		Summary:   strings.TrimSpace(summary),
		Preview:   strings.TrimSpace(preview),
		PlanID:    strings.TrimSpace(plan.ID),
		RequestID: requestID,
		Stats:     stats,
	})
}

func skillRunExecutionHostID(broker *approval.Broker) string {
	if broker == nil {
		return ""
	}
	if strings.TrimSpace(broker.HostID) != "" {
		return strings.TrimSpace(broker.HostID)
	}
	return strings.TrimSpace(broker.Config.HostID)
}

func optionalSkillRunString(params map[string]any, key string) string {
	text := strings.TrimSpace(fmt.Sprint(params[key]))
	if text == "<nil>" {
		return ""
	}
	return text
}

func isTerminalSkillRunStatus(status string) bool {
	return db.IsTerminalSkillRunStatus(status)
}

func firstNonEmptySkillRun(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

func firstNonEmptySkillRunSummary(plan db.SkillRunPlanRecord) string {
	if message := strings.TrimSpace(plan.LastError); message != "" {
		return message
	}
	return "skill run already completed"
}

func (t *RunSkillScript) recoverClaimedSkillRun(ctx context.Context, planID string) (string, error) {
	plan, err := t.DB.GetSkillRunPlan(ctx, planID)
	if err != nil {
		result := encodeSkillRunResult(db.SkillRunStatusPreflightFailed, false, db.SkillRunPlanRecord{ID: planID}, err.Error(), "", 0, nil)
		return result, err
	}
	prepared, cached, out, loadErr := t.loadPreparedSkillRunByRecord(ctx, plan)
	if cached || loadErr != nil {
		return out, loadErr
	}
	result := encodeSkillRunResult(db.SkillRunStatusRunning, false, prepared.plan, "skill run is already in progress", "", prepared.plan.ApprovalRequestID, nil)
	return result, nil
}

func (t *RunSkillScript) runningSkillRunState(ctx context.Context, plan db.SkillRunPlanRecord) (preparedSkillRun, bool, string, error) {
	nowMS := skillRunNowMS(t.ApprovalBroker)
	timeoutMS := int64(plan.TimeoutSeconds) * int64(time.Second/time.Millisecond)
	if timeoutMS <= 0 {
		timeoutMS = int64((30 * time.Second) / time.Millisecond)
	}
	if plan.UpdatedAt > 0 && nowMS > plan.UpdatedAt+timeoutMS+5000 {
		return t.preflightFailureForPlanStatus(ctx, plan, db.SkillRunStatusStalePlan, "a previous run never completed; create a new plan")
	}
	result := encodeSkillRunResult(db.SkillRunStatusRunning, false, plan, "skill run is already in progress", "", plan.ApprovalRequestID, nil)
	return preparedSkillRun{}, true, result, nil
}

func (t *RunSkillScript) loadSkillRunStdin(plan db.SkillRunPlanRecord) (string, error) {
	if strings.TrimSpace(plan.StdinText) == "" {
		return "", nil
	}
	if len(plan.StdinNonce) == 0 {
		return plan.StdinText, nil
	}
	key := t.skillRunSensitiveDataKey()
	if len(key) == 0 {
		return "", fmt.Errorf("secure stdin key unavailable for frozen plan")
	}
	ciphertext, err := base64.RawStdEncoding.DecodeString(strings.TrimSpace(plan.StdinText))
	if err != nil {
		return "", fmt.Errorf("stored skill run stdin is invalid")
	}
	plaintext, err := decryptSkillRunBlob(key, ciphertext, plan.StdinNonce)
	if err != nil {
		return "", fmt.Errorf("stored skill run stdin is unreadable")
	}
	if strings.TrimSpace(plan.StdinSHA256) != "" && hashSkillRunStdin(string(plaintext)) != strings.TrimSpace(plan.StdinSHA256) {
		return "", fmt.Errorf("stored skill run stdin hash mismatch")
	}
	return string(plaintext), nil
}

func (t *RunSkillScript) persistableSkillRunStdin(stdinText string) (string, []byte, string, error) {
	if strings.TrimSpace(stdinText) == "" || t.DB == nil {
		return "", nil, "", nil
	}
	key := t.skillRunSensitiveDataKey()
	if len(key) == 0 {
		return "", nil, "", fmt.Errorf("secure stdin persistence requires an approval or secret-store key")
	}
	ciphertext, nonce, err := encryptSkillRunBlob(key, []byte(stdinText))
	if err != nil {
		return "", nil, "", err
	}
	return base64.RawStdEncoding.EncodeToString(ciphertext), nonce, hashSkillRunStdin(stdinText), nil
}

func (t *RunSkillScript) skillRunSensitiveDataKey() []byte {
	if len(t.SensitiveDataKey) > 0 {
		return append([]byte{}, t.SensitiveDataKey...)
	}
	if t.ApprovalBroker != nil && len(t.ApprovalBroker.SignKey) > 0 {
		encKey, err := hkdf.Key(sha256.New, t.ApprovalBroker.SignKey, nil, "skill-run-encryption", 32)
		if err != nil {
			return nil
		}
		return encKey
	}
	return nil
}

func encryptSkillRunBlob(master, plaintext []byte) ([]byte, []byte, error) {
	block, err := aes.NewCipher(deriveSkillRunKey(master))
	if err != nil {
		return nil, nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, nil, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := crand.Read(nonce); err != nil {
		return nil, nil, err
	}
	sealed := aead.Seal(nil, nonce, plaintext, nil)
	return sealed, nonce, nil
}

func decryptSkillRunBlob(master, ciphertext, nonce []byte) ([]byte, error) {
	block, err := aes.NewCipher(deriveSkillRunKey(master))
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return aead.Open(nil, nonce, ciphertext, nil)
}

func deriveSkillRunKey(master []byte) []byte {
	key, err := hkdf.Key(sha256.New, master, nil, "skill-run-stdin", 32)
	if err != nil {
		return nil
	}
	return key
}

func hashSkillRunStdin(stdinText string) string {
	sum := sha256.Sum256([]byte(stdinText))
	return base64.RawStdEncoding.EncodeToString(sum[:])
}

func classifySkillRunExecutionStatus(runErr error, execErr error) string {
	if errors.Is(runErr, context.DeadlineExceeded) || errors.Is(execErr, context.DeadlineExceeded) {
		return db.SkillRunStatusTimedOut
	}
	return db.SkillRunStatusFailed
}

func skillRunNowMS(broker *approval.Broker) int64 {
	if broker != nil && broker.Now != nil {
		return broker.Now().UTC().UnixMilli()
	}
	return time.Now().UTC().UnixMilli()
}

func (t *RunSkillScript) canPersistSkillRunApprovalLink() bool {
	if t.DB == nil || t.ApprovalBroker == nil || t.ApprovalBroker.DB == nil {
		return false
	}
	return t.DB.SQL == t.ApprovalBroker.DB.SQL
}
