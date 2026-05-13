package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"time"

	"or3-intern/internal/agent"
	"or3-intern/internal/approval"
	"or3-intern/internal/auth"
	"or3-intern/internal/db"
	"or3-intern/internal/tools"
)

type serviceRequestContextKey struct{}

type serviceRequestContext struct {
	RequestID string
}

func serviceBoundaryMiddleware(server *serviceServer, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := strings.TrimSpace(r.Header.Get("X-Request-Id"))
		if requestID == "" {
			requestID = newServiceRequestID()
		}
		w.Header().Set("X-Request-Id", requestID)
		ctx := context.WithValue(r.Context(), serviceRequestContextKey{}, serviceRequestContext{RequestID: requestID})
		r = r.WithContext(ctx)
		if server != nil && server.isMutationRequest(r) && !server.allowMutationRequest(r) {
			writeServiceJSON(w, http.StatusTooManyRequests, serviceErrorPayload(r, "rate limit exceeded"))
			return
		}
		captured := &serviceStatusRecorder{ResponseWriter: w, statusCode: http.StatusOK, requestID: requestID}
		next.ServeHTTP(captured, r)
		log.Printf("service %s %s -> %d", r.Method, r.URL.Path, captured.statusCode)
		server.recordServiceAudit(r, captured.statusCode)
	})
}

type serviceStatusRecorder struct {
	http.ResponseWriter
	statusCode int
	requestID  string
}

func (r *serviceStatusRecorder) ServiceRequestID() string {
	if r == nil {
		return ""
	}
	return strings.TrimSpace(r.requestID)
}

func (r *serviceStatusRecorder) WriteHeader(statusCode int) {
	r.statusCode = statusCode
	r.ResponseWriter.WriteHeader(statusCode)
}

func (r *serviceStatusRecorder) Flush() {
	if flusher, ok := r.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (r *serviceStatusRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker, ok := r.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, fmt.Errorf("response writer does not implement http.Hijacker")
	}
	if r.statusCode == http.StatusOK {
		r.statusCode = http.StatusSwitchingProtocols
	}
	return hijacker.Hijack()
}

func (s *serviceServer) isMutationRequest(r *http.Request) bool {
	if r == nil {
		return false
	}
	if s.isTerminalInteractiveMutation(r) {
		return false
	}
	switch r.Method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

func (s *serviceServer) isTerminalInteractiveMutation(r *http.Request) bool {
	if r == nil || r.Method != http.MethodPost {
		return false
	}
	path := strings.TrimSpace(r.URL.Path)
	if !strings.HasPrefix(path, "/internal/v1/terminal/sessions/") {
		return false
	}
	return strings.HasSuffix(path, "/input") || strings.HasSuffix(path, "/resize")
}

func (s *serviceServer) allowMutationRequest(r *http.Request) bool {
	if s == nil || r == nil {
		return true
	}
	limit := s.config.Service.MutationRateLimitPerMinute
	if limit <= 0 {
		return true
	}
	return s.serviceRateLimiter().Allow(r, limit)
}

func (s *serviceServer) serviceRateLimiter() *serviceRateLimiter {
	s.components()
	return s.rateLimiter
}

type serviceRateLimiter struct {
	mu     sync.Mutex
	window time.Time
	counts map[string]int
}

func (l *serviceRateLimiter) Allow(r *http.Request, limit int) bool {
	if l == nil || r == nil || limit <= 0 {
		return true
	}
	actor := serviceAuthIdentityFromContext(r.Context()).Actor
	if actor == "" {
		actor = remoteIPKey(r.RemoteAddr)
	}
	now := time.Now().UTC().Truncate(time.Minute)
	key := actor + ":" + r.URL.Path
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.counts == nil || !l.window.Equal(now) {
		l.window = now
		l.counts = map[string]int{}
	}
	l.counts[key]++
	return l.counts[key] <= limit
}

func (s *serviceServer) recordServiceAudit(r *http.Request, statusCode int) {
	if s == nil || s.runtime == nil || s.runtime.Audit == nil || r == nil {
		return
	}
	identity := serviceAuthIdentityFromContext(r.Context())
	payload := map[string]any{
		"path":        r.URL.Path,
		"method":      r.Method,
		"status_code": statusCode,
		"request_id":  serviceRequestIDFromContext(r.Context()),
	}
	if remote := remoteIPKey(r.RemoteAddr); remote != "" {
		payload["remote_addr"] = remote
	}
	_ = s.runtime.Audit.Record(r.Context(), "service.request", "", identity.Actor, payload)
}

func serviceRequestIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	requestCtx, _ := ctx.Value(serviceRequestContextKey{}).(serviceRequestContext)
	return strings.TrimSpace(requestCtx.RequestID)
}

