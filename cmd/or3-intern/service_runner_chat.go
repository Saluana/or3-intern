package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"or3-intern/internal/app"
	"or3-intern/internal/approval"
	"or3-intern/internal/controlplane"
	"or3-intern/internal/db"
	"or3-intern/internal/runners"
	"or3-intern/internal/tools"
	"or3-intern/internal/turns"
)

const serviceRunnerChatBodyLimit = 64 * 1024
const serviceRunnerChatPromptCompileTimeout = 8 * time.Second
const serviceRunnerChatAppSessionKeyPrefixMaxBytes = 256

func (s *serviceServer) runnerChatWriteUnavailable() bool {
	if s == nil {
		return true
	}
	chatManager := s.runtimeComponents().chatManager
	return chatManager == nil || chatManager.Manager == nil
}

// runnerChatCreateSessionRequest is the body for POST /runner-chat/sessions.
type runnerChatCreateSessionRequest struct {
	AppSessionKey     string `json:"app_session_key"`
	RunnerID          string `json:"runner_id"`
	ContinuationMode  string `json:"continuation_mode"`
	Model             string `json:"model"`
	Mode              string `json:"mode"`
	Isolation         string `json:"isolation"`
	Cwd               string `json:"cwd"`
	MaxTurns          int    `json:"max_turns"`
	ApprovalAutopilot *bool  `json:"approval_autopilot"`
}

// runnerChatStartTurnRequest is the body for POST /runner-chat/sessions/:id/turns.
type runnerChatStartTurnRequest struct {
	UserMessage       string           `json:"user_message"`
	Attachments       []map[string]any `json:"attachments"`
	ContinuationMode  string           `json:"continuation_mode"`
	Model             string           `json:"model"`
	Mode              string           `json:"mode"`
	Isolation         string           `json:"isolation"`
	Cwd               string           `json:"cwd"`
	MaxTurns          int              `json:"max_turns"`
	TimeoutSeconds    int              `json:"timeout_seconds"`
	Meta              map[string]any   `json:"meta"`
	ThinkingLevel     string           `json:"thinking_level"`
	ApprovalToken     string           `json:"approval_token"`
	ApprovalAutopilot *bool            `json:"approval_autopilot"`
	RunnerPermission  struct {
		RunnerID   string `json:"runner_id"`
		Kind       string `json:"kind"`
		Access     string `json:"access"`
		TargetPath string `json:"target_path"`
	} `json:"runner_permission"`
}

// handleRunnerChatSessions dispatches the runner-chat session/turn API:
//
//	GET  /internal/v1/runner-chat/sessions
//	POST /internal/v1/runner-chat/sessions
//	GET  /internal/v1/runner-chat/sessions/:id
//	GET  /internal/v1/runner-chat/sessions/:id/turns
//	POST /internal/v1/runner-chat/sessions/:id/turns
//	GET  /internal/v1/runner-chat/sessions/:id/turns/:turn_id
//	GET  /internal/v1/runner-chat/sessions/:id/turns/:turn_id/events
//	GET  /internal/v1/runner-chat/sessions/:id/turns/:turn_id/stream  (SSE)
//	POST /internal/v1/runner-chat/sessions/:id/turns/:turn_id/abort
func (s *serviceServer) handleRunnerChatSessions(w http.ResponseWriter, r *http.Request) {
	chatManager := s.runtimeComponents().chatManager
	if chatManager == nil || chatManager.DB == nil {
		writeServiceJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "chat manager unavailable"})
		return
	}
	store := s.control().DB
	if store == nil {
		writeServiceJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "database unavailable"})
		return
	}
	path := r.URL.Path
	if path == "/internal/v1/runner-chat/sessions" || path == "/internal/v1/runner-chat/sessions/" {
		switch r.Method {
		case http.MethodGet:
			s.handleRunnerChatSessionsList(w, r, store)
		case http.MethodPost:
			s.handleRunnerChatSessionCreate(w, r)
		default:
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		}
		return
	}
	rel := strings.TrimPrefix(path, "/internal/v1/runner-chat/sessions/")
	parts := strings.Split(strings.Trim(rel, "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "not found"})
		return
	}
	sessionID := parts[0]
	if !s.requireRunnerChatSessionScope(w, r, store, sessionID) {
		return
	}
	switch {
	case len(parts) == 1:
		if r.Method != http.MethodGet {
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		s.handleRunnerChatSessionRead(w, r, store, sessionID)
	case len(parts) == 2 && parts[1] == "turns":
		switch r.Method {
		case http.MethodGet:
			s.handleRunnerChatTurnsList(w, r, store, sessionID)
		case http.MethodPost:
			s.handleRunnerChatTurnStart(w, r, sessionID)
		default:
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		}
	case len(parts) >= 3 && parts[1] == "turns":
		turnID := parts[2]
		if turnID == "" {
			writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "not found"})
			return
		}
		tail := ""
		if len(parts) == 4 {
			tail = parts[3]
		}
		switch tail {
		case "":
			if r.Method != http.MethodGet {
				writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
				return
			}
			s.handleRunnerChatTurnRead(w, r, store, sessionID, turnID)
		case "events":
			if r.Method != http.MethodGet {
				writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
				return
			}
			s.handleRunnerChatTurnEvents(w, r, store, sessionID, turnID)
		case "stream":
			if r.Method != http.MethodGet {
				writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
				return
			}
			s.handleRunnerChatTurnStream(w, r, store, sessionID, turnID)
		case "abort":
			if r.Method != http.MethodPost {
				writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
				return
			}
			s.handleRunnerChatTurnAbort(w, r, store, sessionID, turnID)
		case "approve", "reject", "cancel":
			if r.Method != http.MethodPost {
				writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
				return
			}
			s.handleRunnerChatTurnDecision(w, r, store, sessionID, turnID, tail)
		default:
			writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "not found"})
		}
	default:
		writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "not found"})
	}
}

