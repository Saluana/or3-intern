package agentcli

import (
	"strings"
	"testing"
)

func TestValidateDoctorAgentRunRequestRejectsUnsafeModes(t *testing.T) {
	err := ValidateDoctorAgentRunRequest(AgentRunRequest{
		Meta:      map[string]any{"doctor_session": true},
		Mode:      string(RunnerModeSafeEdit),
		Isolation: string(IsolationHostReadOnly),
		MaxTurns:  2,
	})
	if err == nil || !strings.Contains(err.Error(), "review mode") {
		t.Fatalf("ValidateDoctorAgentRunRequest() = %v, want review mode error", err)
	}
}

func TestValidateDoctorAgentRunRequestAllowsDoctorDefaults(t *testing.T) {
	if err := ValidateDoctorAgentRunRequest(AgentRunRequest{
		Meta:      map[string]any{"doctor_session": true},
		Mode:      string(RunnerModeReview),
		Isolation: string(IsolationHostReadOnly),
		MaxTurns:  4,
	}); err != nil {
		t.Fatalf("ValidateDoctorAgentRunRequest() = %v, want nil", err)
	}
}

func TestValidateDoctorAgentRunRequestIgnoresNonDoctorMeta(t *testing.T) {
	if err := ValidateDoctorAgentRunRequest(AgentRunRequest{
		Mode:      string(RunnerModeSafeEdit),
		Isolation: string(IsolationSandboxWrite),
		MaxTurns:  12,
	}); err != nil {
		t.Fatalf("ValidateDoctorAgentRunRequest() = %v, want nil", err)
	}
}
