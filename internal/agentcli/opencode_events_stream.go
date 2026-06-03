package agentcli

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"time"
)

type openCodeStreamState struct {
	lastTextByPart  map[string]string
	messageRoleByID map[string]string
	partByID        map[string]map[string]any
	streamedText    atomic.Bool
}

func newOpenCodeStreamState() *openCodeStreamState {
	return &openCodeStreamState{
		lastTextByPart:  map[string]string{},
		messageRoleByID: map[string]string{},
		partByID:        map[string]map[string]any{},
	}
}

// streamOpenCodeGlobalEvents subscribes to GET /event (SSE) and forwards session-scoped
// events to the agent run event handler in the shape expected by OpenCodeAdapter.NormalizeChatEvent.
func streamOpenCodeGlobalEvents(ctx context.Context, client *http.Client, endpoint, sessionID string, onEvent func(AgentRunEvent), seq *int64, state *openCodeStreamState) error {
	if client == nil || onEvent == nil || strings.TrimSpace(sessionID) == "" {
		return nil
	}
	endpoint = strings.TrimRight(strings.TrimSpace(endpoint), "/")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"/event", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "text/event-stream")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("GET %s/event failed: %s %s", endpoint, resp.Status, strings.TrimSpace(string(data)))
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" || data == "[DONE]" {
			continue
		}
		var bus map[string]any
		if err := json.Unmarshal([]byte(data), &bus); err != nil {
			continue
		}
		if !openCodeBusEventMatchesSession(bus, sessionID) {
			continue
		}
		payload, ok := openCodeBusEventToStructuredPayload(bus, state)
		if !ok {
			continue
		}
		emitNativeStructured(seq, onEvent, payload)
	}
	return scanner.Err()
}

func openCodeBusEventMatchesSession(bus map[string]any, sessionID string) bool {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return true
	}
	props := mapField(bus, "properties")
	for _, candidate := range []string{
		stringField(bus, "sessionID"),
		stringField(bus, "session_id"),
		stringField(props, "sessionID"),
		stringField(props, "session_id"),
		stringField(mapField(props, "session"), "id"),
		stringField(mapField(props, "info"), "sessionID"),
		stringField(mapField(props, "info"), "session_id"),
		stringField(mapField(props, "part"), "sessionID"),
		stringField(mapField(props, "part"), "session_id"),
	} {
		if candidate != "" && candidate == sessionID {
			return true
		}
	}
	// Permission and file events may omit session id; allow them through.
	switch stringField(bus, "type") {
	case "permission.updated", "session.error", "session.idle", "session.status", "session.updated":
		return true
	default:
		return false
	}
}

func openCodeBusEventToStructuredPayload(bus map[string]any, state *openCodeStreamState) (map[string]any, bool) {
	if state == nil {
		state = newOpenCodeStreamState()
	}
	eventType := stringField(bus, "type")
	props := mapField(bus, "properties")
	switch eventType {
	case "message.part.updated":
		part := mapField(props, "part")
		if len(part) == 0 {
			return nil, false
		}
		state.storePart(part)
		state.captureMessageRole(props, part)
		role := state.partRole(props, part)
		if role == "user" {
			_ = openCodeTextDeltaFromPart(part, state)
			return nil, false
		}
		if payload, ok := state.emitOpenCodeStreamTextDelta(part); ok {
			return payload, true
		}
		return map[string]any{"type": "message.part.updated", "part": part}, true
	case "message.part.delta":
		partID := firstNonEmpty(
			stringField(props, "partID"),
			stringField(props, "partId"),
			stringField(props, "part_id"),
		)
		delta := extractString(props["delta"])
		if partID == "" || delta == "" {
			return nil, false
		}
		existingPart := state.partByID[partID]
		if len(existingPart) == 0 {
			return nil, false
		}
		role := state.partRole(props, existingPart)
		if role == "user" {
			return nil, false
		}
		previousText := state.lastTextByPart[partID]
		if previousText == "" {
			previousText = extractString(existingPart["text"])
		}
		nextText := previousText + delta
		state.lastTextByPart[partID] = nextText
		updatedPart := cloneStringAnyMap(existingPart)
		updatedPart["text"] = nextText
		state.storePart(updatedPart)
		deltaPart := cloneStringAnyMap(updatedPart)
		deltaPart["text"] = delta
		state.streamedText.Store(true)
		// #region agent log
		agentDebugLog("A-D", "opencode_events_stream.go:part-delta", "OpenCode part delta emitted", map[string]any{
			"partKind":     stringField(updatedPart, "kind"),
			"partType":     stringField(updatedPart, "type"),
			"isReasoning":  openCodePartIsReasoning(updatedPart),
			"deltaPreview": truncateDiagnostic(delta),
			"partID":       partID,
		})
		// #endregion
		return map[string]any{
			"type": "text",
			"part": deltaPart,
		}, true
	case "message.part.removed":
		partID := firstNonEmpty(
			stringField(props, "partID"),
			stringField(props, "partId"),
			stringField(props, "part_id"),
		)
		if partID != "" {
			delete(state.partByID, partID)
			delete(state.lastTextByPart, partID)
		}
		return nil, false
	case "message.updated":
		message := mapField(props, "message")
		if len(message) == 0 {
			message = mapField(props, "info")
		}
		state.captureMessageRole(props, message)
		return nil, false
	case "permission.updated":
		permission := firstNonNil(props["permission"], props["request"], props)
		return map[string]any{
			"type":       "permission.asked",
			"permission": permission,
			"message":    extractString(firstNonNil(props["message"], permission)),
		}, true
	case "session.error":
		return map[string]any{
			"type":    "session.error",
			"message": extractString(firstNonNil(props["message"], props["error"], mapField(props, "error"))),
			"error":   props["error"],
		}, true
	case "session.idle":
		if sessionID := firstNonEmpty(stringField(props, "sessionID"), stringField(props, "session_id")); sessionID != "" {
			return map[string]any{"type": "session.status", "status": "idle", "sessionID": sessionID}, true
		}
		return map[string]any{"type": "session.status", "status": "idle"}, true
	case "session.status", "session.updated":
		status := firstNonEmpty(
			stringField(props, "status"),
			stringField(props, "state"),
			stringField(mapField(props, "session"), "status"),
			stringField(mapField(props, "session"), "state"),
			stringField(mapField(props, "info"), "status"),
			stringField(mapField(props, "info"), "state"),
		)
		if strings.EqualFold(status, "idle") {
			return map[string]any{
				"type":      "session.status",
				"status":    "idle",
				"sessionID": firstNonEmpty(stringField(props, "sessionID"), stringField(props, "session_id"), stringField(mapField(props, "session"), "id")),
			}, true
		}
		return nil, false
	default:
		return nil, false
	}
}