func (s *serviceServer) handleRunnerChatSessionsList(w http.ResponseWriter, r *http.Request, store *db.DB) {
	filter := db.RunnerChatSessionListFilter{}
	query := r.URL.Query()
	if values, present := query["limit"]; present {
		raw := strings.TrimSpace(query.Get("limit"))
		n, err := strconv.Atoi(raw)
		if len(values) != 1 || raw == "" || err != nil || n <= 0 || n > db.MaxRunnerChatSessionListLimit {
			writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid limit", "code": "validation_failed"})
			return
		}
		filter.Limit = n
	}
	if values, present := query["app_session_key_prefix"]; present {
		prefix := strings.TrimSpace(query.Get("app_session_key_prefix"))
		if len(values) != 1 ||
			prefix == "" ||
			len(prefix) > serviceRunnerChatAppSessionKeyPrefixMaxBytes ||
			strings.ContainsAny(prefix, "\x00\r\n") {
			writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid app_session_key_prefix", "code": "validation_failed"})
			return
		}
		filter.AppSessionKeyPrefix = prefix
	}
	if namespace := serviceConnectNamespace(r); namespace != "" {
		if filter.AppSessionKeyPrefix != "" && !strings.HasPrefix(filter.AppSessionKeyPrefix, namespace) {
			writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid app_session_key_prefix", "code": "validation_failed"})
			return
		}
		if filter.AppSessionKeyPrefix == "" {
			filter.AppSessionKeyPrefix = namespace
		}
	}
	sessions, err := store.ListRunnerChatSessions(r.Context(), filter)
	if err != nil {
		writeServiceError(w, r, http.StatusServiceUnavailable, "runner chat sessions list unavailable", err)
		return
	}
	writeServiceValue(w, http.StatusOK, controlplane.BuildRunnerChatSessionListResponse(sessions))
}

