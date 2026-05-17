package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"or3-intern/internal/approval"
	"or3-intern/internal/config"
	"or3-intern/internal/controlplane"
	"or3-intern/internal/db"
)

type serviceAppBootstrapWarning struct {
	Code     string `json:"code"`
	Message  string `json:"message"`
	Severity string `json:"severity"`
}

type serviceAppActionDescriptor struct {
	ID              string `json:"id"`
	Title           string `json:"title"`
	Available       bool   `json:"available"`
	DisabledReason  string `json:"disabled_reason,omitempty"`
	SessionRequired bool   `json:"session_required,omitempty"`
	StepUpRequired  bool   `json:"step_up_required,omitempty"`
	ApprovalLikely  bool   `json:"approval_likely,omitempty"`
}

type serviceAppBootstrapResponse struct {
	Host struct {
		ID          string `json:"id,omitempty"`
		DisplayName string `json:"display_name,omitempty"`
		Version     string `json:"version,omitempty"`
	} `json:"host"`
	Pairing struct {
		Paired   bool   `json:"paired"`
		DeviceID string `json:"device_id,omitempty"`
		Role     string `json:"role,omitempty"`
	} `json:"pairing"`
	Auth struct {
		SessionRequired bool   `json:"session_required"`
		SessionActive   bool   `json:"session_active"`
		StepUpActive    bool   `json:"step_up_active"`
		Kind            string `json:"kind,omitempty"`
		Role            string `json:"role,omitempty"`
		ExecAllowed     bool   `json:"exec_allowed"`
		Capabilities    struct {
			PasskeysSupported bool `json:"passkeys_supported"`
			StepUpSupported   bool `json:"step_up_supported"`
		} `json:"capabilities"`
	} `json:"auth"`
	Status struct {
		Health       *controlplane.HealthReport       `json:"health,omitempty"`
		Readiness    *controlplane.ReadinessReport    `json:"readiness,omitempty"`
		Capabilities *controlplane.CapabilitiesReport `json:"capabilities,omitempty"`
		Summary      string                           `json:"summary"`
		Warnings     []serviceAppBootstrapWarning     `json:"warnings,omitempty"`
	} `json:"status"`
	Counts struct {
		PendingApprovals int `json:"pending_approvals"`
		ActiveJobs       int `json:"active_jobs"`
		ActiveTerminals  int `json:"active_terminals,omitempty"`
	} `json:"counts"`
	Actions  []serviceAppActionDescriptor `json:"actions"`
	Features struct {
		AppBootstrap   bool `json:"app_bootstrap"`
		AppEvents      bool `json:"app_events"`
		AppActions     bool `json:"app_actions"`
		FileMetadataV2 bool `json:"file_metadata_v2"`
	} `json:"features"`
}

type serviceActionResponse struct {
	ActionID    string `json:"action_id"`
	Status      string `json:"status"`
	Message     string `json:"message,omitempty"`
	ApprovalID  int64  `json:"approval_id,omitempty"`
	OperationID string `json:"operation_id,omitempty"`
	LogPath     string `json:"log_path,omitempty"`
}

func (s *serviceServer) handleApp(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/internal/v1/app/bootstrap" {
		writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "app route not found"})
		return
	}
	if r.Method != http.MethodGet {
		writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		return
	}
	writeServiceValue(w, http.StatusOK, s.buildAppBootstrap(r))
}

func (s *serviceServer) handleActions(w http.ResponseWriter, r *http.Request) {
	relative := strings.Trim(strings.TrimPrefix(r.URL.Path, "/internal/v1/actions/"), "/")
	if relative == "" {
		writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "action route not found"})
		return
	}
	if r.Method != http.MethodPost {
		writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		return
	}
	switch relative {
	case "restart-service":
		s.handleRestartServiceAction(w, r)
	default:
		writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "action not found"})
	}
}

