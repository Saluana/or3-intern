package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"or3-intern/internal/approval"
	"or3-intern/internal/config"
	"or3-intern/internal/scope"
	"or3-intern/internal/tools"
)

type messageQuotaCountersContextKey struct{}

type sessionQuotaState struct {
	Session  quotaCounters
	LastSeen time.Time
}

type quotaCounters struct {
	ToolCalls     int
	ExecCalls     int
	WebCalls      int
	SubagentCalls int
}

type quotaCheck struct {
	Scope     string
	Name      string
	Label     string
	ConfigKey string
	Current   int
	Limit     int
}

func (r *Runtime) incrementQuota(ctx context.Context, sessionKey string, toolName string) error {
	r.quotaMu.Lock()
	state := r.sessionQuotaStateLocked(sessionKey)
	message := messageQuotaCountersFromContext(ctx)
	checks := r.quotaChecks(message, &state.Session, toolName)
	for _, check := range checks {
		if check.Limit > 0 && check.Current >= check.Limit {
			r.quotaMu.Unlock()
			if err := r.handleQuotaExceeded(ctx, sessionKey, toolName, check); err != nil {
				return err
			}
			r.quotaMu.Lock()
			state = r.sessionQuotaStateLocked(sessionKey)
			message = messageQuotaCountersFromContext(ctx)
			incrementQuotaCounters(message, toolName)
			incrementQuotaCounters(&state.Session, toolName)
			r.quotaMu.Unlock()
			return nil
		}
	}
	incrementQuotaCounters(message, toolName)
	incrementQuotaCounters(&state.Session, toolName)
	r.quotaMu.Unlock()
	return nil
}

func (r *Runtime) quotaChecks(message *quotaCounters, session *quotaCounters, toolName string) []quotaCheck {
	cfg := r.Hardening.Quotas
	checks := []quotaCheck{
		{Scope: "message", Name: "tool_calls", Label: "per-message total tool-call", ConfigKey: "hardening.quotas.maxToolCalls", Current: message.ToolCalls, Limit: cfg.MaxToolCalls},
		{Scope: "session", Name: "tool_calls", Label: "per-session total tool-call", ConfigKey: "hardening.quotas.maxSessionToolCalls", Current: session.ToolCalls, Limit: cfg.MaxSessionToolCalls},
	}
	switch toolName {
	case "exec", "run_skill", "run_skill_script":
		checks = append(checks,
			quotaCheck{Scope: "message", Name: "exec_calls", Label: "per-message exec-call", ConfigKey: "hardening.quotas.maxExecCalls", Current: message.ExecCalls, Limit: cfg.MaxExecCalls},
			quotaCheck{Scope: "session", Name: "exec_calls", Label: "per-session exec-call", ConfigKey: "hardening.quotas.maxSessionExecCalls", Current: session.ExecCalls, Limit: cfg.MaxSessionExecCalls},
		)
	case "web_fetch", "web_fetch_markdown", "web_search":
		checks = append(checks,
			quotaCheck{Scope: "message", Name: "web_calls", Label: "per-message web-call", ConfigKey: "hardening.quotas.maxWebCalls", Current: message.WebCalls, Limit: cfg.MaxWebCalls},
			quotaCheck{Scope: "session", Name: "web_calls", Label: "per-session web-call", ConfigKey: "hardening.quotas.maxSessionWebCalls", Current: session.WebCalls, Limit: cfg.MaxSessionWebCalls},
		)
	case "spawn_subagent":
		checks = append(checks,
			quotaCheck{Scope: "message", Name: "subagent_calls", Label: "per-message subagent-call", ConfigKey: "hardening.quotas.maxSubagentCalls", Current: message.SubagentCalls, Limit: cfg.MaxSubagentCalls},
			quotaCheck{Scope: "session", Name: "subagent_calls", Label: "per-session subagent-call", ConfigKey: "hardening.quotas.maxSessionSubagentCalls", Current: session.SubagentCalls, Limit: cfg.MaxSessionSubagentCalls},
		)
	}
	return checks
}

