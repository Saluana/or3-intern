package main

import (
	"net/http"
	"strings"
	"time"

	or3log "or3-intern/internal/log"
)

const serviceLogsIdleTimeout = 5 * time.Minute

func (s *serviceServer) handleLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		return
	}
	streamServiceLogs(w, r, serviceLogsIdleTimeout)
}

func streamServiceLogs(w http.ResponseWriter, r *http.Request, idleTimeout time.Duration) {
	filter := serviceLogFilterFromRequest(r)
	if err := beginSSE(w); err != nil {
		writeServiceError(w, r, http.StatusInternalServerError, "streaming is not supported", err)
		return
	}
	for _, entry := range or3log.DefaultBuffer().Snapshot(filter) {
		if err := writeSSEEvent(w, "log", serviceLogPayload(entry)); err != nil {
			return
		}
	}
	entries, unsubscribe := or3log.DefaultBuffer().Subscribe(128)
	defer unsubscribe()
	if idleTimeout <= 0 {
		idleTimeout = serviceLogsIdleTimeout
	}
	idle := time.NewTimer(idleTimeout)
	defer idle.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-idle.C:
			return
		case entry, ok := <-entries:
			if !ok {
				return
			}
			if !filter.Matches(entry) {
				continue
			}
			if err := writeSSEEvent(w, "log", serviceLogPayload(entry)); err != nil {
				return
			}
			if !idle.Stop() {
				select {
				case <-idle.C:
				default:
				}
			}
			idle.Reset(idleTimeout)
		}
	}
}

func serviceLogFilterFromRequest(r *http.Request) or3log.Filter {
	query := r.URL.Query()
	level, ok := or3log.ParseLevel(query.Get("level"))
	if !ok {
		level = or3log.LevelInfo
	}
	return or3log.Filter{
		MinLevel:  level,
		Component: strings.TrimSpace(query.Get("component")),
		TraceID:   strings.TrimSpace(query.Get("trace_id")),
		Session:   strings.TrimSpace(query.Get("session")),
	}
}

func serviceLogPayload(entry or3log.Entry) map[string]any {
	payload := map[string]any{
		"id":        entry.ID,
		"timestamp": entry.Timestamp.Format(time.RFC3339Nano),
		"level":     entry.Level,
		"component": entry.Component,
		"message":   entry.Message,
	}
	if entry.TraceID != "" {
		payload["trace_id"] = entry.TraceID
	}
	if entry.Session != "" {
		payload["session"] = entry.Session
	}
	if len(entry.Fields) > 0 {
		payload["fields"] = entry.Fields
	}
	return payload
}
