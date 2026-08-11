package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"or3-intern/internal/approval"
	"or3-intern/internal/artifacts"
	"or3-intern/internal/controlplane"
	"or3-intern/internal/db"
	"or3-intern/internal/jobs"
	"or3-intern/internal/runners"
	"or3-intern/internal/serviceerrors"
	"or3-intern/internal/streaming"
	"or3-intern/internal/tools"
)

func (s *serviceServer) effectiveServiceProfileName(requested string) string {
	if requested = strings.TrimSpace(requested); requested != "" {
		return requested
	}
	if s == nil {
		return ""
	}
	cfg := s.configSnapshot()
	if !cfg.Security.Profiles.Enabled {
		return ""
	}
	if profileName := strings.TrimSpace(cfg.Security.Profiles.Channels["service"]); profileName != "" {
		return profileName
	}
	return strings.TrimSpace(cfg.Security.Profiles.Default)
}

func isApprovalExternalChannel(channel string) bool {
	switch strings.ToLower(strings.TrimSpace(channel)) {
	case "telegram", "discord", "slack", "whatsapp", "email":
		return true
	default:
		return false
	}
}

func approvalDeliveryMeta(requester approval.RequesterContext) map[string]any {
	meta := map[string]any{}
	for key, value := range requester.ReplyMeta {
		meta[key] = value
	}
	if strings.TrimSpace(requester.ReplyTarget) != "" {
		switch strings.ToLower(strings.TrimSpace(requester.Channel)) {
		case "telegram", "whatsapp":
			meta["chat_id"] = requester.ReplyTarget
		case "slack", "discord":
			meta["channel_id"] = requester.ReplyTarget
		case "email":
			meta["sender_email"] = requester.ReplyTarget
		}
	}
	if len(meta) == 0 {
		return nil
	}
	return meta
}

func limitServiceRequestBody(w http.ResponseWriter, r *http.Request, maxBytes int64) {
	if r != nil && r.Body != nil {
		r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
	}
}

func writeServiceRequestDecodeError(w http.ResponseWriter, err error) {
	var maxErr *http.MaxBytesError
	if errors.As(err, &maxErr) {
		writeServiceJSON(w, http.StatusRequestEntityTooLarge, map[string]any{"error": "request body too large"})
		return
	}
	writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid request body"})
}

func (s *serviceServer) handleArtifacts(w http.ResponseWriter, r *http.Request) {
	if !requireServiceRole(w, r, approval.RoleOperator) {
		return
	}
	switch r.Method {
	case http.MethodPost:
		s.handleArtifactUpload(w, r)
		return
	case http.MethodGet:
	default:
		writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		return
	}
	relative := strings.TrimPrefix(r.URL.Path, "/internal/v1/artifacts/")
	artifactID := strings.TrimSpace(strings.Trim(relative, "/"))
	if artifactID == "" || strings.Contains(artifactID, "/") {
		writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "artifact not found"})
		return
	}
	if s.serviceArtifacts() == nil {
		writeServiceJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "artifacts unavailable"})
		return
	}
	q := r.URL.Query()
	sessionKey := serviceFirstNonEmpty(q.Get("session_key"), q.Get("sessionKey"))
	if sessionKey == "" {
		writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": "session_key is required"})
		return
	}
	if namespace := serviceConnectNamespace(r); namespace != "" && !strings.HasPrefix(sessionKey, namespace) {
		writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "artifact not found"})
		return
	}
	const defaultMaxBytes int64 = 200_000
	const hardCapBytes int64 = 2_000_000
	maxBytes := defaultMaxBytes
	if raw := strings.TrimSpace(q.Get("max_bytes")); raw != "" {
		if parsed, err := strconv.ParseInt(raw, 10, 64); err == nil && parsed > 0 {
			maxBytes = parsed
		}
	}
	if maxBytes > hardCapBytes {
		maxBytes = hardCapBytes
	}
	var offset int64
	if raw := strings.TrimSpace(q.Get("offset")); raw != "" {
		if parsed, err := strconv.ParseInt(raw, 10, 64); err == nil && parsed > 0 {
			offset = parsed
		}
	}
	result, err := s.serviceArtifacts().ReadCappedFrom(r.Context(), sessionKey, artifactID, offset, maxBytes)
	if err != nil {
		switch {
		case errors.Is(err, artifacts.ErrNotFound):
			writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "artifact not found"})
		case errors.Is(err, artifacts.ErrNotAvailable):
			writeServiceJSON(w, http.StatusForbidden, map[string]any{"error": "artifact not available for session"})
		default:
			writeServiceError(w, r, http.StatusInternalServerError, "artifact read failed", err)
		}
		return
	}
	writeServiceJSON(w, http.StatusOK, map[string]any{
		"id":         result.Artifact.ID,
		"mime":       result.Artifact.Mime,
		"size_bytes": result.Artifact.SizeBytes,
		"offset":     offset,
		"read_bytes": result.ReadBytes,
		"truncated":  result.Truncated,
		"content":    result.Content,
	})
}

