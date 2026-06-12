package cronrunner

import (
	"context"
	"errors"
	"strings"
	"testing"

	"or3-intern/internal/bus"
	"or3-intern/internal/cron"
	"or3-intern/internal/db"
	"or3-intern/internal/runners"
)

type fakeRunnerRunEnqueuer struct {
	req runners.RunnerRunRequest
	run db.RunnerRun
	err error
}

func (f *fakeRunnerRunEnqueuer) Enqueue(ctx context.Context, req runners.RunnerRunRequest) (db.RunnerRun, error) {
	f.req = req
	if f.err != nil {
		return db.RunnerRun{}, f.err
	}
	return f.run, nil
}

func TestDispatcherEnqueuesRunnerRun(t *testing.T) {
	enqueuer := &fakeRunnerRunEnqueuer{
		run: db.RunnerRun{ID: "rr_123", JobID: "job-runner-123"},
	}
	runner := New(bus.New(1), "default-session", enqueuer)

	result, err := runner(context.Background(), cron.CronJob{
		ID: "runner-cron",
		Payload: cron.NormalizePayload(cron.CronPayload{
			Kind:       cron.PayloadRunnerRun,
			SessionKey: "cron:custom",
			AgentRun: &cron.CronAgentRunPayload{
				RunnerID: "codex",
				Task:     "review repo",
				Cwd:      "/workspace",
				Meta:     map[string]any{"source": "test"},
			},
		}),
	})
	if err != nil {
		t.Fatalf("runner: %v", err)
	}
	if result.EnqueuedJobID != "job-runner-123" || result.EnqueuedRunID != "rr_123" {
		t.Fatalf("unexpected result: %#v", result)
	}
	if enqueuer.req.ParentSessionKey != "cron:custom" {
		t.Fatalf("expected parent session, got %q", enqueuer.req.ParentSessionKey)
	}
	if enqueuer.req.RunnerID != "codex" || enqueuer.req.Task != "review repo" {
		t.Fatalf("unexpected request: %#v", enqueuer.req)
	}
	if enqueuer.req.Mode != cron.DefaultRunnerRunCronMode {
		t.Fatalf("expected default mode, got %q", enqueuer.req.Mode)
	}
	if enqueuer.req.Isolation != cron.DefaultRunnerRunCronIsolation {
		t.Fatalf("expected default isolation, got %q", enqueuer.req.Isolation)
	}
}

func TestDispatcherRunnerRunUnavailable(t *testing.T) {
	runner := New(bus.New(1), "default-session", nil)
	_, err := runner(context.Background(), cron.CronJob{
		Payload: cron.CronPayload{
			Kind:     cron.PayloadRunnerRun,
			AgentRun: &cron.CronAgentRunPayload{RunnerID: "codex", Task: "review"},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "runner manager") {
		t.Fatalf("expected runner manager error, got %v", err)
	}
}

func TestDispatcherPropagatesRunnerRunEnqueueError(t *testing.T) {
	runner := New(bus.New(1), "default-session", &fakeRunnerRunEnqueuer{err: errors.New("runner delegation is disabled")})
	_, err := runner(context.Background(), cron.CronJob{
		Payload: cron.CronPayload{
			Kind:     cron.PayloadRunnerRun,
			AgentRun: &cron.CronAgentRunPayload{RunnerID: "codex", Task: "review"},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "disabled") {
		t.Fatalf("expected disabled error, got %v", err)
	}
}