func (r *Runtime) handleQuotaExceeded(ctx context.Context, sessionKey string, toolName string, check quotaCheck) error {
	if r.Hardening.Quotas.ExceededAction == config.QuotaExceededActionFail {
		return quotaExceededError(toolName, check, "hard limit reached")
	}
	if r.ApprovalBroker == nil {
		return quotaExceededError(toolName, check, "approval is configured, but the approval broker is unavailable")
	}
	identity := tools.RequesterIdentityFromContext(ctx)
	decision, err := r.ApprovalBroker.EvaluateToolQuota(ctx, approval.ToolQuotaEvaluation{
		Scope:         check.Scope,
		LimitName:     check.Name,
		ToolName:      toolName,
		Current:       check.Current,
		Limit:         check.Limit,
		AgentID:       firstNonEmptyString(identity.Actor, "runtime"),
		SessionID:     sessionKey,
		ApprovalToken: tools.ApprovalTokenFromContext(ctx),
	}, config.ApprovalModeAsk)
	if err != nil {
		return err
	}
	if decision.Allowed {
		if r.Audit != nil {
			_ = r.Audit.Record(ctx, "quota.override", sessionKey, "approval", map[string]any{
				"tool":         toolName,
				"scope":        check.Scope,
				"limit":        check.Name,
				"current":      check.Current,
				"max":          check.Limit,
				"subject_hash": decision.SubjectHash,
			})
		}
		return nil
	}
	if decision.RequiresApproval {
		return &tools.ApprovalRequiredError{ToolName: toolName, RequestID: decision.RequestID}
	}
	return quotaExceededError(toolName, check, decision.Reason)
}

func messageQuotaCountersFromContext(ctx context.Context) *quotaCounters {
	if ctx != nil {
		if counters, ok := ctx.Value(messageQuotaCountersContextKey{}).(*quotaCounters); ok && counters != nil {
			return counters
		}
	}
	return &quotaCounters{}
}

func incrementQuotaCounters(counters *quotaCounters, toolName string) {
	if counters == nil {
		return
	}
	counters.ToolCalls++
	switch toolName {
	case "exec", "run_skill", "run_skill_script":
		counters.ExecCalls++
	case "web_fetch", "web_fetch_markdown", "web_search":
		counters.WebCalls++
	case "spawn_subagent":
		counters.SubagentCalls++
	}
}

func quotaExceededError(toolName string, check quotaCheck, reason string) error {
	return errors.New(quotaExceededMessage(toolName, check, reason))
}

func quotaExceededMessage(toolName string, check quotaCheck, reason string) string {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "limit reached"
	}
	return fmt.Sprintf("tool quota reached for %s: %s limit %d/%d while executing %s (%s). Increase %s or set hardening.quotas.exceededAction to ask/fail as appropriate.",
		check.Scope,
		check.Label,
		check.Current,
		check.Limit,
		toolName,
		reason,
		check.ConfigKey,
	)
}

func (r *Runtime) sessionQuotaState(sessionKey string) *sessionQuotaState {
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" {
		sessionKey = scope.GlobalMemoryScope
	}
	r.quotaMu.Lock()
	defer r.quotaMu.Unlock()
	return r.sessionQuotaStateLocked(sessionKey)
}

func (r *Runtime) sessionQuotaStateLocked(sessionKey string) *sessionQuotaState {
	if r.quotas == nil {
		r.quotas = map[string]*sessionQuotaState{}
	}
	r.evictQuotaStateLocked()
	state := r.quotas[sessionKey]
	if state == nil {
		state = &sessionQuotaState{}
		r.quotas[sessionKey] = state
	}
	state.LastSeen = time.Now()
	return state
}

func (r *Runtime) evictQuotaStateLocked() {
	if len(r.quotas) < maxTrackedQuotaSessions {
		return
	}
	oldestKey := ""
	var oldestTime time.Time
	for key, state := range r.quotas {
		if state == nil {
			delete(r.quotas, key)
			continue
		}
		if oldestKey == "" || state.LastSeen.Before(oldestTime) {
			oldestKey = key
			oldestTime = state.LastSeen
		}
	}
	if oldestKey != "" {
		delete(r.quotas, oldestKey)
	}
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