func writeServiceError(w http.ResponseWriter, r *http.Request, statusCode int, public string, err error) {
	if err != nil {
		log.Printf("service %s %s: %v", r.Method, r.URL.Path, err)
	}
	payload := serviceErrorPayload(r, public)
	if code := agent.PublicErrorCode(err); code != "" {
		payload["code"] = code
	}
	writeServiceJSON(w, statusCode, payload)
}

const (
	serviceCodeValidationFailed      = "validation_failed"
	serviceCodeMethodNotAllowed      = "method_not_allowed"
	serviceCodeNotFound              = "not_found"
	serviceCodeForbidden             = "forbidden"
	serviceCodeUnauthorized          = "unauthorized"
	serviceCodeRateLimited           = "rate_limited"
	serviceCodeCapabilityUnavailable = "capability_unavailable"
	serviceCodeRequestTooLarge       = "request_too_large"
	serviceCodeConflict              = "conflict"
	serviceCodeTimeout               = "timeout"
	serviceCodeRequestFailed         = "request_failed"
)

func servicePublicPairingExchangeError(err error) (string, bool) {
	if err == nil {
		return "", false
	}
	message := strings.TrimSpace(err.Error())
	switch message {
	case "pairing request not found", "pairing request expired", "pairing request is not approved":
		return message, true
	default:
		return "", false
	}
}

func approvalActionPastTense(action string) string {
	switch strings.TrimSpace(action) {
	case "approve":
		return "approved"
	case "deny":
		return "denied"
	case "cancel":
		return "canceled"
	default:
		return "updated"
	}
}

func approvalActionExpiredMessage(action string) string {
	return fmt.Sprintf("This approval request expired before it could be %s. Refresh the approvals list and rerun the request if it is still needed.", approvalActionPastTense(action))
}

func approvalActionResolvedMessage(status string) string {
	switch strings.TrimSpace(status) {
	case approval.StatusApproved:
		return "This approval request was already approved. Refresh the approvals list to see its latest status."
	case approval.StatusDenied:
		return "This approval request was already denied. Refresh the approvals list to see its latest status."
	case approval.StatusCanceled:
		return "This approval request was already canceled. Refresh the approvals list to see its latest status."
	case approval.StatusExpired:
		return "This approval request already expired. Refresh the approvals list and rerun the request if it is still needed."
	default:
		return fmt.Sprintf("This approval request is %s and can no longer be changed. Refresh the approvals list to see its latest status.", strings.TrimSpace(status))
	}
}

func (s *serviceServer) servicePublicApprovalActionError(ctx context.Context, requestID int64, action string, err error) (string, string, bool) {
	if err == nil {
		return "", "", false
	}
	message := strings.TrimSpace(err.Error())
	if message == "" {
		return "", "", false
	}
	switch message {
	case "approval request expired":
		return approvalActionExpiredMessage(action), approval.StatusExpired, true
	case "approval request not found":
		return "This approval request no longer exists.", "", true
	case "approval request is not pending":
		status := ""
		if s != nil && s.broker != nil && s.broker.DB != nil {
			rec, lookupErr := s.broker.DB.GetApprovalRequest(ctx, requestID)
			if lookupErr == nil {
				status = strings.TrimSpace(rec.Status)
			}
		}
		switch status {
		case approval.StatusExpired:
			return approvalActionExpiredMessage(action), status, true
		case approval.StatusApproved, approval.StatusDenied, approval.StatusCanceled:
			return approvalActionResolvedMessage(status), status, true
		case "":
			return "This approval request is no longer waiting for action. Refresh the approvals list to see its latest status.", "", true
		default:
			return approvalActionResolvedMessage(status), status, true
		}
	default:
		return "", "", false
	}
}

func (s *serviceServer) writeServiceApprovalActionError(w http.ResponseWriter, r *http.Request, statusCode int, approvalID int64, action, fallback string, err error) {
	if err != nil {
		log.Printf("service %s %s: %v", r.Method, r.URL.Path, err)
	}
	public := strings.TrimSpace(fallback)
	approvalStatus := ""
	if mapped, status, ok := s.servicePublicApprovalActionError(r.Context(), approvalID, action, err); ok {
		public = mapped
		approvalStatus = status
	}
	payload := serviceErrorPayload(r, public)
	payload["approval_id"] = approvalID
	if strings.TrimSpace(approvalStatus) != "" {
		payload["approval_status"] = approvalStatus
	}
	writeServiceJSON(w, statusCode, payload)
}

