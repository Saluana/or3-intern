package cronrunner

import (
	"context"
	"fmt"
	"strings"

	"or3-intern/internal/agentcli"
	"or3-intern/internal/bus"
	"or3-intern/internal/cron"
	"or3-intern/internal/db"
)

type AgentCLIEnqueuer interface {
	Enqueue(ctx context.Context, req agentcli.AgentRunRequest) (db.AgentCLIRun, error)
}

type Dispatcher struct {
	Bus               *bus.Bus
	DefaultSessionKey string
	AgentCLI          AgentCLIEnqueuer
}

func New(b *bus.Bus, defaultSessionKey string, agentCLI AgentCLIEnqueuer) cron.Runner {
	if b == nil {
		panic("cronrunner dispatcher event bus not configured")
	}
	d := Dispatcher{Bus: b, DefaultSessionKey: defaultSessionKey, AgentCLI: agentCLI}
	return d.Run
}

func (d Dispatcher) Run(ctx context.Context, job cron.CronJob) (cron.RunResult, error) {
	switch job.Payload.Kind {
	case cron.PayloadAgentTurn, cron.PayloadSystemEvent:
		return d.publishAgentTurn(job, job.Payload)
	case cron.PayloadAgentCLIRun:
		return d.enqueueAgentRun(ctx, job, job.Payload)
	default:
		return cron.RunResult{}, fmt.Errorf("unsupported cron payload kind: %s", job.Payload.Kind)
	}
}

func (d Dispatcher) publishAgentTurn(job cron.CronJob, payload cron.CronPayload) (cron.RunResult, error) {
	msg := payload.Message
	if strings.TrimSpace(msg) == "" {
		msg = "cron job: " + job.Name
	}
	sessionKey := payload.SessionKey
	if strings.TrimSpace(sessionKey) == "" {
		sessionKey = d.DefaultSessionKey
	}
	ev := bus.Event{
		Type:       bus.EventCron,
		SessionKey: sessionKey,
		Channel:    payload.Channel,
		From:       payload.To,
		Message:    msg,
		Meta:       map[string]any{"job_id": job.ID},
	}
	if ok := d.Bus.Publish(ev); !ok {
		return cron.RunResult{}, fmt.Errorf("event bus full")
	}
	return cron.RunResult{}, nil
}

func (d Dispatcher) enqueueAgentRun(ctx context.Context, job cron.CronJob, payload cron.CronPayload) (cron.RunResult, error) {
	if d.AgentCLI == nil {
		return cron.RunResult{}, fmt.Errorf("agent CLI manager is not available for cron job")
	}
	run := payload.AgentRun
	sessionKey := payload.SessionKey
	if strings.TrimSpace(sessionKey) == "" {
		sessionKey = d.DefaultSessionKey
	}
	req := agentcli.AgentRunRequest{
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
	created, err := d.AgentCLI.Enqueue(ctx, req)
	if err != nil {
		return cron.RunResult{}, err
	}
	return cron.RunResult{EnqueuedJobID: created.JobID, EnqueuedRunID: created.ID}, nil
}
