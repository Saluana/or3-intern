package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"or3-intern/internal/config"
	"or3-intern/internal/jobs"
	"or3-intern/internal/security"
)

type serviceAppUsageRouteFixture struct {
	Area   string `json:"area"`
	Method string `json:"method"`
	Path   string `json:"path"`
}

func TestOr3NetCompatibilityFixtures_RequestDecoding(t *testing.T) {
}

func TestOr3NetCompatibilityFixtures_Responses(t *testing.T) {
	t.Run("job stream attach", func(t *testing.T) {
		jobs := jobs.NewRegistry(time.Minute, 32)
		job := jobs.RegisterWithID("job_fixture", "turn")
		jobs.Publish(job.ID, "queued", map[string]any{"status": "queued"})
		jobs.Publish(job.ID, "started", map[string]any{"status": "running"})
		jobs.Complete(job.ID, "completed", map[string]any{"final_text": "done"})
		server := &serviceServer{jobs: jobs}

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/internal/v1/jobs/"+job.ID+"/stream", nil)
		server.handleJobs(rec, req)

		body := strings.ReplaceAll(rec.Body.String(), job.ID, "__JOB_ID__")
		expected := loadFixtureString(t, "service_contract/job-stream.sse")
		if body != expected {
			t.Fatalf("job stream fixture mismatch\nexpected:\n%s\ngot:\n%s", expected, body)
		}

		actualLines := sseBodyToJSONLines(t, body, job.ID)
		expectedLines := loadFixtureJSONLines(t, "service_contract/intern-stream-events.jsonl")
		if !reflect.DeepEqual(actualLines, expectedLines) {
			t.Fatalf("frozen intern stream events mismatch\nexpected: %#v\ngot: %#v", expectedLines, actualLines)
		}
	})

	t.Run("job abort", func(t *testing.T) {
		jobs := jobs.NewRegistry(time.Minute, 32)
		snapshot := jobs.RegisterWithID("job_fixture", "turn")
		jobs.Complete(snapshot.ID, "completed", map[string]any{"final_text": "done"})
		server := &serviceServer{jobs: jobs}

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/internal/v1/jobs/"+snapshot.ID+"/abort", nil)
		server.handleJobs(rec, req)

		var actual map[string]any
		if err := json.NewDecoder(rec.Body).Decode(&actual); err != nil {
			t.Fatalf("Decode: %v", err)
		}
		actual["job_id"] = "__JOB_ID__"

		var expected map[string]any
		loadFixtureJSON(t, "service_contract/job-abort-response.json", &expected)
		if !reflect.DeepEqual(actual, expected) {
			t.Fatalf("job abort response mismatch\nexpected: %#v\ngot: %#v", expected, actual)
		}
	})

	t.Run("health status", func(t *testing.T) {
		server := &serviceServer{jobs: jobs.NewRegistry(time.Minute, 32)}
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/internal/v1/health", nil)
		server.handleHealth(rec, req)
		var actual map[string]any
		if err := json.NewDecoder(rec.Body).Decode(&actual); err != nil {
			t.Fatalf("Decode: %v", err)
		}
		if processID, ok := actual["processId"].(float64); !ok || processID <= 0 {
			t.Fatalf("expected health processId, got %#v", actual["processId"])
		}
		if startedAt, ok := actual["startedAt"].(string); !ok || strings.TrimSpace(startedAt) == "" {
			t.Fatalf("expected health startedAt, got %#v", actual["startedAt"])
		}
		actual["processId"] = "__PROCESS_ID__"
		actual["startedAt"] = "__STARTED_AT__"
		var expected map[string]any
		loadFixtureJSON(t, "service_contract/health-response.json", &expected)
		if !reflect.DeepEqual(actual, expected) {
			t.Fatalf("health response mismatch\nexpected: %#v\ngot: %#v", expected, actual)
		}
	})

	t.Run("embeddings status", func(t *testing.T) {
		database, cleanup := openServiceTestDB(t)
		defer cleanup()
		cfg := config.Default()
		cfg.Provider.APIBase = "http://provider.example"
		cfg.Provider.EmbedModel = "text-embedding-3-small"
		server := &serviceServer{config: cfg, database: database, jobs: jobs.NewRegistry(time.Minute, 32)}
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/internal/v1/embeddings/status", nil)
		req = req.WithContext(context.WithValue(req.Context(), serviceAuthContextKey{}, serviceAuthIdentity{Kind: "shared-secret", Actor: "service:shared-secret", Role: "admin"}))
		server.handleEmbeddings(rec, req)
		var actual map[string]any
		if err := json.NewDecoder(rec.Body).Decode(&actual); err != nil {
			t.Fatalf("Decode: %v", err)
		}
		var expected map[string]any
		loadFixtureJSON(t, "service_contract/embeddings-status-response.json", &expected)
		if !reflect.DeepEqual(actual, expected) {
			t.Fatalf("embeddings status response mismatch\nexpected: %#v\ngot: %#v", expected, actual)
		}
	})

	t.Run("audit status", func(t *testing.T) {
		database, cleanup := openServiceTestDB(t)
		defer cleanup()
		audit := &security.AuditLogger{DB: database, Key: []byte(strings.Repeat("a", 32)), Strict: true}
		if err := audit.Record(context.Background(), "tool.execute", "sess-1", "cli", map[string]any{"tool": "exec"}); err != nil {
			t.Fatalf("Record: %v", err)
		}
		cfg := config.Default()
		cfg.Security.Audit.Enabled = true
		server := &serviceServer{config: cfg, database: database, audit: audit, jobs: jobs.NewRegistry(time.Minute, 32)}
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/internal/v1/audit", nil)
		req = req.WithContext(context.WithValue(req.Context(), serviceAuthContextKey{}, serviceAuthIdentity{Kind: "shared-secret", Actor: "service:shared-secret", Role: "admin"}))
		server.handleAudit(rec, req)
		var actual map[string]any
		if err := json.NewDecoder(rec.Body).Decode(&actual); err != nil {
			t.Fatalf("Decode: %v", err)
		}
		actual["lastEventAt"] = "__LAST_EVENT_AT__"
		var expected map[string]any
		loadFixtureJSON(t, "service_contract/audit-status-response.json", &expected)
		if !reflect.DeepEqual(actual, expected) {
			t.Fatalf("audit status response mismatch\nexpected: %#v\ngot: %#v", expected, actual)
		}
	})
}

