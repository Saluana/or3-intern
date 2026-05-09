package cronrunner

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"or3-intern/internal/agentcli"
	"or3-intern/internal/bus"
	"or3-intern/internal/cron"
	"or3-intern/internal/db"
)

type fakeAgentCLIEnqueuer struct {
	req agentcli.AgentRunRequest
	run db.AgentCLIRun
	err error
}

func (f *fakeAgentCLIEnqueuer) Enqueue(ctx context.Context, req agentcli.AgentRunRequest) (db.AgentCLIRun, error) {
	f.req = req
	if f.err != nil {
		return db.AgentCLIRun{}, f.err
	}
	return f.run, nil
}

func TestDispatcherPublishesLegacyCronEvent(t *testing.T) {
	b := bus.New(1)
	events, unsubscribe := b.Subscribe()
	defer unsubscribe()
	runner := New(b, "default-session", nil)

	_, err := runner(context.Background(), cron.CronJob{
		ID:   "legacy",
		Name: "Legacy",
		Payload: cron.CronPayload{
			Kind:    cron.PayloadAgentTurn,
			Message: "run this",
		},
	})
	if err != nil {
		t.Fatalf("runner: %v", err)
	}

	select {
	case ev := <-events:
		if ev.Type != bus.EventCron {
			t.Fatalf("expected cron event, got %s", ev.Type)
		}
		if ev.SessionKey != "default-session" {
			t.Fatalf("expected default session, got %q", ev.SessionKey)
		}
		if ev.Message != "run this" {
			t.Fatalf("expected message, got %q", ev.Message)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timeout waiting for cron event")
	}
}

func TestDispatcherEnqueuesAgentCLIRun(t *testing.T) {
	enqueuer := &fakeAgentCLIEnqueuer{
		run: db.AgentCLIRun{ID: "acr_123", JobID: "job-agentcli-123"},
	}
	runner := New(bus.New(1), "default-session", enqueuer)

	result, err := runner(context.Background(), cron.CronJob{
		ID: "agent-cron",
		Payload: cron.NormalizePayload(cron.CronPayload{
			Kind:       cron.PayloadAgentCLIRun,
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
	if result.EnqueuedJobID != "job-agentcli-123" || result.EnqueuedRunID != "acr_123" {
		t.Fatalf("unexpected result: %#v", result)
	}
	if enqueuer.req.ParentSessionKey != "cron:custom" {
		t.Fatalf("expected parent session, got %q", enqueuer.req.ParentSessionKey)
	}
	if enqueuer.req.RunnerID != "codex" || enqueuer.req.Task != "review repo" {
		t.Fatalf("unexpected request: %#v", enqueuer.req)
	}
	if enqueuer.req.Mode != cron.DefaultAgentCLICronMode {
		t.Fatalf("expected default mode, got %q", enqueuer.req.Mode)
	}
	if enqueuer.req.Isolation != cron.DefaultAgentCLICronIsolation {
		t.Fatalf("expected default isolation, got %q", enqueuer.req.Isolation)
	}
}

func TestDispatcherAgentCLIRunUnavailable(t *testing.T) {
	runner := New(bus.New(1), "default-session", nil)
	_, err := runner(context.Background(), cron.CronJob{
		Payload: cron.CronPayload{
			Kind:     cron.PayloadAgentCLIRun,
			AgentRun: &cron.CronAgentRunPayload{RunnerID: "codex", Task: "review"},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "agent CLI manager") {
		t.Fatalf("expected agent CLI manager error, got %v", err)
	}
}

func TestDispatcherPropagatesAgentCLIEnqueueError(t *testing.T) {
	runner := New(bus.New(1), "default-session", &fakeAgentCLIEnqueuer{err: errors.New("agent CLI delegation is disabled")})
	_, err := runner(context.Background(), cron.CronJob{
		Payload: cron.CronPayload{
			Kind:     cron.PayloadAgentCLIRun,
			AgentRun: &cron.CronAgentRunPayload{RunnerID: "codex", Task: "review"},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "disabled") {
		t.Fatalf("expected disabled error, got %v", err)
	}
}
