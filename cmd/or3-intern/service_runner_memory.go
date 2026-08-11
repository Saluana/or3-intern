package main

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"or3-intern/internal/approval"
	"or3-intern/internal/config"
	"or3-intern/internal/memorysvc"
)

func (s *serviceServer) handleRunnerMemory(w http.ResponseWriter, r *http.Request) {
	if !requireServiceRole(w, r, approval.RoleOperator) {
		return
	}
	svc := s.memoryService()
	if svc == nil {
		writeServiceJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "memory service unavailable"})
		return
	}
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/internal/v1/runner-memory"), "/")
	parts := strings.Split(path, "/")
	if len(parts) == 0 || parts[0] == "" {
		writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "runner memory route not found"})
		return
	}
	identity := serviceAuthIdentityFromContext(r.Context())
	switch parts[0] {
	case "search":
		if r.Method != http.MethodPost {
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		s.handleRunnerMemorySearch(w, r, svc, identity)
	case "notes":
		if r.Method != http.MethodPost {
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		s.handleRunnerMemoryAddNote(w, r, svc, identity)
	case "pinned":
		switch r.Method {
		case http.MethodGet:
			s.handleRunnerMemoryGetPinned(w, r, svc)
		case http.MethodPost, http.MethodPut:
			s.handleRunnerMemorySetPinned(w, r, svc, identity)
		default:
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		}
	default:
		writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "runner memory route not found"})
	}
}

func (s *serviceServer) memoryService() *memorysvc.Service {
	if s == nil {
		return nil
	}
	s.configMu.RLock()
	memorySvc := s.memorySvc
	cfg := config.Clone(s.config)
	database := s.database
	embedProvider := s.embedProvider
	s.configMu.RUnlock()
	if memorySvc != nil {
		return memorySvc
	}
	if database == nil {
		return nil
	}
	return memorysvc.New(cfg, database, embedProvider, currentEmbedFingerprint(cfg))
}

type runnerMemorySearchPayload struct {
	SessionKey      string `json:"session_key"`
	SessionKeyCamel string `json:"sessionKey"`
	Query           string `json:"query"`
	TopK            int    `json:"topK"`
	GlobalOnly      bool   `json:"global_only"`
	GlobalOnlyCamel bool   `json:"globalOnly"`
}

func (s *serviceServer) handleRunnerMemorySearch(w http.ResponseWriter, r *http.Request, svc *memorysvc.Service, identity serviceAuthIdentity) {
	limitServiceRequestBody(w, r, serviceConfigureBodyLimit)
	var payload runnerMemorySearchPayload
	if err := decodeServiceRequestBody(r.Body, &payload); err != nil {
		writeServiceRequestDecodeError(w, err)
		return
	}
	sessionKey := firstNonEmptyString(payload.SessionKey, payload.SessionKeyCamel)
	query := strings.TrimSpace(payload.Query)
	if sessionKey == "" || query == "" {
		writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": "session_key and query are required"})
		return
	}
	resp, err := svc.Search(r.Context(), memorysvc.SearchRequest{
		SessionKey: sessionKey,
		Query:      query,
		TopK:       payload.TopK,
		GlobalOnly: payload.GlobalOnly || payload.GlobalOnlyCamel,
	})
	if err != nil {
		writeServiceError(w, r, http.StatusBadGateway, "memory search failed", err)
		return
	}
	s.recordRunnerMemoryAudit(r.Context(), identity, sessionKey, "runner_memory.search", map[string]any{"query": query, "hits": len(resp.Hits)})
	writeServiceValue(w, http.StatusOK, resp)
}

type runnerMemoryNotePayload struct {
	SessionKey         string `json:"session_key"`
	SessionKeyCamel    string `json:"sessionKey"`
	Text               string `json:"text"`
	Tags               string `json:"tags"`
	SourceMessageID    int64  `json:"source_message_id"`
	SourceMessageCamel int64  `json:"sourceMessageId"`
	GlobalOnly         bool   `json:"global_only"`
	GlobalOnlyCamel    bool   `json:"globalOnly"`
	RunnerID           string `json:"runner_id"`
	RunnerIDCamel      string `json:"runnerId"`
	RunnerTurnID       string `json:"runner_turn_id"`
	RunnerTurnIDCamel  string `json:"runnerTurnId"`
}

