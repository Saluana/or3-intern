package secureconn

import (
	"fmt"
	"strings"
	"time"
)

const RedactedValue = "[redacted]"

type CapabilityDiscovery struct {
	Version                   int      `json:"version"`
	SupportedProtocolVersions []int    `json:"supported_protocol_versions"`
	QRPairingV2               bool     `json:"qr_pairing_v2"`
	RelayRendezvous           bool     `json:"relay_rendezvous"`
	EnrollmentCertificates    bool     `json:"enrollment_certificates"`
	SecureFrames              bool     `json:"secure_frames"`
	LegacyPairingRemote       bool     `json:"legacy_pairing_remote"`
	Capabilities              []string `json:"capabilities"`
}

type WebEnrollmentPolicy struct {
	TrustLevel          string   `json:"trust_level"`
	MaxCertificateTTL   string   `json:"max_certificate_ttl"`
	AllowedCapabilities []string `json:"allowed_capabilities"`
	RequiresStepUp      bool     `json:"requires_step_up"`
}

type TelemetryEvent struct {
	Event      string         `json:"event"`
	Outcome    string         `json:"outcome"`
	Reason     string         `json:"reason,omitempty"`
	HostIDHash string         `json:"host_id_hash,omitempty"`
	DeviceHash string         `json:"device_hash,omitempty"`
	RouteID    string         `json:"route_id,omitempty"`
	LatencyMS  int64          `json:"latency_ms,omitempty"`
	CreatedAt  int64          `json:"created_at"`
	Metadata   map[string]any `json:"metadata,omitempty"`
}

func CurrentCapabilityDiscovery() CapabilityDiscovery {
	return CapabilityDiscovery{
		Version:                   ProtocolVersion,
		SupportedProtocolVersions: []int{ProtocolVersion},
		QRPairingV2:               true,
		RelayRendezvous:           true,
		EnrollmentCertificates:    true,
		SecureFrames:              true,
		LegacyPairingRemote:       false,
		Capabilities:              []string{CapabilityChat, CapabilityFiles, CapabilityTerminal, CapabilityTools, CapabilityDevices},
	}
}

func DefaultWebEnrollmentPolicy() WebEnrollmentPolicy {
	return WebEnrollmentPolicy{
		TrustLevel:          TrustWebLimited,
		MaxCertificateTTL:   (24 * time.Hour).String(),
		AllowedCapabilities: []string{CapabilityChat, CapabilityFiles},
		RequiresStepUp:      true,
	}
}

func ApplyWebEnrollmentRestrictions(platform string, requested []string, requestedExpiry time.Time, now time.Time) ([]string, time.Time, string) {
	if NormalizePlatform(platform) != PlatformWeb {
		return NormalizeCapabilities(requested), requestedExpiry, NormalizeTrustLevel("", platform)
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	policy := DefaultWebEnrollmentPolicy()
	allowed := map[string]bool{}
	for _, capability := range policy.AllowedCapabilities {
		allowed[capability] = true
	}
	out := []string{}
	for _, capability := range NormalizeCapabilities(requested) {
		if allowed[capability] {
			out = append(out, capability)
		}
	}
	maxExpiry := now.Add(24 * time.Hour)
	if requestedExpiry.IsZero() || requestedExpiry.After(maxExpiry) {
		requestedExpiry = maxExpiry
	}
	return out, requestedExpiry, TrustWebLimited
}

func BuildTelemetryEvent(event, outcome, reason string, fields map[string]any, now time.Time) TelemetryEvent {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	redacted := RedactSecureConnectionLogValue(fields).(map[string]any)
	return TelemetryEvent{
		Event:      strings.TrimSpace(event),
		Outcome:    strings.TrimSpace(outcome),
		Reason:     strings.TrimSpace(reason),
		HostIDHash: stringField(redacted, "host_id_hash"),
		DeviceHash: stringField(redacted, "device_hash"),
		RouteID:    stringField(redacted, "route_id"),
		LatencyMS:  int64Field(redacted, "latency_ms"),
		CreatedAt:  now.UTC().UnixMilli(),
		Metadata:   redacted,
	}
}

func RedactSecureConnectionLogValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		out := map[string]any{}
		for key, item := range typed {
			if IsSensitiveLogKey(key) {
				out[key] = RedactedValue
				continue
			}
			out[key] = RedactSecureConnectionLogValue(item)
		}
		return out
	case []any:
		out := make([]any, 0, len(typed))
		for _, item := range typed {
			out = append(out, RedactSecureConnectionLogValue(item))
		}
		return out
	default:
		return value
	}
}

func IsSensitiveLogKey(key string) bool {
	key = strings.ToLower(strings.TrimSpace(key))
	for _, marker := range []string{"secret", "token", "certificate", "private", "plaintext", "payload", "command", "terminal", "tool_args", "file_content", "session_key"} {
		if strings.Contains(key, marker) {
			return true
		}
	}
	return false
}

func LegacyPairingAllowedForRemote(remote bool, explicitOverride bool) bool {
	if remote && !explicitOverride {
		return false
	}
	return true
}

func stringField(fields map[string]any, key string) string {
	return strings.TrimSpace(fmt.Sprint(fields[key]))
}

func int64Field(fields map[string]any, key string) int64 {
	switch value := fields[key].(type) {
	case int64:
		return value
	case int:
		return int64(value)
	case float64:
		return int64(value)
	default:
		return 0
	}
}