func (state *openCodeStreamState) storePart(part map[string]any) {
	if state == nil || len(part) == 0 {
		return
	}
	if state.partByID == nil {
		state.partByID = map[string]map[string]any{}
	}
	partID := firstNonEmpty(stringField(part, "id"), stringField(part, "callID"), stringField(part, "messageID"))
	if partID == "" {
		return
	}
	state.partByID[partID] = cloneStringAnyMap(part)
}

func (state *openCodeStreamState) emitOpenCodeStreamTextDelta(part map[string]any) (map[string]any, bool) {
	delta := openCodeTextDeltaFromPart(part, state)
	if delta == "" {
		return nil, false
	}
	state.streamedText.Store(true)
	deltaPart := cloneStringAnyMap(part)
	deltaPart["text"] = delta
	// #region agent log
	agentDebugLog("A-D", "opencode_events_stream.go:text-delta", "OpenCode text delta emitted", map[string]any{
		"partKind":     stringField(part, "kind"),
		"partType":     stringField(part, "type"),
		"isReasoning":  openCodePartIsReasoning(part),
		"deltaPreview": truncateDiagnostic(delta),
		"partID":       firstNonEmpty(stringField(part, "id"), stringField(part, "callID"), stringField(part, "messageID")),
	})
	// #endregion
	return map[string]any{
		"type": "text",
		"part": deltaPart,
	}, true
}

func (state *openCodeStreamState) captureMessageRole(values ...map[string]any) {
	if state == nil {
		return
	}
	if state.messageRoleByID == nil {
		state.messageRoleByID = map[string]string{}
	}
	for _, value := range values {
		if len(value) == 0 {
			continue
		}
		id := firstNonEmpty(
			stringField(value, "messageID"),
			stringField(value, "messageId"),
			stringField(value, "message_id"),
			stringField(value, "id"),
		)
		role := strings.ToLower(firstNonEmpty(
			stringField(value, "role"),
			stringField(mapField(value, "message"), "role"),
			stringField(mapField(value, "info"), "role"),
		))
		if id != "" && role != "" {
			state.messageRoleByID[id] = role
		}
	}
}

func (state *openCodeStreamState) partRole(props, part map[string]any) string {
	role := strings.ToLower(firstNonEmpty(
		stringField(part, "role"),
		stringField(props, "role"),
		stringField(mapField(props, "message"), "role"),
		stringField(mapField(props, "info"), "role"),
	))
	if role != "" {
		return role
	}
	if state == nil {
		return ""
	}
	for _, id := range []string{
		stringField(part, "messageID"),
		stringField(part, "messageId"),
		stringField(part, "message_id"),
		stringField(props, "messageID"),
		stringField(props, "messageId"),
		stringField(props, "message_id"),
	} {
		if id == "" {
			continue
		}
		if role := state.messageRoleByID[id]; role != "" {
			return role
		}
	}
	return ""
}

func cloneStringAnyMap(input map[string]any) map[string]any {
	out := make(map[string]any, len(input)+1)
	for key, value := range input {
		out[key] = value
	}
	return out
}

func openCodeStreamTextPartType(partType string) bool {
	switch strings.ToLower(strings.TrimSpace(partType)) {
	case "text", "reasoning":
		return true
	default:
		return false
	}
}

func openCodeTextDeltaFromPart(part map[string]any, state *openCodeStreamState) string {
	if !openCodeStreamTextPartType(stringField(part, "type")) {
		return ""
	}
	partID := firstNonEmpty(stringField(part, "id"), stringField(part, "callID"), stringField(part, "messageID"), "text")
	newText := extractString(part["text"])
	if newText == "" {
		return ""
	}
	prev := state.lastTextByPart[partID]
	state.lastTextByPart[partID] = newText
	if prev == "" {
		return newText
	}
	if strings.HasPrefix(newText, prev) {
		return newText[len(prev):]
	}
	if strings.HasPrefix(prev, newText) {
		return ""
	}
	return newText
}

func agentDebugLog(hypothesisID, location, message string, data map[string]any) {
	entry := map[string]any{
		"sessionId":    "3fa1b1",
		"runId":        "post-fix",
		"hypothesisId": hypothesisID,
		"location":     location,
		"message":      message,
		"data":         data,
		"timestamp":    time.Now().UnixMilli(),
	}
	raw, err := json.Marshal(entry)
	if err != nil {
		return
	}
	file, err := os.OpenFile("/Users/brendon/Documents/or3-intern-app/.cursor/debug-3fa1b1.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer file.Close()
	_, _ = file.Write(append(raw, '\n'))
}