func (s *serviceServer) handleRunnerMemoryAddNote(w http.ResponseWriter, r *http.Request, svc *memorysvc.Service, identity serviceAuthIdentity) {
	limitServiceRequestBody(w, r, serviceConfigureBodyLimit)
	var payload runnerMemoryNotePayload
	if err := decodeServiceRequestBody(r.Body, &payload); err != nil {
		writeServiceRequestDecodeError(w, err)
		return
	}
	sessionKey := firstNonEmptyString(payload.SessionKey, payload.SessionKeyCamel)
	text := strings.TrimSpace(payload.Text)
	if sessionKey == "" || text == "" {
		writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": "session_key and text are required"})
		return
	}
	resp, err := svc.AddNote(r.Context(), memorysvc.AddNoteRequest{
		SessionKey:      sessionKey,
		Text:            text,
		Tags:            payload.Tags,
		SourceMessageID: firstNonZeroInt64(payload.SourceMessageID, payload.SourceMessageCamel),
		GlobalOnly:      payload.GlobalOnly || payload.GlobalOnlyCamel,
	})
	if err != nil {
		writeServiceError(w, r, http.StatusBadGateway, "memory note failed", err)
		return
	}
	s.recordRunnerMemoryAudit(r.Context(), identity, sessionKey, "runner_memory.add_note", map[string]any{
		"note_id":        resp.ID,
		"runner_id":      strings.TrimSpace(payload.RunnerID),
		"runner_turn_id": strings.TrimSpace(firstNonEmptyString(payload.RunnerTurnID, payload.RunnerTurnIDCamel)),
	})
	writeServiceValue(w, http.StatusOK, resp)
}

func (s *serviceServer) handleRunnerMemoryGetPinned(w http.ResponseWriter, r *http.Request, svc *memorysvc.Service) {
	sessionKey := strings.TrimSpace(r.URL.Query().Get("session_key"))
	if sessionKey == "" {
		sessionKey = strings.TrimSpace(r.URL.Query().Get("sessionKey"))
	}
	key := strings.TrimSpace(r.URL.Query().Get("key"))
	globalOnly := strings.EqualFold(r.URL.Query().Get("scope"), "global")
	entries, err := svc.GetPinned(r.Context(), sessionKey, key, globalOnly)
	if err != nil {
		writeServiceError(w, r, http.StatusBadGateway, "read pinned memory failed", err)
		return
	}
	writeServiceValue(w, http.StatusOK, map[string]any{"entries": entries})
}

type runnerMemoryPinnedPayload struct {
	SessionKey      string `json:"session_key"`
	SessionKeyCamel string `json:"sessionKey"`
	Key             string `json:"key"`
	Content         string `json:"content"`
	GlobalOnly      bool   `json:"global_only"`
	GlobalOnlyCamel bool   `json:"globalOnly"`
	RunnerID        string `json:"runner_id"`
	RunnerIDCamel   string `json:"runnerId"`
}

func (s *serviceServer) handleRunnerMemorySetPinned(w http.ResponseWriter, r *http.Request, svc *memorysvc.Service, identity serviceAuthIdentity) {
	limitServiceRequestBody(w, r, serviceConfigureBodyLimit)
	var payload runnerMemoryPinnedPayload
	if err := decodeServiceRequestBody(r.Body, &payload); err != nil {
		writeServiceRequestDecodeError(w, err)
		return
	}
	sessionKey := firstNonEmptyString(payload.SessionKey, payload.SessionKeyCamel)
	if sessionKey == "" || strings.TrimSpace(payload.Key) == "" || strings.TrimSpace(payload.Content) == "" {
		writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": "session_key, key, and content are required"})
		return
	}
	if err := svc.SetPinned(r.Context(), memorysvc.SetPinnedRequest{
		SessionKey: sessionKey,
		Key:        payload.Key,
		Content:    payload.Content,
		GlobalOnly: payload.GlobalOnly || payload.GlobalOnlyCamel,
	}); err != nil {
		writeServiceError(w, r, http.StatusBadGateway, "set pinned memory failed", err)
		return
	}
	s.recordRunnerMemoryAudit(r.Context(), identity, sessionKey, "runner_memory.set_pinned", map[string]any{
		"key":       payload.Key,
		"runner_id": strings.TrimSpace(firstNonEmptyString(payload.RunnerID, payload.RunnerIDCamel)),
	})
	writeServiceValue(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *serviceServer) recordRunnerMemoryAudit(ctx context.Context, identity serviceAuthIdentity, sessionKey, eventType string, payload map[string]any) {
	audit := s.serviceAudit()
	if s == nil || audit == nil {
		return
	}
	if payload == nil {
		payload = map[string]any{}
	}
	payload["session_key"] = sessionKey
	actor := strings.TrimSpace(identity.Actor)
	if actor == "" {
		actor = "service"
	}
	encoded, _ := json.Marshal(payload)
	_ = audit.Record(ctx, eventType, sessionKey, actor, string(encoded))
}

func firstNonZeroInt64(values ...int64) int64 {
	for _, v := range values {
		if v > 0 {
			return v
		}
	}
	return 0
}
