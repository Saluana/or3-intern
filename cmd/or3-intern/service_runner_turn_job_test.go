package main

import (
	"context"
	"testing"
	"time"

	"or3-intern/internal/app"
	"or3-intern/internal/jobs"
)

func TestCompleteTurnJobFromRunnerWaitsForRunnerJob(t *testing.T) {
	jobs := jobs.NewRegistry(time.Minute, 16)
	server := &serviceServer{jobs: jobs}
	parent := jobs.RegisterWithID("svc-parent", "turn")
	child := jobs.RegisterWithID("runner-child", "agent_cli")

	done := make(chan struct{})
	go func() {
		defer close(done)
		server.completeTurnJobFromRunner(context.Background(), parent.ID, serviceTurnRequest{SessionKey: "svc:test"}, app.RunnerTurnResult{
			RunnerChatSessionID: "rcs_1",
			RunnerChatTurnID:    "rct_1",
			AgentCLIRunID:       "acr_1",
			AgentCLIJobID:       child.ID,
		})
	}()

	select {
	case <-done:
		t.Fatal("service job completed before runner job")
	case <-time.After(25 * time.Millisecond):
	}

	jobs.Complete(child.ID, "completed", map[string]any{"final_text": "runner final"})
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("service job did not complete after runner job")
	}

	snapshot, ok := jobs.Snapshot(parent.ID)
	if !ok {
		t.Fatal("missing parent job")
	}
	if snapshot.Status != "completed" {
		t.Fatalf("expected completed parent job, got %q", snapshot.Status)
	}
	if got := finalTextFromJobSnapshot(snapshot); got != "runner final" {
		t.Fatalf("expected runner final text, got %q", got)
	}
}
