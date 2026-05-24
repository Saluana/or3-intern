package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"

	"or3-intern/internal/approval"
	"or3-intern/internal/controlplane"
)

func (s *serviceServer) handleApprovals(w http.ResponseWriter, r *http.Request) {
	appSvc := s.app()
	if s.broker == nil {
		writeServiceJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "approval broker unavailable"})
		return
	}
	if !requireServiceRole(w, r, approval.RoleOperator) {
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/internal/v1/approvals")
	if path == "" || path == "/" {
		if r.Method != http.MethodGet {
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		items, err := appSvc.ListApprovalRequests(r.Context(), controlplane.ApprovalFilter{
			Status: r.URL.Query().Get("status"),
			Type:   r.URL.Query().Get("type"),
			Limit:  100,
		})
		if err != nil {
			writeServiceError(w, r, http.StatusBadGateway, "approval list unavailable", err)
			return
		}
		writeServiceJSON(w, http.StatusOK, map[string]any{"items": items})
		return
	}
	trimmedPath := strings.Trim(path, "/")
	if trimmedPath == "count" {
		if r.Method != http.MethodGet {
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		if s.broker == nil {
			writeServiceJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "approval broker unavailable"})
			return
		}
		count, err := s.broker.CountApprovalRequests(r.Context(), r.URL.Query().Get("status"), r.URL.Query().Get("type"))
		if err != nil {
			writeServiceError(w, r, http.StatusBadGateway, "approval count unavailable", err)
			return
		}
		writeServiceJSON(w, http.StatusOK, map[string]any{"count": count})
		return
	}
	if trimmedPath == "expire" {
		if r.Method != http.MethodPost {
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		expired, err := appSvc.ExpireApprovals(r.Context(), serviceAuthIdentityFromContext(r.Context()).Actor)
		if err != nil {
			writeServiceError(w, r, http.StatusBadGateway, "approval expiration failed", err)
			return
		}
		writeServiceJSON(w, http.StatusOK, map[string]any{"expired": expired})
		return
	}
	if trimmedPath == "allowlists" {
		switch r.Method {
		case http.MethodGet:
			items, err := appSvc.ListAllowlists(r.Context(), r.URL.Query().Get("domain"), 100)
			if err != nil {
				writeServiceError(w, r, http.StatusBadGateway, "allowlist list unavailable", err)
				return
			}
			writeServiceJSON(w, http.StatusOK, map[string]any{"items": items})
		case http.MethodPost:
			limitServiceRequestBody(w, r, serviceApprovalBodyLimit)
			body, err := decodeServiceAllowlistRequest(r.Body)
			if err != nil {
				writeServiceRequestDecodeError(w, err)
				return
			}
			rec, err := appSvc.AddAllowlist(r.Context(), body.Domain, body.Scope, body.Matcher, serviceAuthIdentityFromContext(r.Context()).Actor, body.ExpiresAt)
			if err != nil {
				writeServiceError(w, r, http.StatusBadRequest, "allowlist add failed", err)
				return
			}
			writeServiceJSON(w, http.StatusAccepted, map[string]any{"item": rec})
		default:
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		}
		return
	}
	if strings.HasPrefix(trimmedPath, "allowlists/") {
		parts := strings.Split(trimmedPath, "/")
		if len(parts) != 3 || parts[2] != "remove" {
			writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "approval route not found"})
			return
		}
		if r.Method != http.MethodPost {
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		id, err := parseServiceInt64(parts[1])
		if err != nil {
			writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid allowlist ID"})
			return
		}
		if err := appSvc.RemoveAllowlist(r.Context(), id, serviceAuthIdentityFromContext(r.Context()).Actor); err != nil {
			status := http.StatusBadRequest
			if strings.Contains(strings.ToLower(err.Error()), "not found") {
				status = http.StatusNotFound
			}
			writeServiceError(w, r, status, "allowlist remove failed", err)
			return
		}
		writeServiceJSON(w, http.StatusOK, map[string]any{"allowlist_id": id, "status": "removed"})
		return
	}
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) == 1 {
		if r.Method != http.MethodGet {
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		id, err := parseServiceInt64(parts[0])
		if err != nil {
			writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid approval ID"})
			return
		}
		item, err := appSvc.GetApproval(r.Context(), id)
		if err != nil {
			writeServiceError(w, r, http.StatusBadRequest, "approval lookup failed", err)
			return
		}
		writeServiceJSON(w, http.StatusOK, map[string]any{"item": item})
		return
	}
	if len(parts) != 2 {
		writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "approval route not found"})
		return
	}
	id, err := parseServiceInt64(parts[0])
	if err != nil {
		writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid approval ID"})
		return
	}
	switch parts[1] {
	case "approve":
		if r.Method != http.MethodPost {
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		limitServiceRequestBody(w, r, serviceApprovalBodyLimit)
		var body struct {
			Allowlist bool   `json:"allowlist"`
			Note      string `json:"note"`
		}
		if err := decodeServiceRequestBody(r.Body, &body); err != nil {
			writeServiceRequestDecodeError(w, err)
			return
		}
		issued, err := appSvc.ApproveApproval(r.Context(), id, serviceAuthIdentityFromContext(r.Context()).Actor, body.Allowlist, body.Note)
		if err != nil {
			s.writeServiceApprovalActionError(w, r, http.StatusBadRequest, id, "approve", "approval failed", err)
			return
		}
		log.Printf("service_approval: approved approval=%d session=%s allowlist=%t", id, strings.TrimSpace(issued.Request.RequesterSessionID), body.Allowlist)
		response := map[string]any{"request_id": id, "token": issued.Token, "allowlist_id": issued.AllowlistID}
		if sessionKey := strings.TrimSpace(issued.Request.RequesterSessionID); sessionKey != "" {
			response["session_key"] = sessionKey
		}
		warnings := make([]map[string]any, 0, 2)
		if s.broker != nil && s.broker.DB != nil {
			plans, err := approvalSkillRunPlanLookup(r.Context(), s.broker.DB, id, 20)
			if err != nil {
				warnings = append(warnings, map[string]any{
					"code":    "plan_lookup_failed",
					"message": approvalPlanLookupWarning(err),
				})
			} else {
				if len(plans) == 1 {
					response["plan_id"] = plans[0].ID
				}
				if len(plans) > 0 {
					ids := make([]string, 0, len(plans))
					for _, plan := range plans {
						if strings.TrimSpace(plan.ID) == "" {
							continue
						}
						ids = append(ids, strings.TrimSpace(plan.ID))
					}
					response["plan_ids"] = ids
				}
			}
		}
		resumeJobID, err := s.startApprovedResumeJob(r.Context(), issued, serviceAuthIdentityFromContext(r.Context()))
		if err != nil {
			warnings = append(warnings, map[string]any{
				"code":    "resume_start_failed",
				"message": approvalResumeWarning(err),
			})
		} else if strings.TrimSpace(resumeJobID) != "" {
			response["resume_job_id"] = resumeJobID
		}
		if len(warnings) > 0 {
			response["warnings"] = warnings
		}
		writeServiceJSON(w, http.StatusOK, response)
	case "deny":
		if r.Method != http.MethodPost {
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		limitServiceRequestBody(w, r, serviceApprovalBodyLimit)
		var body struct {
			Note string `json:"note"`
		}
		if err := decodeServiceRequestBody(r.Body, &body); err != nil {
			writeServiceRequestDecodeError(w, err)
			return
		}
		if err := appSvc.DenyApproval(r.Context(), id, serviceAuthIdentityFromContext(r.Context()).Actor, body.Note); err != nil {
			s.writeServiceApprovalActionError(w, r, http.StatusBadRequest, id, "deny", "approval denial failed", err)
			return
		}
		log.Printf("service_approval: denied approval=%d", id)
		writeServiceJSON(w, http.StatusOK, map[string]any{"request_id": id, "status": "denied"})
	case "cancel":
		if r.Method != http.MethodPost {
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		limitServiceRequestBody(w, r, serviceApprovalBodyLimit)
		var body struct {
			Note string `json:"note"`
		}
		if err := decodeServiceRequestBody(r.Body, &body); err != nil {
			writeServiceRequestDecodeError(w, err)
			return
		}
		if err := appSvc.CancelApproval(r.Context(), id, serviceAuthIdentityFromContext(r.Context()).Actor, body.Note); err != nil {
			s.writeServiceApprovalActionError(w, r, http.StatusBadRequest, id, "cancel", "approval cancel failed", err)
			return
		}
		writeServiceJSON(w, http.StatusOK, map[string]any{"request_id": id, "status": "canceled"})
	default:
		writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "approval action not found"})
	}
}

