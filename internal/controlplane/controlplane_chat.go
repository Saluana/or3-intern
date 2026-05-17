package controlplane

import (
	"encoding/json"
	"strings"

	"or3-intern/internal/agentcli"
	"or3-intern/internal/db"
)

// BuildChatRunner formats one runner's chat-discovery response item.
// `info` is detection metadata for the runner (or zero-value for or3-intern).
func BuildChatRunner(spec agentcli.RunnerSpec, info agentcli.RunnerInfo, defaultModel, defaultMode, defaultIsolation, defaultCwd string) map[string]any {
	id := info.ID
	if id == "" {
		id = string(spec.ID)
	}
	display := info.DisplayName
	if display == "" {
		display = spec.DisplayName
	}
	status := info.Status
	authStatus := info.AuthStatus
	if string(spec.ID) == string(agentcli.RunnerOR3) {
		// or3-intern is always available.
		if status == "" {
			status = agentcli.RunnerStatusAvailable
		}
		if authStatus == "" {
			authStatus = agentcli.AuthReady
		}
	}
	out := map[string]any{
		"id":                id,
		"display_name":      display,
		"status":            string(status),
		"auth_status":       string(authStatus),
		"supports":          spec.Supports,
		"chat_capabilities": spec.Supports.Chat,
	}
	if info.Runtime.Kind != "" || len(info.Runtime.Models) > 0 || info.Runtime.DefaultModel != "" {
		out["runtime"] = info.Runtime
		if len(info.Runtime.Models) > 0 {
			out["models"] = info.Runtime.Models
		}
		if strings.TrimSpace(info.Runtime.DefaultModel) != "" {
			defaultModel = info.Runtime.DefaultModel
		}
	}
	if v := strings.TrimSpace(info.Version); v != "" {
		out["version"] = v
	}
	if v := strings.TrimSpace(info.BinaryPath); v != "" {
		out["binary_path"] = v
	}
	if v := strings.TrimSpace(info.BinaryName); v != "" {
		out["binary_name"] = v
	}
	if reason := strings.TrimSpace(info.DisabledReason); reason != "" {
		out["disabled_reason"] = reason
	}
	if defaultModel != "" {
		out["default_model"] = defaultModel
	}
	if defaultMode != "" {
		out["default_mode"] = defaultMode
	}
	if defaultIsolation != "" {
		out["default_isolation"] = defaultIsolation
	}
	if defaultCwd != "" {
		out["default_cwd"] = defaultCwd
	}
	return out
}

// BuildChatRunnerListResponse renders the /internal/v1/chat-runners response.
func BuildChatRunnerListResponse(items []map[string]any) map[string]any {
	if items == nil {
		items = []map[string]any{}
	}
	return map[string]any{"runners": items}
}

// BuildRunnerChatSessionResponse converts a persisted runner_chat_sessions row.
func BuildRunnerChatSessionResponse(s db.RunnerChatSession) map[string]any {
	out := map[string]any{
		"id":                s.ID,
		"app_session_key":   s.AppSessionKey,
		"runner_id":         s.RunnerID,
		"continuation_mode": s.ContinuationMode,
		"created_at":        s.CreatedAt,
		"updated_at":        s.UpdatedAt,
	}
	if v := strings.TrimSpace(s.NativeSessionRef); v != "" {
		out["native_session_ref"] = v
	}
	if v := strings.TrimSpace(s.Model); v != "" {
		out["model"] = v
	}
	if v := strings.TrimSpace(s.Mode); v != "" {
		out["mode"] = v
	}
	if v := strings.TrimSpace(s.Isolation); v != "" {
		out["isolation"] = v
	}
	if v := strings.TrimSpace(s.Cwd); v != "" {
		out["cwd"] = v
	}
	if s.MaxTurns > 0 {
		out["max_turns"] = s.MaxTurns
	}
	if s.MetaJSON != "" && s.MetaJSON != "{}" {
		out["meta"] = json.RawMessage(s.MetaJSON)
	}
	return out
}