func (s *serviceServer) handleRunnerChatSessionCreate(w http.ResponseWriter, r *http.Request) {
	if s.runnerChatWriteUnavailable() {
		writeServiceJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "runner manager is disabled", "code": "runner_disabled"})
		return
	}
	limitServiceRequestBody(w, r, serviceRunnerChatBodyLimit)
	var req runnerChatCreateSessionRequest
	if err := decodeServiceJSONLoose(r.Body, &req); err != nil {
		writeServiceError(w, r, http.StatusBadRequest, "invalid request", err)
		return
	}
	if strings.TrimSpace(req.AppSessionKey) == "" {
		writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": "app_session_key required"})
		return
	}
	if namespace := serviceConnectNamespace(r); namespace != "" && !strings.HasPrefix(req.AppSessionKey, namespace) {
		writeServiceJSON(w, http.StatusForbidden, map[string]any{"error": "app_session_key is outside the connected workspace", "code": serviceCodeForbidden})
		return
	}
	if strings.TrimSpace(req.RunnerID) == "" {
		writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": "runner_id required"})
		return
	}
	chatManager := s.runtimeComponents().chatManager
	if chatManager == nil || chatManager.Manager == nil {
		writeServiceJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "runner manager is disabled", "code": "runner_disabled"})
		return
	}
	sess, err := chatManager.EnsureSession(r.Context(), runners.StartTurnRequest{
		AppSessionKey:    req.AppSessionKey,
		RunnerID:         req.RunnerID,
		ContinuationMode: runners.ContinuationMode(strings.TrimSpace(req.ContinuationMode)),
		Model:            req.Model,
		Mode:             req.Mode,
		Isolation:        req.Isolation,
		Cwd:              req.Cwd,
		MaxTurns:         req.MaxTurns,
	})
	if err != nil {
		writeServiceError(w, r, http.StatusBadRequest, "create runner chat session failed", err)
		return
	}
	writeServiceValue(w, http.StatusCreated, controlplane.BuildRunnerChatSessionResponse(sess))
}

func serviceConnectNamespace(r *http.Request) string {
	if r == nil {
		return ""
	}
	identity := serviceAuthIdentityFromContext(r.Context())
	if identity.Role != approval.RoleConnect {
		return ""
	}
	return strings.TrimSpace(identity.Namespace)
}

func (s *serviceServer) requireRunnerChatSessionScope(w http.ResponseWriter, r *http.Request, store *db.DB, sessionID string) bool {
	namespace := serviceConnectNamespace(r)
	if namespace == "" {
		return true
	}
	session, err := store.GetRunnerChatSession(r.Context(), sessionID)
	if err != nil {
		if errors.Is(err, db.ErrRunnerChatSessionNotFound) {
			writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "runner chat session not found", "code": "runner_chat_session_not_found"})
			return false
		}
		writeServiceError(w, r, http.StatusServiceUnavailable, "runner chat session lookup failed", err)
		return false
	}
	if !strings.HasPrefix(session.AppSessionKey, namespace) {
		writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "runner chat session not found", "code": "runner_chat_session_not_found"})
		return false
	}
	return true
}

func (s *serviceServer) handleRunnerChatSessionRead(w http.ResponseWriter, r *http.Request, store *db.DB, id string) {
	sess, err := store.GetRunnerChatSession(r.Context(), id)
	if err != nil {
		if errors.Is(err, db.ErrRunnerChatSessionNotFound) {
			writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "runner chat session not found", "code": "runner_chat_session_not_found"})
			return
		}
		writeServiceError(w, r, http.StatusServiceUnavailable, "runner chat session lookup failed", err)
		return
	}
	writeServiceValue(w, http.StatusOK, controlplane.BuildRunnerChatSessionResponse(sess))
}

func (s *serviceServer) handleRunnerChatTurnsList(w http.ResponseWriter, r *http.Request, store *db.DB, sessionID string) {
	limit := db.DefaultRunnerChatTurnListLimit
	if values, present := r.URL.Query()["limit"]; present {
		raw := strings.TrimSpace(r.URL.Query().Get("limit"))
		n, err := strconv.Atoi(raw)
		if len(values) != 1 || raw == "" || err != nil || n <= 0 || n > db.MaxRunnerChatTurnListLimit {
			writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid limit", "code": "validation_failed"})
			return
		}
		limit = n
	}
	turns, err := store.ListRunnerChatTurns(r.Context(), sessionID, limit)
	if err != nil {
		writeServiceError(w, r, http.StatusServiceUnavailable, "runner chat turns list unavailable", err)
		return
	}
	out := make([]map[string]any, 0, len(turns))
	for _, t := range turns {
		out = append(out, controlplane.BuildRunnerChatTurnResponse(t))
	}
	writeServiceValue(w, http.StatusOK, map[string]any{"turns": out})
}

