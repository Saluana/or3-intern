package cronrunner

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"or3-intern/internal/agentcli"
	"or3-intern/internal/bus"
	"or3-intern/internal/cron"
	"or3-intern/internal/db"
)

// ErrLegacyCronAgentTurn is returned when legacy agent_turn cron payloads cannot run.
var ErrLegacyCronAgentTurn = errors.New("legacy cron agent_turn requires agentCLI.enabled; use agent_cli_run for runner jobs")

type AgentCLIEnqueuer interface {
	Enqueue(ctx context.Context, req agentcli.AgentRunRequest) (db.AgentCLIRun, error)
}

type Dispatcher struct {
	Bus               *bus.Bus
	DefaultSessionKey string
	AgentCLI          AgentCLIEnqueuer
	AgentCLIEnabled   bool
}

func New(b *bus.Bus, defaultSessionKey string, agentCLI AgentCLIEnqueuer, agentCLIEnabled bool) cron.Runner {
	if b == nil {
		panic("cronrunner dispatcher event bus not configured")
	}
	d := Dispatcher{Bus: b, DefaultSessionKey: defaultSessionKey, AgentCLI: agentCLI, AgentCLIEnabled: agentCLIEnabled}
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
	if !d.AgentCLIEnabled {
		return cron.RunResult{}, fmt.Errorf("%w: recreate the job as agent_cli_run or enable agentCLI.enabled", ErrLegacyCronAgentTurn)
	}
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
		Meta: map[string]any{
			"job_id":            job.ID,
			"cron_payload_kind": payload.Kind,
			"runner_first":      true,
		},
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
