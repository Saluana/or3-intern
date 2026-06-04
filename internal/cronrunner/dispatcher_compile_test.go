package cronrunner

import (
	"context"
	"strings"
	"testing"

	"or3-intern/internal/agentcli"
	"or3-intern/internal/app"
	"or3-intern/internal/bus"
	"or3-intern/internal/config"
	"or3-intern/internal/cron"
	"or3-intern/internal/db"
)

type recordingPreparer struct {
	req agentcli.AgentRunRequest
}

func (r *recordingPreparer) PrepareAgentRunRequest(ctx context.Context, req agentcli.AgentRunRequest) agentcli.AgentRunRequest {
	r.req = req
	compiler := app.NewRunnerPromptCompiler(config.Default(), app.RunnerBootstrapContext{Soul: "cron soul"}, app.RunnerContextDeps{})
	return compiler.PrepareAgentRunRequest(ctx, req)
}

func TestDispatcherAgentCLIRunDefaultsToCompiledOR3Context(t *testing.T) {
	enqueuer := &fakeAgentCLIEnqueuer{run: db.AgentCLIRun{ID: "acr_1", JobID: "job_1"}}
	preparer := &recordingPreparer{}
	runner := NewWithPreparer(bus.New(1), "default-session", enqueuer, preparer, true)

	_, err := runner(context.Background(), cron.CronJob{
		ID: "agent-cron",
		Payload: cron.NormalizePayload(cron.CronPayload{
			Kind: cron.PayloadAgentCLIRun,
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
		t.Fatalf("expected compiled OR3 context in cron agent run, got %q", enqueuer.req.Task)
	}
	if !strings.Contains(enqueuer.req.Task, "review repo") {
		t.Fatalf("expected user task preserved: %q", enqueuer.req.Task)
	}
}