func parseServiceInt64(raw string) (int64, error) {
	return strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
}

func statusCodeForControlplaneError(err error, defaultCode int) int {
	switch {
	case errors.Is(err, controlplane.ErrDatabaseUnavailable), errors.Is(err, controlplane.ErrProviderUnavailable), errors.Is(err, controlplane.ErrAuditUnavailable), errors.Is(err, controlplane.ErrJobRegistryUnavailable):
		return http.StatusServiceUnavailable
	default:
		return defaultCode
	}
}

type serviceAllowlistRequest struct {
	Domain    string
	Scope     approval.AllowlistScope
	Matcher   any
	ExpiresAt int64
}

func decodeServiceAllowlistRequest(body io.Reader) (serviceAllowlistRequest, error) {
	var payload struct {
		Domain         string                  `json:"domain"`
		Scope          approval.AllowlistScope `json:"scope"`
		Matcher        json.RawMessage         `json:"matcher"`
		ExpiresAt      int64                   `json:"expires_at"`
		ExpiresAtCamel int64                   `json:"expiresAt"`
	}
	if err := decodeServiceRequestBody(body, &payload); err != nil {
		return serviceAllowlistRequest{}, err
	}
	domain := strings.TrimSpace(payload.Domain)
	var matcher any
	switch domain {
	case string(approval.SubjectExec):
		var item approval.ExecAllowlistMatcher
		if len(payload.Matcher) > 0 {
			if err := json.Unmarshal(payload.Matcher, &item); err != nil {
				return serviceAllowlistRequest{}, fmt.Errorf("invalid request body")
			}
		}
		matcher = item
	case string(approval.SubjectSkillExec):
		var item approval.SkillAllowlistMatcher
		if len(payload.Matcher) > 0 {
			if err := json.Unmarshal(payload.Matcher, &item); err != nil {
				return serviceAllowlistRequest{}, fmt.Errorf("invalid request body")
			}
		}
		matcher = item
	case string(approval.SubjectRunnerPermission):
		var item approval.RunnerPermissionAllowlistMatcher
		if len(payload.Matcher) > 0 {
			if err := json.Unmarshal(payload.Matcher, &item); err != nil {
				return serviceAllowlistRequest{}, fmt.Errorf("invalid request body")
			}
		}
		matcher = item
	default:
		return serviceAllowlistRequest{}, fmt.Errorf("unsupported allowlist domain")
	}
	return serviceAllowlistRequest{
		Domain:    domain,
		Scope:     payload.Scope,
		Matcher:   matcher,
		ExpiresAt: firstPositiveInt64(payload.ExpiresAt, payload.ExpiresAtCamel),
	}, nil
}

func firstPositiveInt64(values ...int64) int64 {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}

func serviceFirstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func serviceApprovalTokenFromRequest(r *http.Request) string {
	if r == nil {
		return ""
	}
	return serviceFirstNonEmpty(
		r.Header.Get("X-Approval-Token"),
		r.Header.Get("X-Or3-Approval-Token"),
	)
}
