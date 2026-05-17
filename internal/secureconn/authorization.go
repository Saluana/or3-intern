package secureconn

import (
	"fmt"
	"net/http"
	"strings"
	"time"
)

const (
	ActionView              = "view"
	ActionMutate            = "mutate"
	ActionTerminal          = "terminal"
	ActionTool              = "tool"
	ActionSecrets           = "secrets"
	ActionSecurityConfig    = "security_config"
	ActionProfileEscalation = "profile_escalation"
	ActionDeviceManagement  = "device_management"

	CapabilityChat     = "chat"
	CapabilityFiles    = "files"
	CapabilityTerminal = "terminal"
	CapabilityTools    = "tools"
	CapabilitySecrets  = "secrets"
	CapabilityAdmin    = "admin"
	CapabilityDevices  = "devices"
)

type ActionRequest struct {
	Class      string `json:"class"`
	Capability string `json:"capability,omitempty"`
	Method     string `json:"method,omitempty"`
	Path       string `json:"path,omitempty"`
	Tool       string `json:"tool,omitempty"`
}

type AuthorizationDecision struct {
	Allowed      bool              `json:"allowed"`
	Code         string            `json:"code,omitempty"`
	SafeMessage  string            `json:"safe_message,omitempty"`
	RequiresStep bool              `json:"requires_step_up,omitempty"`
	AuditPayload map[string]any    `json:"audit_payload"`
	Claims       SecureAuditClaims `json:"claims"`
}

type SecureAuditClaims struct {
	HostID          string `json:"host_id"`
	DeviceID        string `json:"device_id"`
	SessionID       string `json:"session_id"`
	RelayRouteID    string `json:"relay_route_id,omitempty"`
	Role            string `json:"role"`
	TrustLevel      string `json:"trust_level"`
	EnrollmentEpoch int64  `json:"enrollment_epoch"`
}

func ClassifyAction(method, path, tool string) ActionRequest {
	method = strings.ToUpper(strings.TrimSpace(method))
	path = strings.ToLower(strings.TrimSpace(path))
	tool = strings.ToLower(strings.TrimSpace(tool))
	class := ActionView
	capability := CapabilityChat
	switch {
	case strings.Contains(path, "/terminal") || strings.Contains(tool, "terminal") || strings.Contains(tool, "shell") || strings.Contains(tool, "exec"):
		class = ActionTerminal
		capability = CapabilityTerminal
	case strings.Contains(path, "/secrets") || strings.Contains(tool, "secret"):
		class = ActionSecrets
		capability = CapabilitySecrets
	case strings.Contains(path, "/secure-connections") || strings.Contains(path, "/devices"):
		class = ActionDeviceManagement
		capability = CapabilityDevices
	case strings.Contains(path, "/profiles") || strings.Contains(path, "/approvals") || strings.Contains(path, "/auth/policy"):
		class = ActionSecurityConfig
		capability = CapabilityAdmin
	case strings.Contains(tool, "write") || strings.Contains(tool, "delete") || strings.Contains(tool, "patch") || strings.Contains(tool, "apply"):
		class = ActionTool
		capability = CapabilityTools
	case method != "" && method != http.MethodGet && method != http.MethodHead && method != http.MethodOptions:
		class = ActionMutate
		capability = CapabilityFiles
	}
	return ActionRequest{Class: class, Capability: capability, Method: method, Path: path, Tool: tool}
}