func serviceErrorPayload(r *http.Request, public string) map[string]any {
	payload := map[string]any{"error": strings.TrimSpace(public)}
	if payload["error"] == "" {
		payload["error"] = "request failed"
	}
	payload["code"] = serviceErrorCodeForMessage(fmt.Sprint(payload["error"]), http.StatusInternalServerError)
	if guidance := serviceRecoveryGuidance(fmt.Sprint(payload["error"])); guidance != "" {
		payload["recovery"] = guidance
	}
	if r != nil {
		if requestID := serviceRequestIDFromContext(r.Context()); requestID != "" {
			payload["request_id"] = requestID
		}
	}
	return payload
}

func serviceErrorCodeForMessage(message string, statusCode int) string {
	text := strings.ToLower(strings.TrimSpace(message))
	switch {
	case strings.Contains(text, "method not allowed"):
		return serviceCodeMethodNotAllowed
	case strings.Contains(text, "not found") || strings.Contains(text, "route not found"):
		return serviceCodeNotFound
	case strings.Contains(text, "unauthorized"):
		return serviceCodeUnauthorized
	case strings.Contains(text, "forbidden"):
		return serviceCodeForbidden
	case strings.Contains(text, "rate limit") || strings.Contains(text, "too many"):
		return serviceCodeRateLimited
	case strings.Contains(text, "too large"):
		return serviceCodeRequestTooLarge
	case strings.Contains(text, "required") || strings.Contains(text, "invalid") || strings.Contains(text, "unsupported") || strings.Contains(text, "must "):
		return serviceCodeValidationFailed
	case strings.Contains(text, "unavailable") || strings.Contains(text, "disabled"):
		return serviceCodeCapabilityUnavailable
	case strings.Contains(text, "timed out") || strings.Contains(text, "timeout"):
		return serviceCodeTimeout
	case strings.Contains(text, "conflict") || strings.Contains(text, "already"):
		return serviceCodeConflict
	}
	switch statusCode {
	case http.StatusBadRequest:
		return serviceCodeValidationFailed
	case http.StatusUnauthorized:
		return serviceCodeUnauthorized
	case http.StatusForbidden:
		return serviceCodeForbidden
	case http.StatusNotFound:
		return serviceCodeNotFound
	case http.StatusMethodNotAllowed:
		return serviceCodeMethodNotAllowed
	case http.StatusConflict:
		return serviceCodeConflict
	case http.StatusRequestEntityTooLarge:
		return serviceCodeRequestTooLarge
	case http.StatusTooManyRequests:
		return serviceCodeRateLimited
	case http.StatusGatewayTimeout:
		return serviceCodeTimeout
	case http.StatusServiceUnavailable:
		return serviceCodeCapabilityUnavailable
	default:
		return serviceCodeRequestFailed
	}
}

type serviceRequestIDProvider interface {
	ServiceRequestID() string
}

func normalizeServiceErrorPayload(statusCode int, payload map[string]any, requestID string) map[string]any {
	if payload == nil {
		return payload
	}
	errorValue, hasError := payload["error"]
	if !hasError {
		return payload
	}
	code, hasCode := payload["code"]
	if !hasCode || strings.TrimSpace(fmt.Sprint(code)) == "" {
		payload["code"] = serviceErrorCodeForMessage(fmt.Sprint(errorValue), statusCode)
	}
	if _, hasRequestID := payload["request_id"]; !hasRequestID && strings.TrimSpace(requestID) != "" {
		payload["request_id"] = strings.TrimSpace(requestID)
	}
	return payload
}

func addServiceRequestID(payload map[string]any, r *http.Request) map[string]any {
	if payload == nil || r == nil {
		return payload
	}
	if _, ok := payload["request_id"]; ok {
		return payload
	}
	if requestID := serviceRequestIDFromContext(r.Context()); requestID != "" {
		payload["request_id"] = requestID
	}
	return payload
}

