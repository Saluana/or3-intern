package agentcli

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
)

type openCodeStreamState struct {
	lastTextByPart map[string]string
	streamedText   atomic.Bool
}

func newOpenCodeStreamState() *openCodeStreamState {
	return &openCodeStreamState{lastTextByPart: map[string]string{}}
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
		if delta := openCodeTextDeltaFromPart(part, state); delta != "" {
			state.streamedText.Store(true)
			return map[string]any{
				"type": "text",
				"part": map[string]any{"type": "text", "text": delta},
			}, true
		}
		return map[string]any{"type": "message.part.updated", "part": part}, true
	case "message.part.removed":
		return nil, false
	case "message.updated":
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

func openCodeTextDeltaFromPart(part map[string]any, state *openCodeStreamState) string {
	if strings.ToLower(stringField(part, "type")) != "text" {
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
	return newText
}