// BuildRunnerChatTurnResponse converts a persisted runner_chat_turns row.
func BuildRunnerChatTurnResponse(t db.RunnerChatTurn) map[string]any {
	out := map[string]any{
		"id":                t.ID,
		"session_id":        t.SessionID,
		"sequence":          t.Sequence,
		"status":            t.Status,
		"continuation_mode": t.ContinuationMode,
		"requested_at":      t.RequestedAt,
	}
	if t.StartedAt > 0 {
		out["started_at"] = t.StartedAt
	}
	if t.CompletedAt > 0 {
		out["completed_at"] = t.CompletedAt
	}
	if v := strings.TrimSpace(t.UserMessage); v != "" {
		out["user_message"] = v
	}
	if v := strings.TrimSpace(t.FinalText); v != "" {
		out["final_text"] = v
	}
	if v := strings.TrimSpace(t.ErrorMessage); v != "" {
		out["error"] = v
	}
	if v := strings.TrimSpace(t.AgentCLIRunID); v != "" {
		out["agent_cli_run_id"] = v
	}
	if v := strings.TrimSpace(t.AgentCLIJobID); v != "" {
		out["agent_cli_job_id"] = v
	}
	if t.UserMessageID > 0 {
		out["user_message_id"] = t.UserMessageID
	}
	if t.AssistantMessageID > 0 {
		out["assistant_message_id"] = t.AssistantMessageID
	}
	if v := strings.TrimSpace(t.Model); v != "" {
		out["model"] = v
	}
	if v := strings.TrimSpace(t.Mode); v != "" {
		out["mode"] = v
	}
	if v := strings.TrimSpace(t.Isolation); v != "" {
		out["isolation"] = v
	}
	if v := strings.TrimSpace(t.Cwd); v != "" {
		out["cwd"] = v
	}
	return out
}

// BuildRunnerChatEventResponse converts a runner_chat_events row.
func BuildRunnerChatEventResponse(e db.RunnerChatEvent) map[string]any {
	out := map[string]any{
		"id":      e.ID,
		"turn_id": e.TurnID,
		"seq":     e.Seq,
		"ts":      e.TS,
		"type":    e.Type,
	}
	if v := strings.TrimSpace(e.Stream); v != "" {
		out["stream"] = v
	}
	if v := strings.TrimSpace(e.Text); v != "" {
		out["text"] = v
	}
	if v := strings.TrimSpace(e.JobID); v != "" {
		out["job_id"] = v
	}
	if v := strings.TrimSpace(e.PayloadJSON); v != "" {
		out["payload"] = json.RawMessage(v)
	}
	return out
}

// BuildRunnerChatEventListResponse renders an event list payload.
func BuildRunnerChatEventListResponse(events []db.RunnerChatEvent) map[string]any {
	items := make([]map[string]any, 0, len(events))
	for _, e := range events {
		items = append(items, BuildRunnerChatEventResponse(e))
	}
	return map[string]any{"events": items}
}

// BuildChatSessionMetaResponse converts a chat_session_meta row.
func BuildChatSessionMetaResponse(m db.ChatSessionMeta) map[string]any {
	return map[string]any{
		"session_key":              m.SessionKey,
		"host_id":                  m.HostID,
		"title":                    m.Title,
		"runner_id":                m.RunnerID,
		"runner_label":             m.RunnerLabel,
		"runner_chat_session_id":   m.RunnerChatSessionID,
		"runner_continuation_mode": m.RunnerContinuationMode,
		"runner_model":             m.RunnerModel,
		"runner_mode":              m.RunnerMode,
		"runner_isolation":         m.RunnerIsolation,
		"runner_cwd":               m.RunnerCwd,
		"message_count":            m.MessageCount,
		"last_message_preview":     m.LastMessagePreview,
		"last_message_at":          m.LastMessageAt,
		"parent_session_key":       m.ParentSessionKey,
		"fork_anchor_message_id":   m.ForkAnchorMessageID,
		"forked_from_runner_id":    m.ForkedFromRunnerID,
		"fork_strategy":            m.ForkStrategy,
		"archived":                 m.Archived,
		"created_at":               m.CreatedAt,
		"updated_at":               m.UpdatedAt,
	}
}

// BuildChatSessionListResponse renders the chat sessions listing.
func BuildChatSessionListResponse(items []db.ChatSessionMeta) map[string]any {
	out := make([]map[string]any, 0, len(items))
	for _, m := range items {
		out = append(out, BuildChatSessionMetaResponse(m))
	}
	return map[string]any{"sessions": out}
}

// BuildChatMessageResponse renders one row from messages for chat history.
func BuildChatMessageResponse(m db.ChatMessage) map[string]any {
	out := map[string]any{
		"id":          m.ID,
		"session_key": m.SessionKey,
		"role":        m.Role,
		"content":     m.Content,
		"created_at":  m.CreatedAt,
	}
	if m.PayloadJSON != "" && m.PayloadJSON != "{}" {
		out["payload"] = json.RawMessage(m.PayloadJSON)
	}
	return out
}

// BuildChatMessagePageResponse renders a paged messages list.
func BuildChatMessagePageResponse(page db.ChatMessagePage) map[string]any {
	items := make([]map[string]any, 0, len(page.Messages))
	for _, m := range page.Messages {
		items = append(items, BuildChatMessageResponse(m))
	}
	resp := map[string]any{"messages": items}
	if page.NextCursor > 0 {
		resp["next_cursor"] = page.NextCursor
	}
	return resp
}