func serviceRecoveryGuidance(message string) string {
	text := strings.ToLower(strings.TrimSpace(message))
	switch {
	case strings.Contains(text, "provider"), strings.Contains(text, "model"), strings.Contains(text, "api key"), strings.Contains(text, "endpoint"):
		return "Check provider settings with doctor, then retry when the model endpoint is reachable."
	case strings.Contains(text, "tool"):
		return "Check tool configuration and approvals with doctor, then retry the request."
	case strings.Contains(text, "runner") && (strings.Contains(text, "auth") || strings.Contains(text, "authenticated")):
		return "Authenticate the configured runner CLI or choose a different runner."
	case strings.Contains(text, "database"), strings.Contains(text, "sqlite"), strings.Contains(text, "artifact"):
		return "Check local storage paths and permissions, then run doctor repair if needed."
	default:
		return ""
	}
}

func writeServiceAuthError(w http.ResponseWriter, r *http.Request, err error) {
	if err != nil {
		log.Printf("service %s %s auth: %v", r.Method, r.URL.Path, err)
	}
	var authErr *auth.Error
	if errors.As(err, &authErr) {
		payload := serviceErrorPayload(r, authErr.Message)
		payload["code"] = authErr.Code
		if authErr.RetryAfter > 0 {
			payload["retry_after_seconds"] = authErr.RetryAfter
		}
		writeServiceJSON(w, authErr.Status, payload)
		return
	}
	status := http.StatusBadRequest
	message := "auth request failed"
	switch {
	case errors.Is(err, auth.ErrInvalidCeremony):
		status = http.StatusBadRequest
		message = "invalid or expired auth challenge"
	case errors.Is(err, auth.ErrRecoveryRequired):
		status = http.StatusConflict
		message = err.Error()
	}
	payload := serviceErrorPayload(r, message)
	payload["code"] = serviceErrorCodeForMessage(message, status)
	writeServiceJSON(w, status, payload)
}

func serviceAuthSessionToken(r *http.Request) string {
	if r == nil {
		return ""
	}
	return serviceFirstNonEmpty(
		strings.TrimSpace(r.Header.Get("X-Or3-Session")),
		strings.TrimSpace(r.Header.Get("X-Auth-Session")),
		strings.TrimSpace(r.URL.Query().Get("session_token")),
	)
}

func servicePublicJobError(err error) string {
	if err == nil {
		return "job failed"
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return "job canceled"
	}
	return "job failed"
}

func serviceApprovalRequiredPayload(sessionKey string, meta map[string]any, err *tools.ApprovalRequiredError) map[string]any {
	message := "approval is required before this tool can continue"
	var requestID int64
	if err != nil {
		if trimmed := strings.TrimSpace(err.Error()); trimmed != "" {
			message = trimmed
		}
		requestID = err.RequestID
	}
	payload := serviceLifecyclePayload(sessionKey, meta, map[string]any{
		"status":  "approval_required",
		"code":    "approval_required",
		"message": message,
	})
	if requestID > 0 {
		payload["request_id"] = requestID
		payload["approval_id"] = requestID
	}
	return payload
}

func serviceTurnFallbackText(err error, observer *serviceObserver) (string, bool) {
	if err == nil || !strings.Contains(err.Error(), "max tool loops exceeded") {
		return "", false
	}
	message := "I couldn't finish that because the tool calls kept failing or looping."
	if observer != nil {
		lastToolError := strings.TrimSpace(observer.lastToolError)
		if lastToolError != "" {
			if len(lastToolError) > 180 {
				lastToolError = lastToolError[:180] + "..."
			}
			message += " Last tool error: " + lastToolError
		}
	}
	return message, true
}

func remoteIPKey(raw string) string {
	addr := strings.TrimSpace(raw)
	if addr == "" {
		return ""
	}
	host := addr
	if parsed, err := netip.ParseAddrPort(addr); err == nil {
		return parsed.Addr().String()
	}
	if hostPart, _, err := net.SplitHostPort(addr); err == nil {
		host = hostPart
	}
	return strings.Trim(host, "[]")
}

func newServiceRequestID() string {
	return strconv.FormatInt(time.Now().UnixNano(), 36)
}

func validateServiceToolCapabilities(registry *tools.Registry, names []string, maxCapability string) error {
	ceiling := tools.CapabilityLevel(strings.ToLower(strings.TrimSpace(maxCapability)))
	if ceiling == "" || registry == nil || len(names) == 0 {
		return nil
	}
	for _, name := range names {
		toolName := strings.TrimSpace(name)
		if toolName == "" {
			continue
		}
		tool := registry.Get(toolName)
		if tool == nil {
			continue
		}
		if capabilityRank(tools.ToolCapability(tool, nil)) > capabilityRank(ceiling) {
			return fmt.Errorf("tool exceeds service capability ceiling: %s", toolName)
		}
	}
	return nil
}

