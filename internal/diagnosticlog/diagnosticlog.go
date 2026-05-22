package diagnosticlog

import (
	"encoding/json"
	"fmt"
	"strings"

	"or3-intern/internal/adminflow"
	"or3-intern/internal/db"
	"or3-intern/internal/doctor"
)

const (
	DefaultLimit = 100
	MaxLimit     = 200
)

type ClientDiagnostics struct {
	HostProfile           string `json:"host_profile,omitempty"`
	PairingState          string `json:"pairing_state,omitempty"`
	SessionState          string `json:"session_state,omitempty"`
	BaseURL               string `json:"base_url,omitempty"`
	BootstrapReachable    *bool  `json:"bootstrap_reachable,omitempty"`
	ErrorCategory         string `json:"error_category,omitempty"`
	Timeout               bool   `json:"timeout,omitempty"`
	Refused               bool   `json:"refused,omitempty"`
	AuthError             bool   `json:"auth_error,omitempty"`
	CachedRestartGuidance string `json:"cached_restart_guidance,omitempty"`
}

func NewEvent(source, level, correlationID, eventType string, payload any) db.DiagnosticLogEvent {
	raw := RedactPayload(payload)
	return db.DiagnosticLogEvent{
		Source:        clean(source, "doctor"),
		Level:         clean(level, "info"),
		CorrelationID: strings.TrimSpace(correlationID),
		EventType:     strings.TrimSpace(eventType),
		Payload:       raw,
		SizeBytes:     int64(len(raw)),
	}
}

func RedactPayload(payload any) json.RawMessage {
	if payload == nil {
		return json.RawMessage(`{}`)
	}
	var value any
	switch typed := payload.(type) {
	case json.RawMessage:
		value = decodeJSON(typed)
	case []byte:
		value = decodeJSON(typed)
	case string:
		value = adminflow.SanitizeForAI(typed)
	default:
		value = payload
	}
	redacted := redactAny(value)
	data, err := json.Marshal(redacted)
	if err != nil || len(data) == 0 {
		return json.RawMessage(`{}`)
	}
	return data
}

func FindingsFromClientDiagnostics(diag ClientDiagnostics) []doctor.Finding {
	category := strings.ToLower(strings.TrimSpace(diag.ErrorCategory))
	if category == "" {
		switch {
		case diag.Timeout:
			category = "timeout"
		case diag.Refused:
			category = "refused"
		case diag.AuthError:
			category = "auth"
		}
	}
	if category == "" && diag.BootstrapReachable != nil && *diag.BootstrapReachable {
		return nil
	}
	severity := doctor.SeverityWarn
	summary := "App reported service connection trouble"
	detail := "The app could not confirm that the OR3 service is reachable."
	switch category {
	case "timeout":
		severity = doctor.SeverityError
		summary = "App connection to the service timed out"
		detail = "The configured service URL did not respond before the app timeout."
	case "refused", "connection_refused":
		severity = doctor.SeverityError
		summary = "App connection to the service was refused"
		detail = "The service host was reachable, but nothing accepted the connection at the configured address."
	case "auth", "unauthorized", "forbidden":
		severity = doctor.SeverityWarn
		summary = "App service authentication needs attention"
		detail = "The app reached the service, but the current pairing or session was not accepted."
	}
	evidence := []string{}
	for _, item := range []string{
		"host_profile=" + diag.HostProfile,
		"pairing_state=" + diag.PairingState,
		"session_state=" + diag.SessionState,
		"base_url=" + diag.BaseURL,
		"error_category=" + category,
	} {
		if !strings.HasSuffix(item, "=") {
			evidence = append(evidence, adminflow.SanitizeForAI(item))
		}
	}
	fixHint := strings.TrimSpace(diag.CachedRestartGuidance)
	if fixHint == "" {
		fixHint = "Check the app connection settings, verify pairing/session state, then restart the OR3 service if it is not responding."
	}
	return []doctor.Finding{{
		ID:       "app.service_down." + clean(category, "unknown"),
		Area:     "app",
		Severity: severity,
		Summary:  summary,
		Detail:   detail,
		Evidence: evidence,
		FixMode:  doctor.FixModeManual,
		FixHint:  fixHint,
		Metadata: map[string]string{"source": "client_diagnostics", "category": category},
	}}
}

func decodeJSON(raw []byte) any {
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return adminflow.SanitizeForAI(string(raw))
	}
	return value
}

func redactAny(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return adminflow.RedactJSON(redactMapStrings(typed))
	case []any:
		out := make([]any, 0, len(typed))
		for _, item := range typed {
			out = append(out, redactAny(item))
		}
		return out
	case string:
		return adminflow.SanitizeForAI(typed)
	default:
		return typed
	}
}

func redactMapStrings(source map[string]any) map[string]any {
	out := make(map[string]any, len(source))
	for key, value := range source {
		switch typed := value.(type) {
		case string:
			out[key] = adminflow.SanitizeForAI(typed)
		case map[string]any:
			out[key] = redactMapStrings(typed)
		case []any:
			out[key] = redactAny(typed)
		default:
			out[key] = typed
		}
	}
	return out
}

func clean(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' {
			return r
		}
		return '_'
	}, value)
}

func Pattern(value string) string {
	value = strings.TrimSpace(value)
	if len(value) > 120 {
		value = value[:120]
	}
	return fmt.Sprintf("%%%s%%", strings.ReplaceAll(value, "%", `\%`))
}