func (s *serviceServer) handleRunnerChatTurnStart(w http.ResponseWriter, r *http.Request, sessionID string) {
	if s.runnerChatWriteUnavailable() {
		writeServiceJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "runner manager is disabled", "code": "runner_disabled"})
		return
	}
	limitServiceRequestBody(w, r, serviceRunnerChatBodyLimit)
	var req runnerChatStartTurnRequest
	if err := decodeServiceJSONLoose(r.Body, &req); err != nil {
		writeServiceError(w, r, http.StatusBadRequest, "invalid request", err)
		return
	}
	if strings.TrimSpace(req.UserMessage) == "" {
		writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": "user_message required"})
		return
	}
	attachments := decodeServiceAttachments(req.Attachments)
	if err := turns.ValidateAttachments(attachments); err != nil {
		writeServiceError(w, r, http.StatusBadRequest, "invalid attachments", err)
		return
	}
	store := s.control().DB
	if store == nil {
		writeServiceJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "database unavailable"})
		return
	}
	sess, err := store.GetRunnerChatSession(r.Context(), sessionID)
	if err != nil {
		if errors.Is(err, db.ErrRunnerChatSessionNotFound) {
			writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "runner chat session not found", "code": "runner_chat_session_not_found"})
			return
		}
		writeServiceError(w, r, http.StatusServiceUnavailable, "runner chat session lookup unavailable", err)
		return
	}
	promptMessage := strings.TrimSpace(req.UserMessage)
	promptMessageFinal := false
	var compiled app.RunnerPromptCompileResult
	runtime := s.runtimeComponents()
	if runtime.turnOrchestrator != nil {
		continuation := runners.ContinuationMode(strings.TrimSpace(req.ContinuationMode))
		if continuation == "" {
			continuation = runners.ContinuationMode(sess.ContinuationMode)
		}
		if strings.TrimSpace(req.Cwd) != "" {
			if req.Meta == nil {
				req.Meta = map[string]any{}
			}
			req.Meta["_cwd"] = req.Cwd
		}
		compileCtx, cancelCompile := context.WithTimeout(r.Context(), serviceRunnerChatPromptCompileTimeout)
		var compileErr error
		compiled, compileErr = runtime.turnOrchestrator.CompileRunnerChatPromptForSession(compileCtx, sess.ID, sess.AppSessionKey, req.UserMessage, "user_message", req.Meta, continuation)
		cancelCompile()
		if compileErr != nil {
			if r.Context().Err() != nil {
				writeServiceError(w, r, http.StatusServiceUnavailable, "runner chat prompt unavailable", compileErr)
				return
			}
			log.Printf("runner chat: prompt compile failed; falling back to raw user message: session=%s err=%v", sess.ID, compileErr)
			if req.Meta == nil {
				req.Meta = map[string]any{}
			}
			req.Meta["runner_prompt_compile_error"] = compileErr.Error()
			req.Meta["runner_prompt_compile_fallback"] = true
		} else if compileCtx.Err() == context.DeadlineExceeded {
			log.Printf("runner chat: prompt compile timed out; falling back to raw user message: session=%s", sess.ID)
			if req.Meta == nil {
				req.Meta = map[string]any{}
			}
			req.Meta["runner_prompt_compile_error"] = "prompt compile timed out"
			req.Meta["runner_prompt_compile_fallback"] = true
		} else {
			promptMessage = compiled.CompiledPrompt
			promptMessageFinal = true
		}
	}
	startReq := runners.StartTurnRequest{
		ContinuationMode:   runners.ContinuationMode(strings.TrimSpace(req.ContinuationMode)),
		UserMessage:        req.UserMessage,
		PromptMessage:      promptMessage,
		PromptMessageFinal: promptMessageFinal,
		MemoryRefresh:      compiled.MemoryRefresh,
		MemoryDebug:        compiled.MemoryDebug,
		Attachments:        attachments,
		Model:              req.Model,
		Mode:               req.Mode,
		Isolation:          req.Isolation,
		Cwd:                req.Cwd,
		MaxTurns:           req.MaxTurns,
		TimeoutSeconds:     req.TimeoutSeconds,
		Meta:               req.Meta,
		ApprovalToken:      serviceFirstNonEmpty(req.ApprovalToken, serviceApprovalTokenFromRequest(r)),
		ApprovalAutopilot:  runners.ResolveRunnerApprovalAutopilot(req.ApprovalAutopilot),
	}
	if thinking := strings.ToLower(strings.TrimSpace(req.ThinkingLevel)); thinking != "" {
		if startReq.Meta == nil {
			startReq.Meta = map[string]any{}
		}
		startReq.Meta["runner_thinking_level"] = thinking
	}
	if permission, ok := runners.NormalizeRunnerPermissionRequest(runners.RunnerPermissionRequest{
		RunnerID:   strings.TrimSpace(req.RunnerPermission.RunnerID),
		Kind:       strings.TrimSpace(req.RunnerPermission.Kind),
		Access:     strings.TrimSpace(req.RunnerPermission.Access),
		TargetPath: strings.TrimSpace(req.RunnerPermission.TargetPath),
	}); ok {
		startReq.RunnerPermission = &permission
	}
	if runtime.chatManager == nil || runtime.chatManager.Manager == nil {
		writeServiceJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "runner manager is disabled", "code": "runner_disabled"})
		return
	}
	result, err := runtime.chatManager.StartTurn(r.Context(), sessionID, startReq)
	if err != nil {
		var approvalErr *tools.ApprovalRequiredError
		switch {
		case errors.As(err, &approvalErr):
			writeServiceJSON(w, http.StatusConflict, map[string]any{"error": err.Error(), "code": "approval_required", "status": "approval_required", "approval_id": approvalErr.RequestID, "request_id": approvalErr.RequestID})
		case errors.Is(err, runners.ErrUnsupportedNativeSession):
			writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": "native continuation not supported by this runner", "code": "unsupported_native_session"})
		case errors.Is(err, db.ErrRunnerChatSessionNotFound):
			writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "runner chat session not found", "code": "runner_chat_session_not_found"})
		case errors.Is(err, db.ErrRunnerChatTurnActive):
			writeServiceJSON(w, http.StatusConflict, map[string]any{"error": "another turn is already active for this session", "code": "runner_chat_turn_active"})
		default:
			writeRunnerChatStartError(w, r, err)
		}
		return
	}
	writeServiceValue(w, http.StatusAccepted, map[string]any{
		"session_id": result.Session.ID,
		"turn_id":    result.Turn.ID,
		"job_id":     result.JobID,
		"status":     result.Turn.Status,
	})
}