func (s *serviceServer) handleArtifactUpload(w http.ResponseWriter, r *http.Request) {
	if s.serviceArtifacts() == nil {
		writeServiceJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "artifacts unavailable"})
		return
	}
	const maxUploadBytes = 8 << 20
	if err := r.ParseMultipartForm(maxUploadBytes); err != nil {
		writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid multipart form"})
		return
	}
	sessionKey := serviceFirstNonEmpty(r.FormValue("session_key"), r.FormValue("sessionKey"))
	if sessionKey == "" {
		writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": "session_key is required"})
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": "file is required"})
		return
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxUploadBytes+1))
	if err != nil {
		writeServiceError(w, r, http.StatusInternalServerError, "artifact upload read failed", err)
		return
	}
	if len(data) > maxUploadBytes {
		writeServiceJSON(w, http.StatusRequestEntityTooLarge, map[string]any{"error": "file too large"})
		return
	}
	filename := "attachment"
	mimeType := "application/octet-stream"
	if header != nil {
		if name := strings.TrimSpace(header.Filename); name != "" {
			filename = name
		}
		if header.Header != nil {
			if mt := strings.TrimSpace(header.Header.Get("Content-Type")); mt != "" {
				mimeType = mt
			}
		}
	}
	att, err := s.serviceArtifacts().SaveNamed(r.Context(), sessionKey, filename, mimeType, data)
	if err != nil {
		writeServiceError(w, r, http.StatusInternalServerError, "artifact upload failed", err)
		return
	}
	writeServiceJSON(w, http.StatusCreated, map[string]any{
		"id":          att.ArtifactID,
		"artifact_id": att.ArtifactID,
		"name":        att.Filename,
		"mime_type":   att.Mime,
		"size_bytes":  att.SizeBytes,
		"kind":        att.Kind,
	})
}

func (s *serviceServer) writePersistedRunnerRunSnapshot(w http.ResponseWriter, r *http.Request, jobID string) bool {
	store := s.control().DB
	if store == nil {
		return false
	}
	run, ok, err := store.GetRunnerRun(r.Context(), jobID)
	if err != nil {
		writeServiceError(w, r, http.StatusServiceUnavailable, "runner run history unavailable", err)
		return true
	}
	if !ok {
		return false
	}
	response := controlplane.BuildRunnerRunResponse(run)
	if requestedAt, ok := response["requested_at"]; ok {
		response["created_at"] = requestedAt
	}
	events, err := store.ListRunnerRunEvents(r.Context(), run.JobID, 0, 100)
	if err != nil {
		log.Printf("load persisted runner run events failed: job=%s err=%v", run.JobID, err)
	}
	response["events"] = s.runnerRunEventsToJobEvents(events)
	writeServiceValue(w, http.StatusOK, response)
	return true
}

func (s *serviceServer) runnerRunEventsToJobEvents(events []db.RunnerRunEvent) []jobs.Event {
	out := make([]jobs.Event, 0, len(events))
	for _, e := range events {
		payload := map[string]any{
			"type":   e.Type,
			"seq":    e.Seq,
			"stream": e.Stream,
			"chunk":  e.Chunk,
		}
		if e.PayloadJSON != "" {
			var raw map[string]any
			if err := json.Unmarshal([]byte(e.PayloadJSON), &raw); err == nil {
				for k, v := range raw {
					payload[k] = v
				}
			}
		}
		out = append(out, jobs.Event{
			Sequence: e.Seq,
			Type:     e.Type,
			Data:     payload,
		})
	}
	return out
}

func serviceJSONText(values map[string]any, key string) string {
	if values == nil {
		return ""
	}
	value, ok := values[key].(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(value)
}

func serviceMetaText(values map[string]any, key string) string {
	if values == nil {
		return ""
	}
	value, ok := values[key]
	if !ok {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func boundedServiceLogPreview(text string, limit int) string {
	text = redactServiceLogPreview(strings.TrimSpace(text))
	if text == "" || limit <= 0 {
		return ""
	}
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit]) + "..."
}

var serviceLogSecretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)ya29\.[A-Za-z0-9._~+/=-]+`),
	regexp.MustCompile(`(?i)\bBearer\s+[A-Za-z0-9._~+/=-]+`),
	regexp.MustCompile(`(?i)("?(?:access_token|refresh_token|id_token|approval_token|token)"?\s*[:=]\s*")([^"\s]+)("?)`),
}

