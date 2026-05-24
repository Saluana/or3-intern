package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"or3-intern/internal/adminflow"
	"or3-intern/internal/agentcli"
	"or3-intern/internal/db"
)

const doctorSessionMessagePageSize = 500

type doctorSessionTurnLease struct {
	kind string
	id   string
}

func (s *serviceServer) initDoctorTurnTracking() {
	s.doctorTurnOnce.Do(func() {
		s.doctorActiveTurns = map[string]doctorSessionTurnLease{}
	})
}

func (s *serviceServer) claimDoctorSessionTurn(sessionKey, kind, id string) (func(), error) {
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" {
		return nil, errors.New("session_key required")
	}
	s.initDoctorTurnTracking()
	s.doctorTurnMu.Lock()
	defer s.doctorTurnMu.Unlock()
	if existing, ok := s.doctorActiveTurns[sessionKey]; ok {
		return nil, fmt.Errorf("doctor turn already active (%s:%s)", existing.kind, existing.id)
	}
	s.doctorActiveTurns[sessionKey] = doctorSessionTurnLease{kind: kind, id: id}
	return func() {
		s.doctorTurnMu.Lock()
		delete(s.doctorActiveTurns, sessionKey)
		s.doctorTurnMu.Unlock()
	}, nil
}

func (s *serviceServer) listDoctorSessionMessages(ctx context.Context, sessionKey string) ([]db.ChatMessage, error) {
	store := s.doctorDB()
	if store == nil {
		return nil, errors.New("database unavailable")
	}
	all := make([]db.ChatMessage, 0, 32)
	afterID := int64(0)
	for {
		page, err := store.ListChatMessages(ctx, sessionKey, afterID, doctorSessionMessagePageSize)
		if err != nil {
			return nil, err
		}
		all = append(all, page.Messages...)
		if page.NextCursor <= 0 {
			break
		}
		afterID = page.NextCursor
	}
	sortDoctorSessionMessages(all)
	return all, nil
}

func doctorMessageSequenceFromRecord(message db.ChatMessage) int64 {
	var payload map[string]any
	if raw := strings.TrimSpace(message.PayloadJSON); raw != "" && raw != "{}" {
		_ = json.Unmarshal([]byte(raw), &payload)
	}
	if payload != nil {
		switch v := payload["doctor_seq"].(type) {
		case float64:
			if int64(v) > 0 {
				return int64(v)
			}
		case int64:
			if v > 0 {
				return v
			}
		}
	}
	if message.ID > 0 {
		return message.ID
	}
	return message.CreatedAt
}

func sortDoctorSessionMessages(messages []db.ChatMessage) {
	sort.SliceStable(messages, func(i, j int) bool {
		left := doctorMessageSequenceFromRecord(messages[i])
		right := doctorMessageSequenceFromRecord(messages[j])
		if left == right {
			return messages[i].ID < messages[j].ID
		}
		return left < right
	})
}

func writeDoctorSessionPayload(w http.ResponseWriter, status int, payload map[string]any) {
	if payload == nil {
		payload = map[string]any{}
	}
	writeServiceValue(w, status, payload)
}

func doctorSessionMessageResponse(messages []db.ChatMessage, adminBrain adminflow.AdminBrainProvider, transport string, extra map[string]any) map[string]any {
	payload := map[string]any{
		"messages":    doctorAPIChatMessages(messages),
		"admin_brain": adminBrain,
		"transport": transport,
	}
	for key, value := range extra {
		payload[key] = value
	}
	return payload
}

func (s *serviceServer) syncDoctorSessionRunnerMeta(ctx context.Context, meta db.ChatSessionMeta, runnerID, model string, adminBrain adminflow.AdminBrainProvider) (db.ChatSessionMeta, error) {
	runnerID = strings.TrimSpace(runnerID)
	model = strings.TrimSpace(model)
	if runnerID == "" {
		runnerID = strings.TrimSpace(meta.RunnerID)
	}
	if runnerID == "" {
		runnerID = strings.TrimSpace(adminBrain.RunnerID)
	}
	if adminBrain.Kind == adminflow.AdminBrainAPIKeyProvider && doctorUsesRunnerChat(runnerID) {
		runnerID = string(agentcli.RunnerOR3)
	}
	if runnerID == "" && adminBrain.Kind == adminflow.AdminBrainAPIKeyProvider {
		runnerID = string(agentcli.RunnerOR3)
	}
	if runnerID != "" {
		meta.RunnerID = runnerID
	}
	if model != "" {
		meta.RunnerModel = model
	}
	if strings.TrimSpace(meta.RunnerLabel) == "" {
		meta.RunnerLabel = serviceFirstNonEmpty(adminBrain.DisplayName, meta.RunnerID, "Admin Brain")
	}
	needsRunnerChat := doctorUsesRunnerChat(meta.RunnerID)
	if !needsRunnerChat {
		meta.RunnerChatSessionID = ""
		store := s.doctorDB()
		if store == nil {
			return meta, errors.New("database unavailable")
		}
		return store.UpsertChatSessionMeta(ctx, meta)
	}
	if s.chatManager == nil || s.chatManager.DB == nil || s.chatManager.Manager == nil {
		return meta, fmt.Errorf("runner chat unavailable")
	}
	runnerSession, err := s.chatManager.EnsureSession(ctx, agentcli.StartTurnRequest{
		AppSessionKey:    meta.SessionKey,
		RunnerID:         meta.RunnerID,
		ContinuationMode: agentcli.ContinuationReplay,
		Model:            meta.RunnerModel,
		Mode:             string(agentcli.RunnerModeReview),
		Isolation:        string(agentcli.IsolationHostReadOnly),
		MaxTurns:         4,
		TimeoutSeconds:   120,
	})
	if err != nil {
		return meta, err
	}
	meta.RunnerChatSessionID = runnerSession.ID
	meta.RunnerContinuationMode = runnerSession.ContinuationMode
	meta.RunnerModel = runnerSession.Model
	meta.RunnerMode = runnerSession.Mode
	meta.RunnerIsolation = runnerSession.Isolation
	meta.RunnerCwd = runnerSession.Cwd
	store := s.doctorDB()
	if store == nil {
		return meta, errors.New("database unavailable")
	}
	return store.UpsertChatSessionMeta(ctx, meta)
}
