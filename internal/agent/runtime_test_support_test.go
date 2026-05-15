package agent

import (
	"context"
	"fmt"
	"strings"

	"or3-intern/internal/bus"
	"or3-intern/internal/cron"
	"or3-intern/internal/providers"
	"or3-intern/internal/tools"
)

func (r *Runtime) getSessionLock(key string) *sessionLock {
	r.locksMu.Lock()
	defer r.locksMu.Unlock()
	if r.locks == nil {
		r.locks = map[string]*sessionLock{}
	}
	entry := r.locks[key]
	if entry == nil {
		entry = &sessionLock{}
		r.locks[key] = entry
	}
	return entry
}

func toToolDefs(reg *tools.Registry) []providers.ToolDef {
	defs, _ := toProviderToolDefs(reg, providers.OpenAICompatibleProfile())
	return defs
}

func CronRunner(b *bus.Bus, defaultSessionKey string) cron.Runner {
	return func(ctx context.Context, job cron.CronJob) (cron.RunResult, error) {
		select {
		case <-ctx.Done():
			return cron.RunResult{}, ctx.Err()
		default:
		}
		if kind := strings.TrimSpace(job.Payload.Kind); kind != "" && kind != cron.PayloadAgentTurn && kind != cron.PayloadSystemEvent {
			return cron.RunResult{}, fmt.Errorf("unsupported cron payload kind for agent runner: %s", kind)
		}
		msg := job.Payload.Message
		if strings.TrimSpace(msg) == "" {
			msg = "cron job: " + job.Name
		}
		sessionKey := job.Payload.SessionKey
		if strings.TrimSpace(sessionKey) == "" {
			sessionKey = defaultSessionKey
		}
		ev := bus.Event{Type: bus.EventCron, SessionKey: sessionKey, Channel: job.Payload.Channel, From: job.Payload.To, Message: msg, Meta: map[string]any{"job_id": job.ID}}
		if ok := b.Publish(ev); !ok {
			return cron.RunResult{}, fmt.Errorf("event bus full")
		}
		return cron.RunResult{}, nil
	}
}