func writeRunnerChatStartError(w http.ResponseWriter, r *http.Request, err error) {
	public := "start runner chat turn failed"
	payload := serviceErrorPayload(r, public)
	payload["code"] = serviceCodeValidationFailed
	if detail := runnerChatStartErrorDetail(err); detail != "" {
		payload["detail"] = detail
	}
	if err != nil {
		log.Printf("service %s %s: %v", r.Method, r.URL.Path, err)
	}
	writeServiceJSON(w, http.StatusBadRequest, payload)
}

func runnerChatStartErrorDetail(err error) string {
	if err == nil {
		return ""
	}
	detail := strings.TrimSpace(err.Error())
	lower := strings.ToLower(detail)
	for _, safe := range []string{
		"invalid cwd:",
		"unknown runner ",
		"runner is disabled",
		"is disabled by config",
		"is not installed",
		"is not authenticated",
		"is not functional",
	} {
		if strings.Contains(lower, safe) {
			return detail
		}
	}
	return ""
}

func (s *serviceServer) handleRunnerChatTurnRead(w http.ResponseWriter, r *http.Request, store *db.DB, sessionID, turnID string) {
	turn, ok := s.loadRunnerChatTurnForSession(w, r, store, sessionID, turnID)
	if !ok {
		return
	}
	writeServiceValue(w, http.StatusOK, controlplane.BuildRunnerChatTurnResponse(turn))
}

func (s *serviceServer) handleRunnerChatTurnEvents(w http.ResponseWriter, r *http.Request, store *db.DB, sessionID, turnID string) {
	if _, ok := s.loadRunnerChatTurnForSession(w, r, store, sessionID, turnID); !ok {
		return
	}
	afterSeq := int64(0)
	if raw := strings.TrimSpace(r.URL.Query().Get("after_seq")); raw != "" {
		n, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || n < 0 {
			writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid after_seq"})
			return
		}
		afterSeq = n
	}
	limit := db.DefaultRunnerChatEventListLimit
	if values, present := r.URL.Query()["limit"]; present {
		raw := strings.TrimSpace(r.URL.Query().Get("limit"))
		n, err := strconv.Atoi(raw)
		if len(values) != 1 || raw == "" || err != nil || n <= 0 || n > db.MaxRunnerChatEventListLimit {
			writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid limit", "code": "validation_failed"})
			return
		}
		limit = n
	}
	events, err := store.ListRunnerChatEvents(r.Context(), turnID, afterSeq, limit)
	if err != nil {
		writeServiceError(w, r, http.StatusServiceUnavailable, "runner chat events unavailable", err)
		return
	}
	writeServiceValue(w, http.StatusOK, controlplane.BuildRunnerChatEventListResponse(events))
}

