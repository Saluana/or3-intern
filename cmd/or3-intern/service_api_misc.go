package main

import (
	"errors"
	"io"
	"net/http"
	"strings"

	"or3-intern/internal/approval"
	"or3-intern/internal/controlplane"
)

func (s *serviceServer) handleEmbeddings(w http.ResponseWriter, r *http.Request) {
	if !requireServiceRole(w, r, approval.RoleOperator) {
		return
	}
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/internal/v1/embeddings"), "/")
	cp := s.control()
	switch path {
	case "status":
		if r.Method != http.MethodGet {
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		report, err := cp.GetEmbeddingStatus(r.Context())
		if err != nil {
			writeServiceJSON(w, statusCodeForControlplaneError(err, http.StatusBadGateway), map[string]any{"error": controlplane.DescribeUnavailable(err).Error()})
			return
		}
		writeServiceValue(w, http.StatusOK, report)
	case "rebuild":
		if r.Method != http.MethodPost {
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		limitServiceRequestBody(w, r, serviceEmbeddingsBodyLimit)
		var body struct {
			Target      string `json:"target"`
			TargetCamel string `json:"targetName"`
		}
		if err := decodeServiceRequestBody(r.Body, &body); err != nil && !errors.Is(err, io.EOF) {
			writeServiceRequestDecodeError(w, err)
			return
		}
		target := serviceFirstNonEmpty(body.Target, body.TargetCamel, r.URL.Query().Get("target"))
		result, err := cp.RebuildEmbeddings(r.Context(), target)
		if err != nil {
			status := statusCodeForControlplaneError(err, http.StatusBadRequest)
			writeServiceJSON(w, status, map[string]any{"error": controlplane.DescribeUnavailable(err).Error()})
			return
		}
		writeServiceValue(w, http.StatusOK, result)
	default:
		writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "embeddings route not found"})
	}
}

func (s *serviceServer) handleAudit(w http.ResponseWriter, r *http.Request) {
	if !requireServiceRole(w, r, approval.RoleOperator) {
		return
	}
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/internal/v1/audit"), "/")
	cp := s.control()
	switch path {
	case "":
		if r.Method != http.MethodGet {
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		report, err := cp.GetAuditStatus(r.Context())
		if err != nil {
			writeServiceJSON(w, statusCodeForControlplaneError(err, http.StatusBadGateway), map[string]any{"error": controlplane.DescribeUnavailable(err).Error()})
			return
		}
		writeServiceValue(w, http.StatusOK, report)
	case "verify":
		if r.Method != http.MethodPost {
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		result, err := cp.VerifyAudit(r.Context())
		if err != nil {
			writeServiceJSON(w, statusCodeForControlplaneError(err, http.StatusBadGateway), map[string]any{"error": controlplane.DescribeUnavailable(err).Error()})
			return
		}
		writeServiceValue(w, http.StatusOK, result)
	default:
		writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "audit route not found"})
	}
}

func (s *serviceServer) handleScope(w http.ResponseWriter, r *http.Request) {
	if !requireServiceRole(w, r, approval.RoleOperator) {
		return
	}
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/internal/v1/scope"), "/")
	cp := s.control()
	switch path {
	case "links":
		if r.Method != http.MethodPost {
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		limitServiceRequestBody(w, r, serviceScopeBodyLimit)
		var body struct {
			SessionKey      string         `json:"session_key"`
			SessionKeyCamel string         `json:"sessionKey"`
			ScopeKey        string         `json:"scope_key"`
			ScopeKeyCamel   string         `json:"scopeKey"`
			Meta            map[string]any `json:"meta"`
		}
		if err := decodeServiceRequestBody(r.Body, &body); err != nil {
			writeServiceRequestDecodeError(w, err)
			return
		}
		linked, err := cp.LinkSessionScope(r.Context(), controlplane.ScopeLinkInput{
			SessionKey: serviceFirstNonEmpty(body.SessionKey, body.SessionKeyCamel),
			ScopeKey:   serviceFirstNonEmpty(body.ScopeKey, body.ScopeKeyCamel),
			Meta:       body.Meta,
		})
		if err != nil {
			writeServiceJSON(w, statusCodeForControlplaneError(err, http.StatusBadRequest), map[string]any{"error": controlplane.DescribeUnavailable(err).Error()})
			return
		}
		writeServiceJSON(w, http.StatusAccepted, map[string]any{"session_key": linked.SessionKey, "scope_key": linked.ScopeKey})
	case "sessions":
		if r.Method != http.MethodGet {
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		scopeKey := serviceFirstNonEmpty(r.URL.Query().Get("scope_key"), r.URL.Query().Get("scopeKey"))
		if scopeKey == "" {
			writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": "scope_key is required"})
			return
		}
		sessions, err := cp.ListScopeSessions(r.Context(), scopeKey)
		if err != nil {
			writeServiceJSON(w, statusCodeForControlplaneError(err, http.StatusBadGateway), map[string]any{"error": controlplane.DescribeUnavailable(err).Error()})
			return
		}
		writeServiceJSON(w, http.StatusOK, map[string]any{"scope_key": scopeKey, "sessions": sessions})
	case "resolve":
		if r.Method != http.MethodGet {
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		sessionKey := serviceFirstNonEmpty(r.URL.Query().Get("session_key"), r.URL.Query().Get("sessionKey"))
		if sessionKey == "" {
			writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": "session_key is required"})
			return
		}
		scopeKey, err := cp.ResolveScopeKey(r.Context(), sessionKey)
		if err != nil {
			writeServiceJSON(w, statusCodeForControlplaneError(err, http.StatusBadGateway), map[string]any{"error": controlplane.DescribeUnavailable(err).Error()})
			return
		}
		writeServiceJSON(w, http.StatusOK, map[string]any{"session_key": sessionKey, "scope_key": scopeKey})
	default:
		writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "scope route not found"})
	}
}
