package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWriteServiceJSONAddsErrorCode(t *testing.T) {
	rec := httptest.NewRecorder()
	writeServiceJSON(rec, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})

	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["code"] != serviceCodeMethodNotAllowed {
		t.Fatalf("expected method_not_allowed code, got %#v", payload)
	}
	if payload["request_id"] == "" {
		t.Fatalf("expected generated request id, got %#v", payload)
	}
}

func TestServiceErrorPayloadAddsCodeAndRequestID(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/internal/v1/test", nil)
	req = req.WithContext(contextWithServiceRequestID(req.Context(), "req-test"))

	payload := serviceErrorPayload(req, "session_key is required")

	if payload["code"] != serviceCodeValidationFailed {
		t.Fatalf("expected validation code, got %#v", payload)
	}
	if payload["request_id"] != "req-test" {
		t.Fatalf("expected request id, got %#v", payload)
	}
}

func TestWriteServiceJSONUsesBoundaryRequestID(t *testing.T) {
	rec := httptest.NewRecorder()
	w := &serviceStatusRecorder{ResponseWriter: rec, statusCode: http.StatusOK, requestID: "req-boundary"}

	writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "not found"})

	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["request_id"] != "req-boundary" {
		t.Fatalf("expected boundary request id, got %#v", payload)
	}
	if rec.Header().Get("X-Request-Id") != "req-boundary" {
		t.Fatalf("expected boundary X-Request-Id header, got %q", rec.Header().Get("X-Request-Id"))
	}
}

func contextWithServiceRequestID(ctx context.Context, requestID string) context.Context {
	return context.WithValue(ctx, serviceRequestContextKey{}, serviceRequestContext{RequestID: requestID})
}
