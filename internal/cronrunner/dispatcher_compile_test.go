package cronrunner

import (
	"context"
	"strings"
	"testing"

	"or3-intern/internal/app"
	"or3-intern/internal/bus"
	"or3-intern/internal/config"
	"or3-intern/internal/cron"
	"or3-intern/internal/db"
	"or3-intern/internal/runners"
)

type recordingPreparer struct {
	req runners.RunnerRunRequest
}

func (r *recordingPreparer) PrepareRunnerRunRequest(ctx context.Context, req runners.RunnerRunRequest) runners.RunnerRunRequest {
	r.req = req
	compiler := app.NewRunnerPromptCompiler(config.Default(), app.RunnerBootstrapContext{Soul: "cron soul"}, app.RunnerContextDeps{})
	return compiler.PrepareRunnerRunRequest(ctx, req)
}

func TestDispatcherRunnerRunDefaultsToCompiledOR3Context(t *testing.T) {
	enqueuer := &fakeRunnerRunEnqueuer{run: db.RunnerRun{ID: "rr_1", JobID: "job_1"}}
	preparer := &recordingPreparer{}
	runner := NewWithPreparer(bus.New(1), "default-session", enqueuer, preparer)

	_, err := runner(context.Background(), cron.CronJob{
		ID: "runner-cron",
		Payload: cron.NormalizePayload(cron.CronPayload{
			Kind: cron.PayloadRunnerRun,
			AgentRun: &cron.CronAgentRunPayload{
				RunnerID: "codex",
				Task:     "review repo",
			},
		}),
	})
	if err != nil {
		t.Fatalf("runner: %v", err)
	}
	if !strings.Contains(enqueuer.req.Task, "cron soul") {
		t.Fatalf("expected compiled OR3 context in cron runner run, got %q", enqueuer.req.Task)
	}
	if !strings.Contains(enqueuer.req.Task, "review repo") {
		t.Fatalf("expected user task preserved: %q", enqueuer.req.Task)
	}
}
