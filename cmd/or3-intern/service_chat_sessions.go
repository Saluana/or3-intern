package main

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"or3-intern/internal/agentcli"
	"or3-intern/internal/controlplane"
	"or3-intern/internal/db"
)

const serviceChatSessionsBodyLimit = 64 * 1024

// handleChatRunners exposes runner-discovery filtered/decorated for the chat
// transport selector. or3-intern is always present and reported available.
//
//	GET /internal/v1/chat-runners
func (s *serviceServer) handleChatRunners(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		return
	}
	appSvc := s.app()
	detected, err := appSvc.DetectAgentCLIRunners(r.Context())
	if err != nil {
		writeServiceError(w, r, http.StatusServiceUnavailable, "agent runner detection unavailable", err)
		return
	}
	infoByID := make(map[string]agentcli.RunnerInfo, len(detected))
	for _, info := range detected {
		infoByID[info.ID] = info
	}
	specs := agentcli.AllRunners()
	out := make([]map[string]any, 0, len(specs))
	for _, spec := range specs {
		if !spec.Supports.Chat.ChatSelectable {
			continue
		}
		if s.agentCLIManager == nil && spec.ID != agentcli.RunnerOR3 {
			continue
		}
		info := infoByID[string(spec.ID)]
		out = append(out, controlplane.BuildChatRunner(spec, info, "", "", "", ""))
	}
	writeServiceValue(w, http.StatusOK, map[string]any{"runners": out})
}

// chatSessionsCreateRequest is the body for POST /chat-sessions.
type chatSessionsCreateRequest struct {
	SessionKey  string `json:"session_key"`
	Title       string `json:"title"`
	RunnerID    string `json:"runner_id"`
	RunnerLabel string `json:"runner_label"`
}

// chatSessionsRenameRequest is the body for PATCH /chat-sessions/:key.
type chatSessionsRenameRequest struct {
	Title    *string `json:"title"`
	Archived *bool   `json:"archived"`
}

// chatSessionsForkRequest is the body for POST /chat-sessions/:key/fork.
type chatSessionsForkRequest struct {
	NewSessionKey         string `json:"new_session_key"`
	AnchorMessageID       int64  `json:"anchor_message_id"`
	TargetRunnerID        string `json:"target_runner_id"`
	Title                 string `json:"title"`
	AllowIncompleteAnchor bool   `json:"allow_incomplete_anchor"`
	ForkStrategy          string `json:"fork_strategy"`
}

// handleChatSessions dispatches the chat-session metadata API:
//
//	GET    /internal/v1/chat-sessions
//	POST   /internal/v1/chat-sessions
//	GET    /internal/v1/chat-sessions/:key
//	PATCH  /internal/v1/chat-sessions/:key
//	GET    /internal/v1/chat-sessions/:key/messages
//	GET    /internal/v1/chat-sessions/:key/messages/stream
//	POST   /internal/v1/chat-sessions/:key/fork
func (s *serviceServer) handleChatSessions(w http.ResponseWriter, r *http.Request) {
	store := s.control().DB
	if store == nil {
		writeServiceJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "database unavailable"})
		return
	}
	path := r.URL.Path
	if path == "/internal/v1/chat-sessions" || path == "/internal/v1/chat-sessions/" {
		switch r.Method {
		case http.MethodGet:
			s.handleChatSessionsList(w, r, store)
		case http.MethodPost:
			s.handleChatSessionsCreate(w, r, store)
		default:
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		}
		return
	}
	rel := strings.TrimPrefix(path, "/internal/v1/chat-sessions/")
	parts := strings.SplitN(strings.Trim(rel, "/"), "/", 2)
	sessionKey := strings.TrimSpace(parts[0])
	if sessionKey == "" {
		writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "not found"})
		return
	}
	tail := ""
	if len(parts) == 2 {
		tail = parts[1]
	}
	switch tail {
	case "":
		switch r.Method {
		case http.MethodGet:
			s.handleChatSessionRead(w, r, store, sessionKey)
		case http.MethodPatch:
			s.handleChatSessionPatch(w, r, store, sessionKey)
		default:
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		}
	case "messages":
		if r.Method != http.MethodGet {
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		s.handleChatSessionMessages(w, r, store, sessionKey)
	case "messages/stream":
		if r.Method != http.MethodGet {
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		s.streamChatSessionMessages(w, r, store, sessionKey)
	case "fork":
		if r.Method != http.MethodPost {
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		s.handleChatSessionFork(w, r, store, sessionKey)
	default:
		writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "not found"})
	}
}