func redactServiceLogPreview(text string) string {
	for _, pattern := range serviceLogSecretPatterns {
		text = pattern.ReplaceAllStringFunc(text, func(match string) string {
			if strings.HasPrefix(strings.ToLower(match), "bearer ") {
				return "Bearer [redacted]"
			}
			if strings.Contains(match, ":") || strings.Contains(match, "=") {
				return pattern.ReplaceAllString(match, `${1}[redacted]${3}`)
			}
			return "[redacted]"
		})
	}
	return text
}

func decodeServiceJSONMap(raw string) map[string]any {
	out := map[string]any{}
	if strings.TrimSpace(raw) == "" {
		return out
	}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return map[string]any{}
	}
	return out
}

func (s *serviceServer) streamJob(w http.ResponseWriter, r *http.Request, jobID string) {
	s.streamJobWithHeartbeat(w, r, jobID, serviceJobStreamHeartbeatInterval)
}

func (s *serviceServer) streamJobWithHeartbeat(w http.ResponseWriter, r *http.Request, jobID string, heartbeatInterval time.Duration) {
	snapshot, events, unsubscribe, ok := s.app().SubscribeJob(jobID)
	if !ok {
		writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "job not found"})
		return
	}
	defer unsubscribe()
	w.Header().Set("X-Or3-Job-Id", jobID)
	if err := beginSSE(w); err != nil {
		writeServiceError(w, r, http.StatusInternalServerError, "streaming is not supported", err)
		return
	}
	for _, event := range snapshot.Events {
		if err := writeSSEEvent(w, event.Type, serviceStreamEventPayload(event)); err != nil {
			return
		}
	}
	if isTerminalStatus(snapshot.Status) {
		return
	}
	var heartbeat <-chan time.Time
	var heartbeatTicker *time.Ticker
	if heartbeatInterval > 0 {
		heartbeatTicker = time.NewTicker(heartbeatInterval)
		defer heartbeatTicker.Stop()
		heartbeat = heartbeatTicker.C
	}
	for {
		select {
		case <-r.Context().Done():
			return
		case <-heartbeat:
			if err := writeSSEComment(w, "or3 job heartbeat"); err != nil {
				return
			}
		case event, ok := <-events:
			if !ok {
				return
			}
			if err := writeSSEEvent(w, event.Type, serviceStreamEventPayload(event)); err != nil {
				return
			}
		}
	}
}

func serviceStreamEventPayload(event jobs.Event) map[string]any {
	payload := map[string]any{}
	for key, value := range event.Data {
		payload[key] = value
	}
	payload["sequence"] = event.Sequence
	payload["type"] = event.Type
	return payload
}

type serviceObserver struct {
	streaming.ConversationObserver
	finalText             string
	sawToolCall           bool
	sawToolResult         bool
	lastToolName          string
	lastToolStatus        string
	lastToolError         string
	lastToolResultPreview string
	lastToolResult        string
	lastToolCallID        string
	lastApprovalID        int64
}

func (o *serviceObserver) OnCompletion(ctx context.Context, finalText string, streamed bool) {
	o.finalText = finalText
	if o.ConversationObserver != nil {
		o.ConversationObserver.OnCompletion(ctx, finalText, streamed)
	}
}

func (o *serviceObserver) OnError(ctx context.Context, err error) {
	if o.ConversationObserver != nil {
		o.ConversationObserver.OnError(ctx, err)
	}
}

func (o *serviceObserver) OnToolCall(ctx context.Context, name string, arguments string) {
	o.sawToolCall = true
	o.lastToolName = strings.TrimSpace(name)
	o.lastToolStatus = "running"
	if o.ConversationObserver != nil {
		o.ConversationObserver.OnToolCall(ctx, name, arguments)
	}
}

func (o *serviceObserver) OnToolResult(ctx context.Context, name string, out string, err error) {
	o.sawToolResult = true
	o.lastToolName = strings.TrimSpace(name)
	o.lastToolStatus = "completed"
	o.lastToolResult = boundedServiceLogPreview(out, 16384)
	o.lastToolResultPreview = boundedServiceLogPreview(out, 180)
	if err != nil {
		o.lastToolError = err.Error()
		o.lastToolStatus = "failed"
		var approvalErr *tools.ApprovalRequiredError
		if errors.As(err, &approvalErr) {
			o.lastToolStatus = "approval_required"
			o.lastApprovalID = approvalErr.RequestID
		}
	}
	if o.ConversationObserver != nil {
		o.ConversationObserver.OnToolResult(ctx, name, out, err)
	}
}

