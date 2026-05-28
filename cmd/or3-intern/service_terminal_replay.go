package main

import (
	"maps"
	"time"
)

func terminalEventOutputBytes(event serviceTerminalEvent) int {
	if event.Type != "output" {
		return 0
	}
	chunk, _ := event.Data["chunk"].(string)
	return len(chunk)
}

func terminalReplayOutputBytes(events []serviceTerminalEvent) int {
	total := 0
	for _, event := range events {
		total += terminalEventOutputBytes(event)
	}
	return total
}

func (s *serviceTerminalSession) trimReplayBufferLocked() {
	excess := terminalReplayOutputBytes(s.events) - serviceTerminalReplayMaxBytes
	if excess <= 0 {
		return
	}
	filtered := s.events[:0]
	for _, event := range s.events {
		if excess > 0 && event.Type == "output" {
			excess -= terminalEventOutputBytes(event)
			continue
		}
		filtered = append(filtered, event)
	}
	s.events = filtered
}

func (s *serviceTerminalSession) publishTerminalEvent(event serviceTerminalEvent) {
	for subscriber := range s.subscribers {
		select {
		case subscriber <- event:
		default:
			delete(s.subscribers, subscriber)
			close(subscriber)
		}
	}
}

func (s *serviceTerminalSession) appendEvent(eventType string, data map[string]any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC()
	s.LastActiveAt = now
	s.ExpiresAt = now.Add(serviceTerminalSessionTTL)
	if data == nil {
		data = map[string]any{}
	}
	if _, ok := data["session_id"]; !ok {
		data["session_id"] = s.ID
	}

	if eventType == "output" {
		chunk, _ := data["chunk"].(string)
		stream, _ := data["stream"].(string)
		if len(s.events) > 0 {
			last := &s.events[len(s.events)-1]
			if last.Type == "output" {
				lastStream, _ := last.Data["stream"].(string)
				if lastStream == stream {
					prev, _ := last.Data["chunk"].(string)
					last.Data = map[string]any{
						"session_id": s.ID,
						"stream":     stream,
						"chunk":      prev + chunk,
					}
					s.trimReplayBufferLocked()
					live := serviceTerminalEvent{
						Type: "output",
						Data: map[string]any{
							"session_id": s.ID,
							"stream":     stream,
							"chunk":      chunk,
						},
					}
					s.publishTerminalEvent(live)
					return
				}
			}
		}
	}

	event := serviceTerminalEvent{Type: eventType, Data: maps.Clone(data)}
	s.events = append(s.events, event)
	s.trimReplayBufferLocked()
	s.publishTerminalEvent(event)
}
