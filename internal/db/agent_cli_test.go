package db

import (
	"context"
	"testing"
)

func TestListAgentCLIRuns_ReturnsRecentRuns(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()
	runs := []AgentCLIRun{
		{
			ID:               "run-old",
			JobID:            "job-old",
			ParentSessionKey: "sess-a",
			RunnerID:         "opencode",
			Task:             "old task",
			Status:           AgentCLIStatusQueued,
			RequestedAt:      1000,
		},
		{
			ID:               "run-new",
			JobID:            "job-new",
			ParentSessionKey: "sess-a",
			RunnerID:         "opencode",
			Task:             "new task",
			Status:           AgentCLIStatusSucceeded,
			RequestedAt:      2000,
			CompletedAt:      3000,
		},
	}
	for _, run := range runs {
		if err := d.EnqueueAgentCLIRun(ctx, run); err != nil {
			t.Fatalf("EnqueueAgentCLIRun(%s): %v", run.ID, err)
		}
	}

	got, err := d.ListAgentCLIRuns(ctx, AgentCLIRunFilter{ParentSessionKey: "sess-a", Limit: 10})
	if err != nil {
		t.Fatalf("ListAgentCLIRuns: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 runs, got %d", len(got))
	}
	if got[0].ID != "run-new" || got[1].ID != "run-old" {
		t.Fatalf("expected newest first, got %#v", got)
	}

	queued, err := d.ListAgentCLIRuns(ctx, AgentCLIRunFilter{Status: AgentCLIStatusQueued})
	if err != nil {
		t.Fatalf("ListAgentCLIRuns queued: %v", err)
	}
	if len(queued) != 1 || queued[0].ID != "run-old" {
		t.Fatalf("expected only queued run-old, got %#v", queued)
	}
}
