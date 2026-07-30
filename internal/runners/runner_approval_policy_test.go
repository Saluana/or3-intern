package runners

import "testing"

func TestResolveRunnerApprovalAutopilot(t *testing.T) {
	t.Parallel()
	trueValue := true
	falseValue := false
	if !ResolveRunnerApprovalAutopilot(nil) {
		t.Fatal("expected default true when omitted")
	}
	if !ResolveRunnerApprovalAutopilot(&trueValue) {
		t.Fatal("expected explicit true")
	}
	if ResolveRunnerApprovalAutopilot(&falseValue) {
		t.Fatal("expected explicit false")
	}
}

func TestRunnerApprovalAutopilotFromTurnMeta(t *testing.T) {
	t.Parallel()
	if !RunnerApprovalAutopilotFromTurnMeta(`{"runner_approval_autopilot":true}`) {
		t.Fatal("expected true from meta")
	}
	if RunnerApprovalAutopilotFromTurnMeta(`{"runner_approval_autopilot":false}`) {
		t.Fatal("expected false from meta")
	}
	if !RunnerApprovalAutopilotFromTurnMeta(`{}`) {
		t.Fatal("expected default true when meta key missing")
	}
}