func AuthorizeAction(claims SessionClaims, action ActionRequest, now time.Time) AuthorizationDecision {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	decision := AuthorizationDecision{
		Claims: SecureAuditClaims{
			HostID:          claims.HostID,
			DeviceID:        claims.DeviceID,
			SessionID:       claims.SessionID,
			RelayRouteID:    claims.RelayRouteID,
			Role:            claims.Role,
			TrustLevel:      claims.TrustLevel,
			EnrollmentEpoch: claims.EnrollmentEpoch,
		},
	}
	decision.AuditPayload = map[string]any{
		"session_id": claims.SessionID,
		"device_id":  claims.DeviceID,
		"route_id":   claims.RelayRouteID,
		"role":       claims.Role,
		"trust":      claims.TrustLevel,
		"class":      action.Class,
		"capability": action.Capability,
	}
	if claims.ExpiresAtUnixMs > 0 && claims.ExpiresAtUnixMs <= now.UnixMilli() {
		return decision.deny(ErrorSessionExpired, "This secure connection expired.", false)
	}
	role := NormalizeRole(claims.Role)
	if role == "" {
		return decision.deny(ErrorCapabilityDenied, "This device role is not valid.", false)
	}
	if role == RoleViewer && action.Class != ActionView {
		return decision.deny(ErrorCapabilityDenied, "This device can view only.", false)
	}
	if role == RoleOperator && (action.Class == ActionSecurityConfig || action.Class == ActionProfileEscalation || action.Class == ActionDeviceManagement) {
		return decision.deny(ErrorCapabilityDenied, "This action requires an admin device.", false)
	}
	if action.Capability != "" && !hasCapability(claims.Capabilities, action.Capability) && role != RoleAdmin {
		return decision.deny(ErrorCapabilityDenied, "This device is not allowed to perform that action.", false)
	}
	if (requiresStepUp(action) || requiresWebStepUp(claims, action)) && !hasRecentStepUp(claims.StepUpAtUnixMs, now, stepUpTTLForTrust(claims.TrustLevel)) {
		return decision.deny(ErrorStepUpRequired, "Verify with a passkey or device credential before continuing.", true)
	}
	decision.Allowed = true
	decision.AuditPayload["outcome"] = "allowed"
	return decision
}

func requiresWebStepUp(claims SessionClaims, action ActionRequest) bool {
	return claims.TrustLevel == TrustWebLimited && action.Class != ActionView
}

func stepUpTTLForTrust(trustLevel string) time.Duration {
	if trustLevel == TrustWebLimited {
		return 2 * time.Minute
	}
	return DefaultStepUpTTL
}

func (d AuthorizationDecision) deny(code, message string, stepUp bool) AuthorizationDecision {
	d.Allowed = false
	d.Code = code
	d.SafeMessage = message
	d.RequiresStep = stepUp
	if d.AuditPayload == nil {
		d.AuditPayload = map[string]any{}
	}
	d.AuditPayload["outcome"] = "denied"
	d.AuditPayload["code"] = code
	return d
}

func AuthorizationError(decision AuthorizationDecision) error {
	if decision.Allowed {
		return nil
	}
	return SecureConnectionError{Code: decision.Code, SafeMessage: decision.SafeMessage, Retryable: decision.RequiresStep}
}

func ValidateStepUpUpdate(claims SessionClaims, stepUpAt time.Time, now time.Time) (SessionClaims, error) {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if stepUpAt.IsZero() || stepUpAt.After(now.Add(30*time.Second)) {
		return claims, fmt.Errorf("invalid step-up verification timestamp")
	}
	claims.StepUpAtUnixMs = stepUpAt.UTC().UnixMilli()
	return claims, nil
}

func requiresStepUp(action ActionRequest) bool {
	switch action.Class {
	case ActionTerminal, ActionTool, ActionSecrets, ActionSecurityConfig, ActionProfileEscalation, ActionDeviceManagement:
		return true
	default:
		return false
	}
}

func hasRecentStepUp(stepUpAtUnixMs int64, now time.Time, ttl time.Duration) bool {
	if stepUpAtUnixMs <= 0 {
		return false
	}
	if ttl <= 0 {
		ttl = DefaultStepUpTTL
	}
	stepUpAt := time.UnixMilli(stepUpAtUnixMs).UTC()
	return !stepUpAt.After(now) && now.Sub(stepUpAt) <= ttl
}

func hasCapability(capabilities []string, want string) bool {
	want = strings.ToLower(strings.TrimSpace(want))
	for _, capability := range NormalizeCapabilities(capabilities) {
		if capability == want {
			return true
		}
	}
	return false
}
