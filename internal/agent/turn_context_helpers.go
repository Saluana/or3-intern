package agent

import "context"

func TurnStateFromContextOrDefault(ctx context.Context, sessionKey string) TurnState {
	if state, ok := TurnStateFromContext(ctx); ok {
		return state
	}
	return TurnState{SessionKey: sessionKey}
}
