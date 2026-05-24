package agent

import "context"

type turnStateContextKey struct{}

// TurnState tracks per-turn protected context used by plan gating and reminders.
type TurnState struct {
	SessionKey           string
	UserMessageID        int64
	UserMessage          string
	PlanGateReminderSent bool
	ExplorationToolCalls int
}

func ContextWithTurnState(ctx context.Context, state TurnState) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, turnStateContextKey{}, state)
}

func TurnStateFromContext(ctx context.Context) (TurnState, bool) {
	if ctx == nil {
		return TurnState{}, false
	}
	state, ok := ctx.Value(turnStateContextKey{}).(TurnState)
	return state, ok
}

func ContextWithExplorationToolCall(ctx context.Context) context.Context {
	state, ok := TurnStateFromContext(ctx)
	if !ok {
		return ctx
	}
	state.ExplorationToolCalls++
	return ContextWithTurnState(ctx, state)
}

func ContextWithPlanGateReminderSent(ctx context.Context) context.Context {
	state, ok := TurnStateFromContext(ctx)
	if !ok {
		return ctx
	}
	state.PlanGateReminderSent = true
	return ContextWithTurnState(ctx, state)
}