func (s *serviceServer) buildAppBootstrap(r *http.Request) serviceAppBootstrapResponse {
	var response serviceAppBootstrapResponse

	identity := serviceAuthIdentityFromContext(r.Context())
	authSvc := s.app().Auth()
	hostID := s.control().GetCapabilities("", "").HostID
	hostName, _ := os.Hostname()
	if strings.TrimSpace(hostName) == "" {
		hostName = "or3-intern host"
	}

	response.Host.ID = hostID
	response.Host.DisplayName = hostName
	response.Features.AppBootstrap = true
	response.Features.AppEvents = false
	response.Features.AppActions = true
	response.Features.FileMetadataV2 = false

	response.Pairing.Paired = identity.Kind == "paired-device" || identity.Kind == "auth-session"
	response.Pairing.DeviceID = identity.Device
	response.Pairing.Role = identity.Role

	response.Auth.SessionRequired = s.config.Auth.EnforcementMode == config.AuthEnforcementSession
	response.Auth.SessionActive = identity.Kind == "auth-session"
	response.Auth.StepUpActive = identity.StepUpOK
	response.Auth.Kind = identity.Kind
	response.Auth.Role = identity.Role
	response.Auth.ExecAllowed = s.config.Tools.EnableExec && serviceBootstrapExecAllowed(identity.Role)
	response.Auth.Capabilities.PasskeysSupported = authSvc != nil && authSvc.Enabled()
	response.Auth.Capabilities.StepUpSupported = s.config.Auth.Enabled && s.config.Auth.RequirePasskeyForSensitive

	health := s.control().GetHealth()
	readiness := s.control().GetReadiness()
	capabilities := s.control().GetCapabilities("", "")
	response.Status.Health = &health
	response.Status.Readiness = &readiness
	response.Status.Capabilities = &capabilities
	response.Status.Summary = serviceBootstrapSummary(health, readiness)

	warnings := make([]serviceAppBootstrapWarning, 0, 8)
	if !health.RuntimeAvailable {
		warnings = append(warnings, serviceAppBootstrapWarning{Code: "runtime_unavailable", Message: "The OR3 runtime is not available right now.", Severity: "error"})
	}
	if !health.JobRegistryAvailable {
		warnings = append(warnings, serviceAppBootstrapWarning{Code: "job_registry_unavailable", Message: "Live job tracking is limited right now.", Severity: "warning"})
	}
	if !health.ApprovalBrokerAvailable {
		warnings = append(warnings, serviceAppBootstrapWarning{Code: "approval_broker_unavailable", Message: "Approval workflows are unavailable right now.", Severity: "warning"})
	}
	if !readiness.Ready {
		warnings = append(warnings, serviceAppBootstrapWarning{Code: "host_not_ready", Message: "This computer still has readiness issues to resolve.", Severity: "warning"})
	}
	if response.Pairing.Paired && !response.Auth.SessionActive {
		warnings = append(warnings, serviceAppBootstrapWarning{Code: "session_not_active", Message: "Passkey sign-in is still required for protected actions.", Severity: "info"})
	}
	if response.Auth.Kind == "shared-secret" && !response.Auth.ExecAllowed {
		warnings = append(warnings, serviceAppBootstrapWarning{
			Code:     "shared_secret_limited",
			Message:  "This connection is using the shared service secret as service-client. Read-only API calls work, but exec and approvals need a paired operator or admin device.",
			Severity: "warning",
		})
	}
	for _, quarantined := range s.config.IntegrationWarnings {
		name := strings.TrimSpace(quarantined.Name)
		if name == "" {
			name = "integration"
		}
		warnings = append(warnings, serviceAppBootstrapWarning{
			Code:     "integration_quarantined",
			Message:  fmt.Sprintf("%s was disabled because its settings are incomplete: %s", name, serviceFirstNonEmpty(quarantined.Reason, "invalid configuration")),
			Severity: "warning",
		})
	}
	if !s.config.ContextConfigured {
		warnings = append(warnings, serviceAppBootstrapWarning{
			Code:     "legacy_context_mode",
			Message:  "This host is using legacy context settings because the saved config has no context section.",
			Severity: "info",
		})
	}
	if embeddingStatus, err := s.control().GetEmbeddingStatus(r.Context()); err == nil && strings.EqualFold(embeddingStatus.Status, "mismatch") {
		warnings = append(warnings, serviceAppBootstrapWarning{
			Code:     "embedding_fingerprint_mismatch",
			Message:  "Memory embeddings were built with a different provider or model. Rebuild embeddings so recall stays accurate.",
			Severity: "warning",
		})
	}
	response.Status.Warnings = warnings

	response.Counts.PendingApprovals = s.bootstrapPendingApprovalCount(r.Context())
	response.Counts.ActiveJobs = s.bootstrapActiveJobCount(r.Context())
	response.Counts.ActiveTerminals = s.bootstrapActiveTerminalCount()

	response.Actions = []serviceAppActionDescriptor{
		s.restartActionDescriptor(),
	}
	return response
}

func serviceBootstrapSummary(health controlplane.HealthReport, readiness controlplane.ReadinessReport) string {
	if !health.RuntimeAvailable {
		return "offline"
	}
	if !readiness.Ready || strings.EqualFold(health.Status, "degraded") {
		return "degraded"
	}
	return "ready"
}

func serviceBootstrapExecAllowed(role string) bool {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "operator", "admin":
		return true
	default:
		return false
	}
}

func (s *serviceServer) bootstrapPendingApprovalCount(ctx context.Context) int {
	if s == nil || s.broker == nil {
		return 0
	}
	items, err := s.broker.ListApprovalRequestsFiltered(ctx, approval.StatusPending, "", 200)
	if err != nil {
		return 0
	}
	return len(items)
}

func (s *serviceServer) bootstrapActiveJobCount(ctx context.Context) int {
	if s == nil || s.control() == nil || s.control().DB == nil {
		return 0
	}
	items, err := s.control().DB.ListSubagentJobs(ctx, db.SubagentJobFilter{Status: "active", Limit: 200})
	if err != nil {
		return 0
	}
	return len(items)
}

func (s *serviceServer) bootstrapActiveTerminalCount() int {
	if s == nil {
		return 0
	}
	s.cleanupTerminalSessions()
	s.terminals().mu.Lock()
	defer s.terminals().mu.Unlock()
	return len(s.terminals().sessions)
}