func (s *serviceServer) loadRunnerChatTurnForSession(w http.ResponseWriter, r *http.Request, store *db.DB, sessionID, turnID string) (db.RunnerChatTurn, bool) {
	turn, err := store.GetRunnerChatTurn(r.Context(), turnID)
	if err != nil {
		if errors.Is(err, db.ErrRunnerChatTurnNotFound) {
			writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "runner chat turn not found", "code": "runner_chat_turn_not_found"})
			return db.RunnerChatTurn{}, false
		}
		writeServiceError(w, r, http.StatusServiceUnavailable, "runner chat turn lookup failed", err)
		return db.RunnerChatTurn{}, false
	}
	if turn.SessionID != sessionID {
		writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "runner chat turn not found", "code": "runner_chat_turn_not_found"})
		return db.RunnerChatTurn{}, false
	}
	return turn, true
}

// handleRunnerChatTurnStream streams runner chat events as SSE. It first
// flushes any persisted events past after_seq, then polls the store at a
// fixed interval until the turn reaches a terminal status.
//
// NOTE: This implementation polls the chat-event store rather than tapping
// into the live JobRegistry pub-sub channel. ChatManager already mirrors
// every job event into runner_chat_events synchronously, so polling is
// correctness-equivalent and avoids re-implementing the channel-fanout
// pattern. Future work: subscribe to JobRegistry directly for lower latency.
func (s *serviceServer) handleRunnerChatTurnStream(w http.ResponseWriter, r *http.Request, store *db.DB, sessionID, turnID string) {
	turn, ok := s.loadRunnerChatTurnForSession(w, r, store, sessionID, turnID)
	if !ok {
		return
	}
	afterSeq := int64(0)
	if raw := strings.TrimSpace(r.URL.Query().Get("after_seq")); raw != "" {
		if n, err := strconv.ParseInt(raw, 10, 64); err == nil && n >= 0 {
			afterSeq = n
		}
	}
	w.Header().Set("X-Or3-Turn-Id", turnID)
	if err := beginSSE(w); err != nil {
		writeServiceError(w, r, http.StatusInternalServerError, "streaming is not supported", err)
		return
	}
	flush := func(events []db.RunnerChatEvent) (int64, bool) {
		max := afterSeq
		for _, ev := range events {
			payload := controlplane.BuildRunnerChatEventResponse(ev)
			if err := writeSSEEvent(w, ev.Type, payload); err != nil {
				return max, false
			}
			if ev.Seq > max {
				max = ev.Seq
			}
		}
		return max, true
	}
	flushPending := func(limit int) bool {
		for {
			events, err := store.ListRunnerChatEvents(r.Context(), turnID, afterSeq, limit)
			if err != nil {
				return false
			}
			if len(events) == 0 {
				return true
			}
			next, ok := flush(events)
			if !ok {
				return false
			}
			afterSeq = next
			if len(events) < limit {
				return true
			}
		}
	}
	// Initial flush of all persisted history. A reconnect to a completed turn
	// must not silently stop after the first page of tool/content events.
	if !flushPending(db.MaxRunnerChatEventListLimit) {
		return
	}
	if isTerminalRunnerChatStatus(turn.Status) {
		_ = writeSSEEvent(w, "done", map[string]any{"status": turn.Status})
		return
	}
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	lastKeepalive := time.Now()
	const runnerChatStreamKeepaliveInterval = 15 * time.Second
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			beforeSeq := afterSeq
			if !flushPending(db.DefaultRunnerChatEventListLimit) {
				_ = writeSSEEvent(w, "error", map[string]any{"error": "runner chat events unavailable"})
				return
			}
			if afterSeq > beforeSeq {
				lastKeepalive = time.Now()
			}
			cur, err := store.GetRunnerChatTurn(r.Context(), turnID)
			if err == nil && isTerminalRunnerChatStatus(cur.Status) {
				// Drain any final events recorded after the last poll.
				_ = flushPending(db.DefaultRunnerChatEventListLimit)
				_ = writeSSEEvent(w, "done", map[string]any{
					"status":               cur.Status,
					"final_text":           cur.FinalText,
					"error_message":        cur.ErrorMessage,
					"assistant_message_id": cur.AssistantMessageID,
				})
				return
			}
			if err == nil &&
				!isTerminalRunnerChatStatus(cur.Status) &&
				time.Since(lastKeepalive) >= runnerChatStreamKeepaliveInterval {
				if err := writeSSEEvent(w, "keepalive", map[string]any{
					"status":  cur.Status,
					"turn_id": turnID,
				}); err != nil {
					return
				}
				lastKeepalive = time.Now()
			}
		}
	}
}

