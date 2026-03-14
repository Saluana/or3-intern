package tools

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type fakeSpawnManager struct {
	req SpawnRequest
	job SpawnJob
	err error
}

func (f *fakeSpawnManager) Enqueue(ctx context.Context, req SpawnRequest) (SpawnJob, error) {
	f.req = req
	if f.err != nil {
		return SpawnJob{}, f.err
	}
	if f.job.ID == "" {
		f.job = SpawnJob{ID: "job-123", ChildSessionKey: req.ParentSessionKey + ":subagent:job-123"}
	}
	return f.job, nil
}

func TestSpawnSubagent_ExecuteSuccessUsesContextDefaults(t *testing.T) {
	mgr := &fakeSpawnManager{}
	tool := &SpawnSubagent{Manager: mgr}
	ctx := ContextWithSession(context.Background(), "sess-1")
	ctx = ContextWithDelivery(ctx, "cli", "user")
	out, err := tool.Execute(ctx, map[string]any{"task": "investigate"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "job-123") {
		t.Fatalf("expected output to include job id, got %q", out)
	}
	if mgr.req.ParentSessionKey != "sess-1" || mgr.req.Channel != "cli" || mgr.req.To != "user" || mgr.req.Task != "investigate" {
		t.Fatalf("unexpected enqueue request: %#v", mgr.req)
	}
}

func TestSpawnSubagent_ExecuteEmptyTask(t *testing.T) {
	tool := &SpawnSubagent{Manager: &fakeSpawnManager{}}
	_, err := tool.Execute(context.Background(), map[string]any{"task": "   "})
	if err == nil {
		t.Fatal("expected empty task error")
	}
}

func TestSpawnSubagent_DisabledWithoutManager(t *testing.T) {
	tool := &SpawnSubagent{}
	_, err := tool.Execute(context.Background(), map[string]any{"task": "work"})
	if err == nil {
		t.Fatal("expected disabled error")
	}
}

func TestSpawnSubagent_ExecuteManagerError(t *testing.T) {
	tool := &SpawnSubagent{Manager: &fakeSpawnManager{err: errors.New("queue full")}}
	_, err := tool.Execute(context.Background(), map[string]any{"task": "work"})
	if err == nil || !strings.Contains(err.Error(), "queue full") {
		t.Fatalf("expected propagated queue error, got %v", err)
	}
}