func (s *serviceServer) restartActionDescriptor() serviceAppActionDescriptor {
	descriptor := serviceAppActionDescriptor{
		ID:              "restart-service",
		Title:           "Restart service",
		SessionRequired: true,
		StepUpRequired:  true,
	}
	if s != nil && s.broker != nil && strings.EqualFold(string(s.config.Security.Approvals.Exec.Mode), string(config.ApprovalModeAsk)) {
		descriptor.ApprovalLikely = true
	}
	if !s.terminalAvailable() {
		descriptor.DisabledReason = "Shell access is turned off on this computer."
		return descriptor
	}
	_, _, ok := s.findServiceRestartScript()
	if !ok {
		descriptor.DisabledReason = "The restart script is not available on this computer."
		return descriptor
	}
	descriptor.Available = true
	return descriptor
}

func (s *serviceServer) handleRestartServiceAction(w http.ResponseWriter, r *http.Request) {
	descriptor := s.restartActionDescriptor()
	if !descriptor.Available {
		writeServiceJSON(w, http.StatusServiceUnavailable, serviceErrorPayload(r, serviceFirstNonEmpty(descriptor.DisabledReason, "restart is not available on this computer")))
		return
	}
	scriptPath, workingDir, ok := s.findServiceRestartScript()
	if !ok {
		writeServiceJSON(w, http.StatusServiceUnavailable, serviceErrorPayload(r, "restart is not available on this computer"))
		return
	}
	shellPath, err := resolveTerminalShell("sh")
	if err != nil {
		writeServiceError(w, r, http.StatusServiceUnavailable, "restart shell is not available", err)
		return
	}
	decision, err := s.evaluateTerminalApproval(r.Context(), shellPath, workingDir, "")
	if err != nil {
		writeServiceError(w, r, http.StatusBadGateway, "restart approval failed", err)
		return
	}
	if decision.RequiresApproval {
		writeServiceValue(w, http.StatusConflict, serviceActionResponse{
			ActionID:   "restart-service",
			Status:     "approval_required",
			Message:    "restart service requires approval",
			ApprovalID: decision.RequestID,
		})
		return
	}
	if !decision.Allowed {
		reason := strings.TrimSpace(decision.Reason)
		if reason == "" {
			reason = "restart service denied"
		}
		writeServiceJSON(w, http.StatusForbidden, serviceErrorPayload(r, reason))
		return
	}
	operationID := newServiceRequestID()
	logPath, err := startDetachedServiceRestart(scriptPath, workingDir, s.unsafeDev, operationID)
	if err != nil {
		writeServiceError(w, r, http.StatusBadGateway, "restart failed to start", err)
		return
	}
	writeServiceValue(w, http.StatusAccepted, serviceActionResponse{
		ActionID:    "restart-service",
		Status:      "accepted",
		Message:     "restart requested",
		OperationID: operationID,
		LogPath:     logPath,
	})
}

func startDetachedServiceRestart(scriptPath, workingDir string, unsafeDev bool, operationID string) (string, error) {
	scriptPath = strings.TrimSpace(scriptPath)
	workingDir = strings.TrimSpace(workingDir)
	if scriptPath == "" || workingDir == "" {
		return "", fmt.Errorf("restart script is unavailable")
	}
	logPath, err := serviceRestartOperationLogPath(workingDir, operationID)
	if err != nil {
		return "", err
	}
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return "", fmt.Errorf("restart log unavailable: %w", err)
	}
	defer logFile.Close()
	fmt.Fprintf(logFile, "%s restart requested by pid %d\n", time.Now().UTC().Format(time.RFC3339Nano), os.Getpid())
	cmd := exec.Command(scriptPath, "restart")
	cmd.Dir = workingDir
	cmd.Env = append(os.Environ(), "OR3_SERVICE_RESTART_LOG="+logPath)
	if unsafeDev {
		cmd.Env = append(cmd.Env, "OR3_SERVICE_UNSAFE_DEV=true")
	}
	cmd.Stdin = nil
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return logPath, err
	}
	return logPath, nil
}

func serviceRestartOperationLogPath(workingDir, operationID string) (string, error) {
	workingDir = strings.TrimSpace(workingDir)
	if workingDir == "" {
		return "", fmt.Errorf("restart working directory is unavailable")
	}
	operationID = serviceRestartLogID(operationID)
	if operationID == "" {
		operationID = serviceRestartLogID(newServiceRequestID())
	}
	runDir := filepath.Join(workingDir, ".run")
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		return "", err
	}
	return filepath.Join(runDir, "service-restart-"+operationID+".log"), nil
}

func serviceRestartLogID(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	var b strings.Builder
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func (s *serviceServer) findServiceRestartScript() (scriptPath string, workingDir string, ok bool) {
	for _, dir := range serviceRestartSearchDirs() {
		script := filepath.Join(dir, "scripts", "restart-service.sh")
		info, err := os.Stat(script)
		if err != nil || info.IsDir() {
			continue
		}
		if info.Mode()&0o111 == 0 {
			continue
		}
		return script, dir, true
	}
	return "", "", false
}