func (s *serviceServer) handleRunnerChatTurnAbort(w http.ResponseWriter, r *http.Request, store *db.DB, sessionID, turnID string) {
	if s.runnerChatWriteUnavailable() {
		writeServiceJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "runner manager is disabled", "code": "runner_disabled"})
		return
	}
	if _, ok := s.loadRunnerChatTurnForSession(w, r, store, sessionID, turnID); !ok {
		return
	}
	chatManager := s.runtimeComponents().chatManager
	if chatManager == nil || chatManager.Manager == nil {
		writeServiceJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "runner manager is disabled", "code": "runner_disabled"})
		return
	}
	if err := chatManager.AbortTurn(r.Context(), turnID); err != nil {
		if errors.Is(err, db.ErrRunnerChatTurnNotFound) {
			writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "runner chat turn not found", "code": "runner_chat_turn_not_found"})
			return
		}
		writeServiceError(w, r, http.StatusServiceUnavailable, "abort failed", err)
		return
	}
	writeServiceJSON(w, http.StatusAccepted, map[string]any{"status": "aborting"})
}

func (s *serviceServer) handleRunnerChatTurnDecision(w http.ResponseWriter, r *http.Request, store *db.DB, sessionID, turnID, decision string) {
	if s.runnerChatWriteUnavailable() {
		writeServiceJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "runner manager is disabled", "code": "runner_disabled"})
		return
	}
	if _, ok := s.loadRunnerChatTurnForSession(w, r, store, sessionID, turnID); !ok {
		return
	}
	limitServiceRequestBody(w, r, serviceRunnerChatBodyLimit)
	var body struct {
		Note         string `json:"note"`
		AllowSession bool   `json:"allow_session"`
	}
	if r.ContentLength != 0 {
		if err := decodeServiceJSONLoose(r.Body, &body); err != nil {
			writeServiceError(w, r, http.StatusBadRequest, "invalid request", err)
			return
		}
	}
	actor := serviceAuthIdentityFromContext(r.Context()).Actor
	chatManager := s.runtimeComponents().chatManager
	if chatManager == nil || chatManager.Manager == nil {
		writeServiceJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "runner manager is disabled", "code": "runner_disabled"})
		return
	}
	result, err := chatManager.RespondToTurnApproval(r.Context(), turnID, runners.RespondToTurnApprovalOpts{
		Decision:     decision,
		Note:         body.Note,
		AllowSession: body.AllowSession,
		Actor:        actor,
	})
	if err != nil {
		switch {
		case errors.Is(err, db.ErrRunnerChatTurnNotFound):
			writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "runner chat turn not found", "code": "runner_chat_turn_not_found"})
		case errors.Is(err, db.ErrRunnerChatSessionNotFound):
			writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "runner chat session not found", "code": "runner_chat_session_not_found"})
		default:
			writeServiceError(w, r, http.StatusBadRequest, "approval decision failed", err)
		}
		return
	}
	response := map[string]any{
		"status":            "ok",
		"decision":          decision,
		"route":             result.Route,
		"approval_id":       result.ApprovalID,
		"native_continued":  result.NativeContinued,
		"fallback_to_token": result.FallbackToToken,
		"allowlist_session": result.AllowlistSession,
	}
	if result.AllowlistID != 0 {
		response["allowlist_id"] = result.AllowlistID
	}
	if result.Token != "" {
		response["token"] = result.Token
	}
	writeServiceJSON(w, http.StatusAccepted, response)
}

func isTerminalRunnerChatStatus(status string) bool {
	switch status {
	case db.RunnerChatTurnStatusSucceeded,
		db.RunnerChatTurnStatusApprovalRequired,
		db.RunnerChatTurnStatusFailed,
		db.RunnerChatTurnStatusAborted,
		db.RunnerChatTurnStatusTimedOut:
		return true
	}
	return false
}