func TestServiceRouteContracts_CurrentAppUsageRoutesStayRegistered(t *testing.T) {
	var fixtures []serviceAppUsageRouteFixture
	loadFixtureJSON(t, "service_contract/app-usage-routes.json", &fixtures)
	if len(fixtures) == 0 {
		t.Fatal("expected app usage route fixtures")
	}
	routes := serviceRouteSpecs(&serviceServer{})
	for _, fixture := range fixtures {
		t.Run(fixture.Area+" "+fixture.Method+" "+fixture.Path, func(t *testing.T) {
			if strings.TrimSpace(fixture.Method) == "" || strings.TrimSpace(fixture.Path) == "" {
				t.Fatalf("fixture must include method and path: %#v", fixture)
			}
			if !serviceRouteFixtureIsRegistered(routes, fixture.Path) {
				t.Fatalf("expected app route %s %s to be registered", fixture.Method, fixture.Path)
			}
		})
	}
}

func TestServiceRouteContracts_UnknownInternalRouteReturnsStructuredJSON(t *testing.T) {
	secret := strings.Repeat("r", 32)
	server := &serviceServer{config: config.Default(), jobs: jobs.NewRegistry(time.Minute, 32)}
	server.config.Service.SharedSecretRole = "operator"
	httpServer := newServiceTestHTTPServer(t, secret, server)
	defer httpServer.Close()

	req := mustServiceRequest(t, httpServer, secret, http.MethodGet, "/internal/v1/not-a-real-route", "")
	req.Header.Set("X-Request-Id", "req-unknown-route")
	resp, err := httpServer.Client().Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d (%s)", resp.StatusCode, mustReadBody(t, resp.Body))
	}
	payload := mustDecodeJSONBody(t, resp.Body)
	for _, key := range []string{"error", "code", "request_id"} {
		if strings.TrimSpace(stringValue(payload[key])) == "" {
			t.Fatalf("expected %s in unknown route error payload, got %#v", key, payload)
		}
	}
	if payload["request_id"] != "req-unknown-route" {
		t.Fatalf("expected request id propagation, got %#v", payload)
	}
}