func (s *serviceServer) handleChatSessionsList(w http.ResponseWriter, r *http.Request, store *db.DB) {
	q := r.URL.Query()
	filter := db.ChatSessionListFilter{
		HostID:         strings.TrimSpace(q.Get("host_id")),
		RunnerID:       strings.TrimSpace(q.Get("runner_id")),
		IncludeArchive: q.Get("include_archived") == "1" || q.Get("include_archived") == "true",
		OnlyArchived:   q.Get("only_archived") == "1" || q.Get("only_archived") == "true",
		Search:         strings.TrimSpace(q.Get("q")),
	}
	if raw := strings.TrimSpace(q.Get("limit")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n <= 0 {
			writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid limit"})
			return
		}
		filter.Limit = n
	}
	if err := store.BackfillExternalChannelChatSessionMeta(r.Context()); err != nil {
		writeServiceError(w, r, http.StatusServiceUnavailable, "chat session metadata backfill failed", err)
		return
	}
	rows, err := store.ListChatSessions(r.Context(), filter)
	if err != nil {
		writeServiceError(w, r, http.StatusServiceUnavailable, "chat session list unavailable", err)
		return
	}
	writeServiceValue(w, http.StatusOK, controlplane.BuildChatSessionListResponse(rows))
}

func (s *serviceServer) handleChatSessionsCreate(w http.ResponseWriter, r *http.Request, store *db.DB) {
	limitServiceRequestBody(w, r, serviceChatSessionsBodyLimit)
	var req chatSessionsCreateRequest
	if err := decodeServiceJSONLoose(r.Body, &req); err != nil {
		writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid request", "code": "validation_failed"})
		return
	}
	if strings.TrimSpace(req.SessionKey) == "" {
		writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": "session_key required"})
		return
	}
	meta, err := store.UpsertChatSessionMeta(r.Context(), db.ChatSessionMeta{
		SessionKey:  req.SessionKey,
		Title:       req.Title,
		RunnerID:    req.RunnerID,
		RunnerLabel: req.RunnerLabel,
	})
	if err != nil {
		writeServiceError(w, r, http.StatusServiceUnavailable, "chat session create failed", err)
		return
	}
	writeServiceValue(w, http.StatusCreated, controlplane.BuildChatSessionMetaResponse(meta))
}

func (s *serviceServer) handleChatSessionRead(w http.ResponseWriter, r *http.Request, store *db.DB, sessionKey string) {
	meta, err := store.GetChatSessionMeta(r.Context(), sessionKey)
	if err != nil {
		if errors.Is(err, db.ErrChatSessionNotFound) {
			writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "chat session not found", "code": "chat_session_not_found"})
			return
		}
		writeServiceError(w, r, http.StatusServiceUnavailable, "chat session lookup failed", err)
		return
	}
	writeServiceValue(w, http.StatusOK, controlplane.BuildChatSessionMetaResponse(meta))
}

func (s *serviceServer) handleChatSessionPatch(w http.ResponseWriter, r *http.Request, store *db.DB, sessionKey string) {
	limitServiceRequestBody(w, r, serviceChatSessionsBodyLimit)
	var req chatSessionsRenameRequest
	if err := decodeServiceJSONLoose(r.Body, &req); err != nil {
		writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid request", "code": "validation_failed"})
		return
	}
	if req.Title == nil && req.Archived == nil {
		writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": "title or archived required"})
		return
	}
	if req.Title != nil {
		if err := store.RenameChatSession(r.Context(), sessionKey, *req.Title); err != nil {
			if errors.Is(err, db.ErrChatSessionNotFound) {
				writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "chat session not found", "code": "chat_session_not_found"})
				return
			}
			writeServiceError(w, r, http.StatusServiceUnavailable, "chat session rename failed", err)
			return
		}
	}
	if req.Archived != nil {
		if err := store.ArchiveChatSession(r.Context(), sessionKey, *req.Archived); err != nil {
			if errors.Is(err, db.ErrChatSessionNotFound) {
				writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "chat session not found", "code": "chat_session_not_found"})
				return
			}
			writeServiceError(w, r, http.StatusServiceUnavailable, "chat session archive failed", err)
			return
		}
	}
	meta, err := store.GetChatSessionMeta(r.Context(), sessionKey)
	if err != nil {
		writeServiceError(w, r, http.StatusServiceUnavailable, "chat session lookup failed", err)
		return
	}
	writeServiceValue(w, http.StatusOK, controlplane.BuildChatSessionMetaResponse(meta))
}

