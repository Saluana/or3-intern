package integrations

import "strings"

type State string

const (
	StateOff               State = "off"
	StateNeedsSetup        State = "needs setup"
	StateConnected         State = "connected"
	StateDegraded          State = "degraded"
	StateDisabledForSafety State = "disabled for safety"
)

func Label(state State) string {
	switch state {
	case StateOff, StateNeedsSetup, StateConnected, StateDegraded, StateDisabledForSafety:
		return string(state)
	default:
		return string(StateNeedsSetup)
	}
}

func FromStatus(enabled bool, connected bool, lastError string) State {
	if !enabled {
		return StateOff
	}
	if connected {
		return StateConnected
	}
	if strings.TrimSpace(lastError) != "" {
		return StateDegraded
	}
	return StateNeedsSetup
}
