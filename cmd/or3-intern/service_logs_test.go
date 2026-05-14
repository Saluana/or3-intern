package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	or3log "or3-intern/internal/log"
)

func TestStreamServiceLogsWritesFilteredSnapshot(t *testing.T) {
	or3log.ResetDefaultBufferForTest(10)
	or3log.DefaultBuffer().Append(or3log.Entry{Level: or3log.LevelInfo, Component: "service", Message: "ignore trace=other", TraceID: "other"})
	or3log.DefaultBuffer().Append(or3log.Entry{Level: or3log.LevelWarn, Component: "service_turn", Message: "approval required trace=trace-a session=session-a", TraceID: "trace-a", Session: "session-a"})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/internal/v1/logs/stream?level=info&trace_id=trace-a", nil)
	streamServiceLogs(rec, req, time.Millisecond)

	if got := rec.Header().Get("Content-Type"); got != "text/event-stream" {
		t.Fatalf("expected text/event-stream content type, got %q", got)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "event: log") || !strings.Contains(body, "trace-a") || !strings.Contains(body, "service_turn") {
		t.Fatalf("expected filtered log event, got %q", body)
	}
	if strings.Contains(body, "ignore") {
		t.Fatalf("unexpected unfiltered log entry in body: %q", body)
	}
}

func TestServiceBoundaryPropagatesTraceID(t *testing.T) {
	server := &serviceServer{}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := serviceTraceIDFromContext(r.Context()); got != "trace-a" {
			t.Fatalf("expected trace-a in request context, got %q", got)
		}
		writeServiceJSON(w, http.StatusOK, map[string]any{"ok": true})
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/internal/v1/test", nil)
	req.Header.Set("X-Trace-Id", "trace-a")
	serviceBoundaryMiddleware(server, handler).ServeHTTP(rec, req)

	if got := rec.Header().Get("X-Trace-Id"); got != "trace-a" {
		t.Fatalf("expected response trace header, got %q", got)
	}
}
