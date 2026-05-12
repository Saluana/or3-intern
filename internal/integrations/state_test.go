package integrations

import "testing"

func TestFromStatusLabels(t *testing.T) {
	tests := []struct {
		name      string
		enabled   bool
		connected bool
		err       string
		want      State
	}{
		{name: "off", enabled: false, want: StateOff},
		{name: "needs setup", enabled: true, want: StateNeedsSetup},
		{name: "connected", enabled: true, connected: true, want: StateConnected},
		{name: "degraded", enabled: true, err: "dial failed", want: StateDegraded},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := FromStatus(tc.enabled, tc.connected, tc.err); got != tc.want {
				t.Fatalf("expected %q, got %q", tc.want, got)
			}
		})
	}
}