func TestServiceRouteContracts_Non2xxJSONResponsesIncludeErrorCodeAndRequestID(t *testing.T) {
	secret := strings.Repeat("c", 32)
	server := &serviceServer{
		config: config.Default(),
		jobs:   jobs.NewRegistry(time.Minute, 32),
	}
	server.config.Service.SharedSecretRole = "operator"
	httpServer := newServiceTestHTTPServer(t, secret, server)
	defer httpServer.Close()

	tests := []struct {
		name       string
		method     string
		path       string
		body       string
		wantStatus int
	}{
		{name: "jobs route missing", method: http.MethodGet, path: "/internal/v1/jobs", wantStatus: http.StatusNotFound},
		{name: "cron method", method: http.MethodPost, path: "/internal/v1/cron", wantStatus: http.StatusMethodNotAllowed},
		{name: "approvals unavailable", method: http.MethodGet, path: "/internal/v1/approvals", wantStatus: http.StatusServiceUnavailable},
		{name: "configure validation", method: http.MethodGet, path: "/internal/v1/configure/fields", wantStatus: http.StatusBadRequest},
		{name: "files route missing", method: http.MethodGet, path: "/internal/v1/files/unknown", wantStatus: http.StatusNotFound},
		{name: "terminal unavailable", method: http.MethodPost, path: "/internal/v1/terminal/sessions", body: `{"root_id":"workspace","path":"."}`, wantStatus: http.StatusServiceUnavailable},
		{name: "bootstrap method", method: http.MethodPost, path: "/internal/v1/app/bootstrap", wantStatus: http.StatusMethodNotAllowed},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			requestID := "req-contract-" + strings.ReplaceAll(strings.ReplaceAll(tt.name, " ", "-"), "_", "-")
			req := mustServiceRequest(t, httpServer, secret, tt.method, tt.path, tt.body)
			req.Header.Set("X-Request-Id", requestID)

			resp, err := httpServer.Client().Do(req)
			if err != nil {
				t.Fatalf("Do: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != tt.wantStatus {
				t.Fatalf("expected %d, got %d (%s)", tt.wantStatus, resp.StatusCode, mustReadBody(t, resp.Body))
			}
			if resp.StatusCode < 400 {
				t.Fatalf("expected non-2xx response, got %d", resp.StatusCode)
			}

			var payload map[string]any
			if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
				t.Fatalf("Decode: %v", err)
			}
			if strings.TrimSpace(stringValue(payload["error"])) == "" {
				t.Fatalf("expected error field, got %#v", payload)
			}
			if strings.TrimSpace(stringValue(payload["code"])) == "" {
				t.Fatalf("expected code field, got %#v", payload)
			}
			if got := strings.TrimSpace(stringValue(payload["request_id"])); got != requestID {
				t.Fatalf("expected request_id %q, got %#v", requestID, payload)
			}
			if got := strings.TrimSpace(resp.Header.Get("X-Request-Id")); got != requestID {
				t.Fatalf("expected X-Request-Id %q, got %q", requestID, got)
			}
		})
	}
}

func stringValue(value any) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func serviceRouteFixtureIsRegistered(routes []serviceRouteSpec, fixturePath string) bool {
	path := strings.TrimSpace(fixturePath)
	for _, route := range routes {
		if path == route.Path {
			return true
		}
		if route.Subtree && strings.HasPrefix(path, strings.TrimRight(route.Path, "/")+"/") {
			return true
		}
	}
	return false
}

func loadFixtureString(t *testing.T, rel string) string {
	t.Helper()
	path := filepath.Join("testdata", rel)
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", path, err)
	}
	return string(b)
}

func loadFixtureJSON(t *testing.T, rel string, out any) {
	t.Helper()
	path := filepath.Join("testdata", rel)
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", path, err)
	}
	if err := json.Unmarshal(b, out); err != nil {
		t.Fatalf("json.Unmarshal(%s): %v", path, err)
	}
}

func loadFixtureJSONLines(t *testing.T, rel string) []map[string]any {
	t.Helper()
	path := filepath.Join("testdata", rel)
	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("Open(%s): %v", path, err)
	}
	defer file.Close()

	var out []map[string]any
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var entry map[string]any
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("json.Unmarshal line in %s: %v", path, err)
		}
		out = append(out, entry)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("Scanner(%s): %v", path, err)
	}
	return out
}

func sseBodyToJSONLines(t *testing.T, body string, jobID string) []map[string]any {
	t.Helper()
	frames := strings.Split(strings.TrimSpace(body), "\n\n")
	out := make([]map[string]any, 0, len(frames))
	for _, frame := range frames {
		frame = strings.TrimSpace(frame)
		if frame == "" {
			continue
		}
		var eventType string
		var dataLine string
		for _, line := range strings.Split(frame, "\n") {
			if strings.HasPrefix(line, "event: ") {
				eventType = strings.TrimPrefix(line, "event: ")
			}
			if strings.HasPrefix(line, "data: ") {
				dataLine = strings.TrimPrefix(line, "data: ")
			}
		}
		var data map[string]any
		if err := json.Unmarshal([]byte(strings.ReplaceAll(dataLine, jobID, "__JOB_ID__")), &data); err != nil {
			t.Fatalf("unmarshal SSE data: %v", err)
		}
		out = append(out, map[string]any{
			"event": eventType,
			"data":  data,
		})
	}
	return out
}
