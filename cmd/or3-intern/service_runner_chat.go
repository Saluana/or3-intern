package main

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"or3-intern/internal/agentcli"
	"or3-intern/internal/controlplane"
	"or3-intern/internal/db"
)

const serviceRunnerChatBodyLimit = 64 * 1024

func (s *serviceServer) runnerChatWriteUnavailable() bool {
	return s == nil || s.chatManager == nil || s.chatManager.Manager == nil
}

// runnerChatCreateSessionRequest is the body for POST /runner-chat/sessions.
type runnerChatCreateSessionRequest struct {
	AppSessionKey    string `json:"app_session_key"`
	RunnerID         string `json:"runner_id"`
	ContinuationMode string `json:"continuation_mode"`
	Model            string `json:"model"`
	Mode             string `json:"mode"`
	Isolation        string `json:"isolation"`
	Cwd              string `json:"cwd"`
	MaxTurns         int    `json:"max_turns"`
}

// runnerChatStartTurnRequest is the body for POST /runner-chat/sessions/:id/turns.
type runnerChatStartTurnRequest struct {
	UserMessage      string         `json:"user_message"`
	ContinuationMode string         `json:"continuation_mode"`
	Model            string         `json:"model"`
	Mode             string         `json:"mode"`
	Isolation        string         `json:"isolation"`
	Cwd              string         `json:"cwd"`
	MaxTurns         int            `json:"max_turns"`
	TimeoutSeconds   int            `json:"timeout_seconds"`
	Meta             map[string]any `json:"meta"`
}

// handleRunnerChatSessions dispatches the runner-chat session/turn API:
//
//	POST /internal/v1/runner-chat/sessions
//	GET  /internal/v1/runner-chat/sessions/:id
//	GET  /internal/v1/runner-chat/sessions/:id/turns
//	POST /internal/v1/runner-chat/sessions/:id/turns
//	GET  /internal/v1/runner-chat/sessions/:id/turns/:turn_id
//	GET  /internal/v1/runner-chat/sessions/:id/turns/:turn_id/events
//	GET  /internal/v1/runner-chat/sessions/:id/turns/:turn_id/stream  (SSE)
//	POST /internal/v1/runner-chat/sessions/:id/turns/:turn_id/abort
func (s *serviceServer) handleRunnerChatSessions(w http.ResponseWriter, r *http.Request) {
	if s.chatManager == nil || s.chatManager.DB == nil {
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
		if r.Method != http.MethodPost {
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		s.handleRunnerChatSessionCreate(w, r)
		return
	}
	rel := strings.TrimPrefix(path, "/internal/v1/runner-chat/sessions/")
	parts := strings.Split(strings.Trim(rel, "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "not found"})
		return
	}
	sessionID := parts[0]
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
		default:
			writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "not found"})
		}
	default:
		writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "not found"})
	}
}

func (s *serviceServer) handleRunnerChatSessionCreate(w http.ResponseWriter, r *http.Request) {
	if s.runnerChatWriteUnavailable() {
		writeServiceJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "agent CLI manager is disabled", "code": "agent_cli_disabled"})
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
	if strings.TrimSpace(req.RunnerID) == "" {
		writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": "runner_id required"})
		return
	}
	sess, err := s.chatManager.EnsureSession(r.Context(), agentcli.StartTurnRequest{
		AppSessionKey:    req.AppSessionKey,
		RunnerID:         req.RunnerID,
		ContinuationMode: agentcli.ContinuationMode(strings.TrimSpace(req.ContinuationMode)),
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
	limit := 0
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n <= 0 {
			writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid limit"})
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
		writeServiceJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "agent CLI manager is disabled", "code": "agent_cli_disabled"})
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
	startReq := agentcli.StartTurnRequest{
		ContinuationMode: agentcli.ContinuationMode(strings.TrimSpace(req.ContinuationMode)),
		UserMessage:      req.UserMessage,
		Model:            req.Model,
		Mode:             req.Mode,
		Isolation:        req.Isolation,
		Cwd:              req.Cwd,
		MaxTurns:         req.MaxTurns,
		TimeoutSeconds:   req.TimeoutSeconds,
		Meta:             req.Meta,
	}
	result, err := s.chatManager.StartTurn(r.Context(), sessionID, startReq)
	if err != nil {
		switch {
		case errors.Is(err, agentcli.ErrUnsupportedNativeSession):
			writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": "native continuation not supported by this runner", "code": "unsupported_native_session"})
		case errors.Is(err, db.ErrRunnerChatSessionNotFound):
			writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "runner chat session not found", "code": "runner_chat_session_not_found"})
		case errors.Is(err, db.ErrRunnerChatTurnActive):
			writeServiceJSON(w, http.StatusConflict, map[string]any{"error": "another turn is already active for this session", "code": "runner_chat_turn_active"})
		default:
			writeServiceError(w, r, http.StatusBadRequest, "start runner chat turn failed", err)
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
	limit := 200
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n <= 0 {
			writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid limit"})
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
	// Initial flush of persisted history.
	events, err := store.ListRunnerChatEvents(r.Context(), turnID, afterSeq, 1000)
	if err == nil {
		if next, ok := flush(events); ok {
			afterSeq = next
		} else {
			return
		}
	}
	if isTerminalRunnerChatStatus(turn.Status) {
		_ = writeSSEEvent(w, "done", map[string]any{"status": turn.Status})
		return
	}
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			batch, err := store.ListRunnerChatEvents(r.Context(), turnID, afterSeq, 200)
			if err != nil {
				_ = writeSSEEvent(w, "error", map[string]any{"error": fmt.Sprintf("%v", err)})
				return
			}
			if len(batch) > 0 {
				next, ok := flush(batch)
				if !ok {
					return
				}
				afterSeq = next
			}
			cur, err := store.GetRunnerChatTurn(r.Context(), turnID)
			if err == nil && isTerminalRunnerChatStatus(cur.Status) {
				// Drain any final events recorded after the last poll.
				if tail, err := store.ListRunnerChatEvents(r.Context(), turnID, afterSeq, 200); err == nil && len(tail) > 0 {
					_, _ = flush(tail)
				}
				_ = writeSSEEvent(w, "done", map[string]any{
					"status":               cur.Status,
					"final_text":           cur.FinalText,
					"error_message":        cur.ErrorMessage,
					"assistant_message_id": cur.AssistantMessageID,
				})
				return
			}
		}
	}
}

func (s *serviceServer) handleRunnerChatTurnAbort(w http.ResponseWriter, r *http.Request, store *db.DB, sessionID, turnID string) {
	if s.runnerChatWriteUnavailable() {
		writeServiceJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "agent CLI manager is disabled", "code": "agent_cli_disabled"})
		return
	}
	if _, ok := s.loadRunnerChatTurnForSession(w, r, store, sessionID, turnID); !ok {
		return
	}
	if err := s.chatManager.AbortTurn(r.Context(), turnID); err != nil {
		if errors.Is(err, db.ErrRunnerChatTurnNotFound) {
			writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "runner chat turn not found", "code": "runner_chat_turn_not_found"})
			return
		}
		writeServiceError(w, r, http.StatusServiceUnavailable, "abort failed", err)
		return
	}
	writeServiceJSON(w, http.StatusAccepted, map[string]any{"status": "aborting"})
}

func isTerminalRunnerChatStatus(status string) bool {
	switch status {
	case db.RunnerChatTurnStatusSucceeded,
		db.RunnerChatTurnStatusFailed,
		db.RunnerChatTurnStatusAborted,
		db.RunnerChatTurnStatusTimedOut:
		return true
	}
	return false
}