func (o *serviceObserver) OnToolLifecycle(ctx context.Context, event streaming.ToolLifecycleEvent) {
	o.sawToolCall = true
	o.lastToolName = strings.TrimSpace(event.Name)
	o.lastToolStatus = strings.TrimSpace(event.Status)
	o.lastToolCallID = strings.TrimSpace(event.ToolCallID)
	if event.Result != "" || event.ResultPreview != "" || event.Status == "completed" || event.Status == "failed" {
		o.sawToolResult = true
	}
	if event.ResultPreview != "" {
		o.lastToolResult = boundedServiceLogPreview(event.ResultPreview, 16384)
		o.lastToolResultPreview = boundedServiceLogPreview(event.ResultPreview, 180)
	} else if event.Result != "" {
		o.lastToolResult = boundedServiceLogPreview(event.Result, 16384)
		o.lastToolResultPreview = boundedServiceLogPreview(event.Result, 180)
	}
	if event.ApprovalID > 0 {
		o.lastApprovalID = event.ApprovalID
		o.lastToolStatus = "approval_required"
	}
	if event.PublicCode == "" && event.Status == "failed" {
		event.PublicCode = serviceerrors.PublicErrorToolExecution
	}
	if event.Status == "failed" && o.lastToolError == "" {
		o.lastToolError = firstNonEmptyString(event.ResultPreview, event.Result, event.PublicCode)
	}
	if lifecycle, ok := o.ConversationObserver.(streaming.ToolLifecycleObserver); ok {
		lifecycle.OnToolLifecycle(ctx, event)
	}
}

func (o *serviceObserver) sawToolActivity() bool {
	return o != nil && (o.sawToolCall || o.sawToolResult)
}

func (o *serviceObserver) finalTextForCompletion(defaultMessage string) (string, bool) {
	if o == nil {
		return strings.TrimSpace(defaultMessage), strings.TrimSpace(defaultMessage) != ""
	}
	if finalText := strings.TrimSpace(o.finalText); finalText != "" {
		return finalText, false
	}
	if fallback, ok := o.emptyFinalTextFallback(); ok {
		o.finalText = fallback
		return fallback, true
	}
	defaultMessage = strings.TrimSpace(defaultMessage)
	if defaultMessage == "" {
		return "", false
	}
	o.finalText = defaultMessage
	return defaultMessage, true
}

func (o *serviceObserver) emptyFinalTextFallback() (string, bool) {
	if o == nil || !o.sawToolActivity() {
		return "", false
	}
	toolName := strings.TrimSpace(o.lastToolName)
	if toolName == "" {
		toolName = "tool"
	}
	switch strings.TrimSpace(o.lastToolStatus) {
	case "failed", "error":
		unavailableDetail := strings.ToLower(firstNonEmptyString(o.lastToolError, o.lastToolResultPreview))
		if tools.IsToolNotAvailableThisTurn(unavailableDetail) {
			if strings.EqualFold(toolName, tools.ToolNameExec) {
				return "I tried to run a shell command, but the Admin Assistant is intentionally limited to dedicated Doctor tools for safety. No command was run. Ask again and I will use Doctor status/config tools instead of exec.", true
			}
			if tools.IsWriteToolName(toolName) {
				return "I can't create or modify files in Ask mode (read-only). Switch to Work mode if you'd like me to write that file for you, or I can paste the content here for you to save manually.", true
			}
		}
		message := fmt.Sprintf("The tool failed, and the model did not return a final message. Last tool: %s.", toolName)
		if detail := strings.TrimSpace(firstNonEmptyString(o.lastToolError, o.lastToolResultPreview)); detail != "" {
			message += " " + boundedServiceLogPreview(detail, 220)
		}
		return message, true
	case "approval_required":
		if o.lastApprovalID > 0 {
			return fmt.Sprintf("The tool still needs approval before it can continue. Last tool: %s. Approval request: %d.", toolName, o.lastApprovalID), true
		}
		return fmt.Sprintf("The tool still needs approval before it can continue. Last tool: %s.", toolName), true
	default:
		return fmt.Sprintf("The tool finished, but the model did not return a final message. Last tool: %s.", toolName), true
	}
}

func (s *serviceServer) handleRunnerRunners(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		return
	}
	appSvc := s.app()
	detected, err := appSvc.DetectRunnerRunners(r.Context())
	if err != nil {
		writeServiceError(w, r, http.StatusServiceUnavailable, "runner detection unavailable", err)
		return
	}
	writeServiceValue(w, http.StatusOK, map[string]any{"runners": detected})
}

