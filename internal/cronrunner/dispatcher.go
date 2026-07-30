package cronrunner

import (
	"context"
	"fmt"
	"strings"

	"or3-intern/internal/bus"
	"or3-intern/internal/cron"
	"or3-intern/internal/db"
	"or3-intern/internal/runners"
)

type RunnerRunEnqueuer interface {
	Enqueue(ctx context.Context, req runners.RunnerRunRequest) (db.RunnerRun, error)
}

type RunnerRunPreparer interface {
	PrepareRunnerRunRequest(ctx context.Context, req runners.RunnerRunRequest) runners.RunnerRunRequest
}

type Dispatcher struct {
	Bus               *bus.Bus
	DefaultSessionKey string
	Runner            RunnerRunEnqueuer
	Preparer          RunnerRunPreparer
}

func New(b *bus.Bus, defaultSessionKey string, runner RunnerRunEnqueuer) cron.Runner {
	return NewWithPreparer(b, defaultSessionKey, runner, nil)
}

func NewWithPreparer(b *bus.Bus, defaultSessionKey string, runner RunnerRunEnqueuer, preparer RunnerRunPreparer) cron.Runner {
	if b == nil {
		panic("cronrunner dispatcher event bus not configured")
	}
	d := Dispatcher{
		Bus:               b,
		DefaultSessionKey: defaultSessionKey,
		Runner:            runner,
		Preparer:          preparer,
	}
	return d.Run
}

func (d Dispatcher) Run(ctx context.Context, job cron.CronJob) (cron.RunResult, error) {
	switch job.Payload.Kind {
	case cron.PayloadRunnerRun:
		return d.enqueueRunnerRun(ctx, job, job.Payload)
	default:
		return cron.RunResult{}, fmt.Errorf("unsupported cron payload kind: %s", job.Payload.Kind)
	}
}

func (d Dispatcher) enqueueRunnerRun(ctx context.Context, job cron.CronJob, payload cron.CronPayload) (cron.RunResult, error) {
	if d.Runner == nil {
		return cron.RunResult{}, fmt.Errorf("runner manager is not available for cron job")
	}
	run := payload.AgentRun
	sessionKey := payload.SessionKey
	if strings.TrimSpace(sessionKey) == "" {
		sessionKey = d.DefaultSessionKey
	}
	req := runners.RunnerRunRequest{
		ParentSessionKey: sessionKey,
		RunnerID:         run.RunnerID,
		Task:             run.Task,
		TimeoutSeconds:   run.TimeoutSeconds,
		Cwd:              run.Cwd,
		Model:            run.Model,
		Mode:             run.Mode,
		Isolation:        run.Isolation,
		MaxTurns:         run.MaxTurns,
		Meta:             run.Meta,
	}
	if d.Preparer != nil {
		req = d.Preparer.PrepareRunnerRunRequest(ctx, req)
	}
	created, err := d.Runner.Enqueue(ctx, req)
	if err != nil {
		return cron.RunResult{}, err
	}
	return cron.RunResult{EnqueuedJobID: created.JobID, EnqueuedRunID: created.ID}, nil
}