func (s *serviceServer) handleChatSessionMessages(w http.ResponseWriter, r *http.Request, store *db.DB, sessionKey string) {
	q := r.URL.Query()
	limit := 0
	if raw := strings.TrimSpace(q.Get("limit")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n <= 0 {
			writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid limit"})
			return
		}
		limit = n
	}
	afterID := int64(0)
	if raw := strings.TrimSpace(q.Get("after_id")); raw != "" {
		n, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || n < 0 {
			writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid after_id"})
			return
		}
		afterID = n
	}
	page, err := store.ListChatMessages(r.Context(), sessionKey, afterID, limit)
	if err != nil {
		writeServiceError(w, r, http.StatusServiceUnavailable, "chat messages unavailable", err)
		return
	}
	writeServiceValue(w, http.StatusOK, controlplane.BuildChatMessagePageResponse(page))
}

func (s *serviceServer) streamChatSessionMessages(w http.ResponseWriter, r *http.Request, store *db.DB, sessionKey string) {
	q := r.URL.Query()
	afterID := int64(0)
	if raw := strings.TrimSpace(q.Get("after_id")); raw != "" {
		n, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || n < 0 {
			writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid after_id"})
			return
		}
		afterID = n
	}
	pollEvery := time.Second
	if raw := strings.TrimSpace(q.Get("poll_ms")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 250 || n > 10000 {
			writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid poll_ms"})
			return
		}
		pollEvery = time.Duration(n) * time.Millisecond
	}
	if err := beginSSE(w); err != nil {
		writeServiceError(w, r, http.StatusInternalServerError, "streaming is not supported", err)
		return
	}
	ticker := time.NewTicker(pollEvery)
	defer ticker.Stop()
	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()

	flush := func() error {
		page, err := store.ListChatMessages(r.Context(), sessionKey, afterID, 100)
		if err != nil {
			return err
		}
		for _, message := range page.Messages {
			if message.ID > afterID {
				afterID = message.ID
			}
			if err := writeSSEEvent(w, "message", controlplane.BuildChatMessageResponse(message)); err != nil {
				return err
			}
		}
		return nil
	}
	if err := flush(); err != nil {
		_ = writeSSEEvent(w, "error", map[string]any{"error": "chat messages unavailable"})
		return
	}
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			if err := flush(); err != nil {
				_ = writeSSEEvent(w, "error", map[string]any{"error": "chat messages unavailable"})
				return
			}
		case <-heartbeat.C:
			if err := writeSSEEvent(w, "heartbeat", map[string]any{"session_key": sessionKey, "after_id": afterID}); err != nil {
				return
			}
		}
	}
}

func (s *serviceServer) handleChatSessionFork(w http.ResponseWriter, r *http.Request, store *db.DB, sessionKey string) {
	limitServiceRequestBody(w, r, serviceChatSessionsBodyLimit)
	var req chatSessionsForkRequest
	if err := decodeServiceJSONLoose(r.Body, &req); err != nil {
		writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid request", "code": "validation_failed"})
		return
	}
	if strings.TrimSpace(req.NewSessionKey) == "" {
		writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": "new_session_key required"})
		return
	}
	if req.AnchorMessageID <= 0 {
		writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": "anchor_message_id required"})
		return
	}
	meta, _, err := store.ForkChatSession(r.Context(), db.ForkChatSessionRequest{
		SourceSessionKey:      sessionKey,
		NewSessionKey:         req.NewSessionKey,
		AnchorMessageID:       req.AnchorMessageID,
		TargetRunnerID:        req.TargetRunnerID,
		Title:                 req.Title,
		AllowIncompleteAnchor: req.AllowIncompleteAnchor,
		ForkStrategy:          req.ForkStrategy,
	})
	if err != nil {
		switch {
		case errors.Is(err, db.ErrChatSessionNotFound):
			writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "source chat session not found", "code": "chat_session_not_found"})
		case errors.Is(err, db.ErrInvalidForkAnchor):
			writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid fork anchor", "code": "invalid_fork_anchor"})
		case errors.Is(err, db.ErrForkAnchorIncomplete):
			writeServiceJSON(w, http.StatusConflict, map[string]any{"error": "fork anchor incomplete; pass allow_incomplete_anchor=true to truncate", "code": "fork_anchor_incomplete"})
		default:
			writeServiceError(w, r, http.StatusServiceUnavailable, "fork failed", err)
		}
		return
	}
	writeServiceValue(w, http.StatusCreated, controlplane.BuildChatSessionMetaResponse(meta))
}

// decodeServiceJSONLoose decodes a JSON body without DisallowUnknownFields,
// because chat-session payloads from the frontend may evolve faster than the
// backend struct. Trailing data is still rejected.
func decodeServiceJSONLoose(body io.Reader, out any) error {
	dec := json.NewDecoder(body)
	dec.UseNumber()
	if err := dec.Decode(out); err != nil {
		return err
	}
	var extra struct{}
	if err := dec.Decode(&extra); err != io.EOF {
		return errors.New("unexpected trailing data")
	}
	return nil
}
