package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"or3-intern/internal/agent"
	"or3-intern/internal/tools"
)

type serviceTurnRequestFixture struct {
	SessionKey    string         `json:"session_key"`
	Message       string         `json:"message"`
	AllowedTools  []string       `json:"allowed_tools"`
	RestrictTools bool           `json:"restrict_tools"`
	Meta          map[string]any `json:"meta"`
	ProfileName   string         `json:"profile_name"`
}

type serviceSubagentRequestFixture struct {
	ParentSessionKey string         `json:"parent_session_key"`
	Task             string         `json:"task"`
	AllowedTools     []string       `json:"allowed_tools"`
	RestrictTools    bool           `json:"restrict_tools"`
	TimeoutSeconds   int            `json:"timeout_seconds"`
	Meta             map[string]any `json:"meta"`
	ProfileName      string         `json:"profile_name"`
	Channel          string         `json:"channel"`
	ReplyTo          string         `json:"reply_to"`
}

func TestOr3NetCompatibilityFixtures_RequestDecoding(t *testing.T) {
	registry := tools.NewRegistry()
	registry.Register(serviceTestTool{name: "read_file"})
	registry.Register(serviceTestTool{name: "write_file"})

	t.Run("turn request", func(t *testing.T) {
		var expected serviceTurnRequestFixture
		loadFixtureJSON(t, "service_contract/turn-request.decoded.json", &expected)
		body := loadFixtureString(t, "service_contract/turn-request.json")
		actual, err := decodeServiceTurnRequest(strings.NewReader(body), registry)
		if err != nil {
			t.Fatalf("decodeServiceTurnRequest: %v", err)
		}
		got := serviceTurnRequestFixture{
			SessionKey:    actual.SessionKey,
			Message:       actual.Message,
			AllowedTools:  actual.AllowedTools,
			RestrictTools: actual.RestrictTools,
			Meta:          actual.Meta,
			ProfileName:   actual.ProfileName,
		}
		if !reflect.DeepEqual(got, expected) {
			t.Fatalf("decoded turn request mismatch\nexpected: %#v\ngot: %#v", expected, got)
		}
	})

	t.Run("subagent request", func(t *testing.T) {
		var expected serviceSubagentRequestFixture
		loadFixtureJSON(t, "service_contract/subagent-request.decoded.json", &expected)
		body := loadFixtureString(t, "service_contract/subagent-request.json")
		actual, err := decodeServiceSubagentRequest(strings.NewReader(body), registry)
		if err != nil {
			t.Fatalf("decodeServiceSubagentRequest: %v", err)
		}
		got := serviceSubagentRequestFixture{
			ParentSessionKey: actual.ParentSessionKey,
			Task:             actual.Task,
			AllowedTools:     actual.AllowedTools,
			RestrictTools:    actual.RestrictTools,
			TimeoutSeconds:   actual.TimeoutSeconds,
			Meta:             actual.Meta,
			ProfileName:      actual.ProfileName,
			Channel:          actual.Channel,
			ReplyTo:          actual.ReplyTo,
		}
		if !reflect.DeepEqual(got, expected) {
			t.Fatalf("decoded subagent request mismatch\nexpected: %#v\ngot: %#v", expected, got)
		}
	})
}

func TestOr3NetCompatibilityFixtures_Responses(t *testing.T) {
	t.Run("turn response", func(t *testing.T) {
		rt, cleanup := buildServiceTestRuntime(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte("data: {\"id\":\"1\",\"choices\":[{\"delta\":{\"content\":\"Hello fixture\"},\"finish_reason\":\"stop\"}]}\n"))
			_, _ = w.Write([]byte("data: [DONE]\n"))
		})
		defer cleanup()
		server := &serviceServer{runtime: rt, jobs: agent.NewJobRegistry(time.Minute, 32)}
		httpServer := newServiceTestHTTPServer(t, strings.Repeat("t", 32), server)
		defer httpServer.Close()

		req := mustServiceRequest(t, httpServer, strings.Repeat("t", 32), http.MethodPost, "/internal/v1/turns", `{"session_key":"svc:fixture","message":"hello"}`)
		resp, err := httpServer.Client().Do(req)
		if err != nil {
			t.Fatalf("Do: %v", err)
		}
		defer resp.Body.Close()
		var actual map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&actual); err != nil {
			t.Fatalf("Decode: %v", err)
		}
		actual["job_id"] = "__JOB_ID__"

		var expected map[string]any
		loadFixtureJSON(t, "service_contract/turn-response.json", &expected)
		if !reflect.DeepEqual(actual, expected) {
			t.Fatalf("turn response mismatch\nexpected: %#v\ngot: %#v", expected, actual)
		}
	})

	t.Run("subagent response", func(t *testing.T) {
		database, cleanup := openServiceTestDB(t)
		defer cleanup()
		jobs := agent.NewJobRegistry(time.Minute, 32)
		manager := &agent.SubagentManager{DB: database, Jobs: jobs, MaxQueued: 4}
		server := &serviceServer{subagentManager: manager, jobs: jobs}
		httpServer := newServiceTestHTTPServer(t, strings.Repeat("u", 32), server)
		defer httpServer.Close()

		body := loadFixtureString(t, "service_contract/subagent-request.json")
		req := mustServiceRequest(t, httpServer, strings.Repeat("u", 32), http.MethodPost, "/internal/v1/subagents", body)
		resp, err := httpServer.Client().Do(req)
		if err != nil {
			t.Fatalf("Do: %v", err)
		}
		defer resp.Body.Close()
		var actual map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&actual); err != nil {
			t.Fatalf("Decode: %v", err)
		}
		actual["job_id"] = "__JOB_ID__"
		actual["child_session_key"] = "__CHILD_SESSION_KEY__"

		var expected map[string]any
		loadFixtureJSON(t, "service_contract/subagent-response.json", &expected)
		if !reflect.DeepEqual(actual, expected) {
			t.Fatalf("subagent response mismatch\nexpected: %#v\ngot: %#v", expected, actual)
		}
	})

	t.Run("job stream attach", func(t *testing.T) {
		jobs := agent.NewJobRegistry(time.Minute, 32)
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
	})

	t.Run("job abort", func(t *testing.T) {
		jobs := agent.NewJobRegistry(time.Minute, 32)
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
