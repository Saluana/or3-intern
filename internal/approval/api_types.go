package approval

import (
	"encoding/json"
	"strings"

	"or3-intern/internal/db"
)

// ApprovalRequestListItem is the public list shape (no raw subject_json).
type ApprovalRequestListItem struct {
	ID                 int64            `json:"id"`
	Type               string           `json:"type"`
	Status             string           `json:"status"`
	PolicyMode         string           `json:"policy_mode,omitempty"`
	Preview            string           `json:"preview,omitempty"`
	RequesterAgentID   string           `json:"requester_agent_id,omitempty"`
	RequesterSessionID string           `json:"requester_session_id,omitempty"`
	RequesterContext   RequesterContext `json:"requester_context,omitempty"`
	ExecutionHostID    string           `json:"execution_host_id,omitempty"`
	RequestedAt        int64            `json:"requested_at"`
	ExpiresAt          int64            `json:"expires_at,omitempty"`
	ResolvedAt         int64            `json:"resolved_at,omitempty"`
}

// ApprovalRequestDetail includes the full subject for step-up detail fetches.
type ApprovalRequestDetail struct {
	ApprovalRequestListItem
	Subject map[string]any `json:"subject,omitempty"`
}

// ApprovalAllowlistItem is the public allowlist shape.
type ApprovalAllowlistItem struct {
	ID        int64          `json:"id"`
	Domain    string         `json:"domain"`
	Scope     map[string]any `json:"scope,omitempty"`
	Matcher   map[string]any `json:"matcher,omitempty"`
	CreatedBy string         `json:"created_by,omitempty"`
	CreatedAt int64          `json:"created_at"`
	ExpiresAt int64          `json:"expires_at,omitempty"`
	Disabled  bool           `json:"disabled"`
}

func ToApprovalRequestListItem(rec db.ApprovalRequestRecord) ApprovalRequestListItem {
	return ApprovalRequestListItem{
		ID:                 rec.ID,
		Type:               rec.Type,
		Status:             rec.Status,
		PolicyMode:         rec.PolicyMode,
		Preview:            SafeSubjectPreview(rec.Type, rec.SubjectJSON),
		RequesterAgentID:   rec.RequesterAgentID,
		RequesterSessionID: rec.RequesterSessionID,
		RequesterContext:   RequesterContextFromJSON(rec.RequesterContextJSON),
		ExecutionHostID:    rec.ExecutionHostID,
		RequestedAt:        rec.RequestedAt,
		ExpiresAt:          rec.ExpiresAt,
		ResolvedAt:         rec.ResolvedAt,
	}
}

func ToApprovalRequestDetail(rec db.ApprovalRequestRecord) ApprovalRequestDetail {
	item := ToApprovalRequestListItem(rec)
	subject := map[string]any{}
	if rec.SubjectJSON != "" {
		_ = jsonUnmarshalMap(rec.SubjectJSON, &subject)
	}
	return ApprovalRequestDetail{
		ApprovalRequestListItem: item,
		Subject:                 subject,
	}
}

func ToApprovalAllowlistItem(rec db.ApprovalAllowlistRecord) ApprovalAllowlistItem {
	scope := map[string]any{}
	matcher := map[string]any{}
	_ = jsonUnmarshalMap(rec.ScopeJSON, &scope)
	_ = jsonUnmarshalMap(rec.MatcherJSON, &matcher)
	return ApprovalAllowlistItem{
		ID:        rec.ID,
		Domain:    rec.Domain,
		Scope:     scope,
		Matcher:   matcher,
		CreatedBy: rec.CreatedBy,
		CreatedAt: rec.CreatedAt,
		ExpiresAt: rec.ExpiresAt,
		Disabled:  rec.DisabledAt > 0,
	}
}

func jsonUnmarshalMap(raw string, out *map[string]any) error {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	return json.Unmarshal([]byte(raw), out)
}

// PairingListItem is a redacted pairing request for list endpoints.
type PairingListItem struct {
	ID          int64  `json:"id"`
	DeviceID    string `json:"device_id"`
	Role        string `json:"role"`
	DisplayName string `json:"display_name,omitempty"`
	Origin      string `json:"origin,omitempty"`
	Status      string `json:"status"`
	RequestedAt int64  `json:"requested_at"`
	ExpiresAt   int64  `json:"expires_at,omitempty"`
}

func ToPairingListItem(rec db.PairingRequestRecord) PairingListItem {
	return PairingListItem{
		ID:          rec.ID,
		DeviceID:    rec.DeviceID,
		Role:        rec.Role,
		DisplayName: rec.DisplayName,
		Origin:      rec.Origin,
		Status:      rec.Status,
		RequestedAt: rec.RequestedAt,
		ExpiresAt:   rec.ExpiresAt,
	}
}