func (s *serviceServer) handleRunnerRuns(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/internal/v1/runner-runs" || r.URL.Path == "/internal/v1/runner-runs/" {
		switch r.Method {
		case http.MethodGet:
			s.handleRunnerRunsList(w, r)
		case http.MethodPost:
			s.handleRunnerRunsStart(w, r)
		default:
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		}
		return
	}
	relative := strings.TrimPrefix(r.URL.Path, "/internal/v1/runner-runs/")
	parts := strings.SplitN(strings.Trim(relative, "/"), "/", 2)
	runID := strings.TrimSpace(parts[0])
	if runID == "" {
		writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "not found"})
		return
	}
	if len(parts) == 2 && parts[1] == "events" {
		s.handleRunnerRunEvents(w, r, runID)
		return
	}
	if r.Method != http.MethodGet {
		writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		return
	}
	s.handleRunnerRunRead(w, r, runID)
}

func (s *serviceServer) handleRunnerRunsList(w http.ResponseWriter, r *http.Request) {
	store := s.control().DB
	if store == nil {
		writeServiceJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "database unavailable"})
		return
	}
	limit := 0
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n <= 0 {
			writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid limit"})
			return
		}
		limit = n
	}
	runs, err := store.ListRunnerRuns(r.Context(), db.RunnerRunFilter{
		Status:           strings.TrimSpace(r.URL.Query().Get("status")),
		ParentSessionKey: strings.TrimSpace(r.URL.Query().Get("parent_session_key")),
		Limit:            limit,
	})
	if err != nil {
		writeServiceError(w, r, http.StatusServiceUnavailable, "runner runs list unavailable", err)
		return
	}
	writeServiceValue(w, http.StatusOK, controlplane.BuildRunnerRunListResponse(runs))
}

func (s *serviceServer) handleRunnerRunsStart(w http.ResponseWriter, r *http.Request) {
	limitServiceRequestBody(w, r, serviceRunnerRunsBodyLimit)
	req, err := decodeServiceRunnerRunRequest(r.Body)
	if err != nil {
		writeServiceRequestDecodeError(w, err)
		return
	}
	runReq := runners.RunnerRunRequest{
		ParentSessionKey: req.ParentSessionKey,
		RunnerID:         req.RunnerID,
		Task:             req.Task,
		TimeoutSeconds:   req.TimeoutSeconds,
		Cwd:              req.Cwd,
		Model:            req.Model,
		Mode:             req.Mode,
		Isolation:        req.Isolation,
		MaxTurns:         req.MaxTurns,
		Meta:             req.Meta,
	}
	run, err := s.app().StartRunnerRun(r.Context(), runReq)
	if err != nil {
		writeServiceError(w, r, http.StatusBadRequest, "runner run rejected", err)
		return
	}
	writeServiceJSON(w, http.StatusAccepted, map[string]any{
		"job_id": run.JobID,
		"run_id": run.ID,
		"status": run.Status,
	})
}

func (s *serviceServer) handleRunnerRunRead(w http.ResponseWriter, r *http.Request, id string) {
	store := s.control().DB
	if store == nil {
		writeServiceJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "database unavailable"})
		return
	}
	run, ok, err := store.GetRunnerRun(r.Context(), id)
	if err != nil {
		writeServiceError(w, r, http.StatusServiceUnavailable, "runner run lookup unavailable", err)
		return
	}
	if !ok {
		writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "run not found"})
		return
	}
	writeServiceValue(w, http.StatusOK, controlplane.BuildRunnerRunResponse(run))
}

func (s *serviceServer) handleRunnerRunEvents(w http.ResponseWriter, r *http.Request, runID string) {
	afterSeq := int64(0)
	if afterStr := r.URL.Query().Get("after_seq"); afterStr != "" {
		if n, err := strconv.ParseInt(afterStr, 10, 64); err == nil {
			afterSeq = n
		}
	}
	store := s.control().DB
	if store == nil {
		writeServiceJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "database unavailable"})
		return
	}
	run, ok, err := store.GetRunnerRun(r.Context(), runID)
	if err != nil {
		writeServiceError(w, r, http.StatusServiceUnavailable, "runner run lookup unavailable", err)
		return
	}
	if !ok {
		writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "run not found"})
		return
	}
	events, err := store.ListRunnerRunEvents(r.Context(), run.JobID, afterSeq, 200)
	if err != nil {
		writeServiceError(w, r, http.StatusServiceUnavailable, "runner run events unavailable", err)
		return
	}
	writeServiceValue(w, http.StatusOK, controlplane.BuildRunnerRunEventListResponse(events))
}
