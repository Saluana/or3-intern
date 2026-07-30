package main

import (
	"strings"
	"testing"
)

func TestDecodeServiceRunnerRunRequest_Valid(t *testing.T) {
	body := `{
		"parent_session_key": "session-1",
		"runner_id": "codex",
		"task": "fix the tests",
		"timeout_seconds": 60,
		"cwd": "/workspace",
		"model": "gpt-5",
		"mode": "safe_edit",
		"isolation": "host_workspace_write",
		"max_turns": 5
	}`
	req, err := decodeServiceRunnerRunRequest(strings.NewReader(body))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if req.ParentSessionKey != "session-1" {
		t.Errorf("expected session-1, got %q", req.ParentSessionKey)
	}
	if req.RunnerID != "codex" {
		t.Errorf("expected codex, got %q", req.RunnerID)
	}
	if req.Task != "fix the tests" {
		t.Errorf("expected 'fix the tests', got %q", req.Task)
	}
	if req.TimeoutSeconds != 60 {
		t.Errorf("expected 60, got %d", req.TimeoutSeconds)
	}
	if req.Cwd != "/workspace" {
		t.Errorf("expected /workspace, got %q", req.Cwd)
	}
	if req.Model != "gpt-5" {
		t.Errorf("expected gpt-5, got %q", req.Model)
	}
	if req.Mode != "safe_edit" {
		t.Errorf("expected safe_edit, got %q", req.Mode)
	}
	if req.Isolation != "host_workspace_write" {
		t.Errorf("expected host_workspace_write, got %q", req.Isolation)
	}
	if req.MaxTurns != 5 {
		t.Errorf("expected 5, got %d", req.MaxTurns)
	}
}

func TestDecodeServiceRunnerRunRequest_Minimal(t *testing.T) {
	body := `{
		"parent_session_key": "session-1",
		"runner_id": "opencode",
		"task": "hello"
	}`
	req, err := decodeServiceRunnerRunRequest(strings.NewReader(body))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if req.TimeoutSeconds != 0 {
		t.Errorf("expected 0 default, got %d", req.TimeoutSeconds)
	}
	if req.Mode != "" {
		t.Errorf("expected empty mode, got %q", req.Mode)
	}
}

func TestDecodeServiceRunnerRunRequest_RejectsCamelCaseAliases(t *testing.T) {
	body := `{
		"parentSessionKey": "session-camel",
		"runnerId": "codex",
		"task": "fix the tests",
		"timeoutSeconds": 30,
		"maxTurns": 4
	}`
	_, err := decodeServiceRunnerRunRequest(strings.NewReader(body))
	if err == nil {
		t.Fatal("expected error for camelCase aliases")
	}
}

func TestServiceLifecyclePayloadIncludesTraceID(t *testing.T) {
	payload := serviceLifecyclePayload("session-1", map[string]any{
		"trace_id":           "turn_trace",
		"request_id":         "req_trace",
		"network_session_id": "net_trace",
	}, map[string]any{"status": "running"})

	if payload["trace_id"] != "turn_trace" {
		t.Fatalf("expected trace_id to be carried in lifecycle payload, got %#v", payload)
	}
	if payload["request_id"] != "req_trace" || payload["network_session_id"] != "net_trace" {
		t.Fatalf("expected audit identifiers to remain present, got %#v", payload)
	}
}

func TestDecodeServiceRunnerRunRequest_RejectsUnknownFields(t *testing.T) {
	body := `{
		"parent_session_key": "session-1",
		"runner_id": "opencode",
		"task": "hello",
		"unknown_field": "should not be here"
	}`
	_, err := decodeServiceRunnerRunRequest(strings.NewReader(body))
	if err == nil {
		t.Fatal("expected error for unknown field")
	}
}

func TestDecodeServiceRunnerRunRequest_RequiresParentSessionKey(t *testing.T) {
	body := `{
		"runner_id": "opencode",
		"task": "hello"
	}`
	_, err := decodeServiceRunnerRunRequest(strings.NewReader(body))
	if err == nil {
		t.Fatal("expected error for missing parent_session_key")
	}
	if !strings.Contains(err.Error(), "parent_session_key") {
		t.Errorf("expected parent_session_key error, got %v", err)
	}
}

func TestDecodeServiceRunnerRunRequest_RequiresRunnerID(t *testing.T) {
	body := `{
		"parent_session_key": "session-1",
		"task": "hello"
	}`
	_, err := decodeServiceRunnerRunRequest(strings.NewReader(body))
	if err == nil {
		t.Fatal("expected error for missing runner_id")
	}
	if !strings.Contains(err.Error(), "runner_id") {
		t.Errorf("expected runner_id error, got %v", err)
	}
}

func TestDecodeServiceRunnerRunRequest_RequiresTask(t *testing.T) {
	body := `{
		"parent_session_key": "session-1",
		"runner_id": "opencode"
	}`
	_, err := decodeServiceRunnerRunRequest(strings.NewReader(body))
	if err == nil {
		t.Fatal("expected error for missing task")
	}
	if !strings.Contains(err.Error(), "task") {
		t.Errorf("expected task error, got %v", err)
	}
}

func TestDecodeServiceRunnerRunRequest_InvalidTimeout(t *testing.T) {
	body := `{
		"parent_session_key": "session-1",
		"runner_id": "opencode",
		"task": "hello",
		"timeout_seconds": "not_a_number"
	}`
	_, err := decodeServiceRunnerRunRequest(strings.NewReader(body))
	if err == nil {
		t.Fatal("expected error for invalid timeout_seconds")
	}
}

func TestDecodeServiceRunnerRunRequest_InvalidMaxTurns(t *testing.T) {
	body := `{
		"parent_session_key": "session-1",
		"runner_id": "opencode",
		"task": "hello",
		"max_turns": "not_a_number"
	}`
	_, err := decodeServiceRunnerRunRequest(strings.NewReader(body))
	if err == nil {
		t.Fatal("expected error for invalid max_turns")
	}
}

func TestDecodeServiceRunnerRunRequest_TrailingData(t *testing.T) {
	body := `{"parent_session_key": "session-1", "runner_id": "opencode", "task": "hello"} extra`
	_, err := decodeServiceRunnerRunRequest(strings.NewReader(body))
	if err == nil {
		t.Fatal("expected error for trailing data")
	}
}