func capabilityRank(level tools.CapabilityLevel) int {
	switch level {
	case tools.CapabilityPrivileged:
		return 3
	case tools.CapabilityGuarded:
		return 2
	case tools.CapabilitySafe:
		return 1
	default:
		return 0
	}
}

func beginSSE(w http.ResponseWriter) error {
	flusher, ok := w.(http.Flusher)
	if !ok {
		return fmt.Errorf("streaming is not supported")
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Connection", "keep-alive")
	flusher.Flush()
	return nil
}

func writeSSEEvent(w http.ResponseWriter, eventType string, payload map[string]any) error {
	flusher, ok := w.(http.Flusher)
	if !ok {
		return fmt.Errorf("streaming is not supported")
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", eventType, encoded); err != nil {
		return err
	}
	flusher.Flush()
	return nil
}

func writeServiceJSON(w http.ResponseWriter, statusCode int, payload map[string]any) {
	if statusCode >= 400 {
		requestID := ""
		if provider, ok := w.(serviceRequestIDProvider); ok {
			requestID = provider.ServiceRequestID()
		}
		if requestID == "" && payload != nil {
			if existing, ok := payload["request_id"]; ok {
				requestID = strings.TrimSpace(fmt.Sprint(existing))
			}
		}
		if requestID == "" && payload != nil {
			if _, hasError := payload["error"]; hasError {
				requestID = newServiceRequestID()
				w.Header().Set("X-Request-Id", requestID)
			}
		} else if requestID != "" && strings.TrimSpace(w.Header().Get("X-Request-Id")) == "" {
			w.Header().Set("X-Request-Id", requestID)
		}
		payload = normalizeServiceErrorPayload(statusCode, payload, requestID)
	}
	writeServiceValue(w, statusCode, payload)
}

func writeServiceValue(w http.ResponseWriter, statusCode int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(payload)
}

func acceptsSSE(r *http.Request) bool {
	return strings.Contains(strings.ToLower(r.Header.Get("Accept")), "text/event-stream")
}

func cloneServiceMeta(meta map[string]any) map[string]any {
	if len(meta) == 0 {
		return map[string]any{}
	}
	out := make(map[string]any, len(meta))
	for key, value := range meta {
		out[key] = value
	}
	return out
}

type serviceAuditHeaders struct {
	RequestID        string
	WorkspaceID      string
	NetworkSessionID string
}

func serviceAuditHeadersFromRequest(r *http.Request) serviceAuditHeaders {
	return serviceAuditHeaders{
		RequestID:        strings.TrimSpace(r.Header.Get("X-Request-Id")),
		WorkspaceID:      strings.TrimSpace(r.Header.Get("X-Workspace-Id")),
		NetworkSessionID: strings.TrimSpace(r.Header.Get("X-Network-Session-Id")),
	}
}

func mergeServiceAuditMeta(meta map[string]any, audit serviceAuditHeaders) map[string]any {
	out := cloneServiceMeta(meta)
	if audit.RequestID != "" {
		out["request_id"] = audit.RequestID
	}
	if audit.WorkspaceID != "" {
		out["workspace_id"] = audit.WorkspaceID
	}
	if audit.NetworkSessionID != "" {
		out["network_session_id"] = audit.NetworkSessionID
	}
	return out
}

func serviceLifecyclePayload(sessionKey string, meta map[string]any, extra map[string]any) map[string]any {
	payload := map[string]any{"session_key": sessionKey}
	for _, key := range []string{"request_id", "workspace_id", "network_session_id"} {
		if value, ok := meta[key]; ok {
			payload[key] = value
		}
	}
	for key, value := range extra {
		payload[key] = value
	}
	return payload
}

func isTerminalStatus(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "completed", "failed", "aborted", db.SubagentStatusSucceeded, db.SubagentStatusInterrupted:
		return true
	default:
		return false
	}
}

func (s *serviceServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		return
	}
	writeServiceValue(w, http.StatusOK, s.control().GetHealth())
}

func (s *serviceServer) handleReadiness(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		return
	}
	report := s.control().GetReadiness()
	statusCode := http.StatusOK
	if !report.Ready {
		statusCode = http.StatusServiceUnavailable
	}
	writeServiceValue(w, statusCode, report)
}

func (s *serviceServer) handleCapabilities(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		return
	}
	writeServiceValue(w, http.StatusOK, s.control().GetCapabilities(r.URL.Query().Get("channel"), r.URL.Query().Get("trigger")))
}
