package approval

import "testing"

func TestRunnerAutopilotSensitiveTarget(t *testing.T) {
	t.Parallel()
	tests := []struct {
		target string
		want   bool
	}{
		{target: ".ssh/id_ed25519", want: true},
		{target: "home/.ssh/id_rsa", want: true},
		{target: "/Users/me/.ssh/id_rsa", want: true},
		{target: ".gnupg/private-keys-v1.d/key", want: true},
		{target: ".aws/credentials", want: true},
		{target: ".config/gh/hosts.yml", want: true},
		{target: "etc/passwd", want: true},
		{target: "/etc/hosts", want: true},
		{target: "var/db/systemkey", want: true},
		{target: ".env", want: true},
		{target: "config/.env.local", want: true},
		{target: "approvals.key", want: true},
		{target: "src/main.go", want: false},
		{target: "README.md", want: false},
		{target: "docs/approval-flow.md", want: true},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.target, func(t *testing.T) {
			t.Parallel()
			if got := runnerAutopilotSensitiveTarget(tt.target); got != tt.want {
				t.Fatalf("runnerAutopilotSensitiveTarget(%q) = %v, want %v", tt.target, got, tt.want)
			}
		})
	}
}

func TestReviewRunnerPermissionAutopilot_RejectsRelativeSSHWrite(t *testing.T) {
	t.Parallel()
	decision := reviewRunnerPermissionAutopilot(RunnerPermissionEvaluation{
		Autopilot:      true,
		PermissionKind: "filesystem",
		Access:         "write",
		TargetPath:     ".ssh/id_ed25519",
		RunnerMode:     "safe_edit",
	})
	if decision.Action != "escalate" {
		t.Fatalf("expected escalate for relative .ssh write, got action=%q reason=%q", decision.Action, decision.Reason)
	}
	if decision.Risk != "extreme" {
		t.Fatalf("expected extreme risk, got %q", decision.Risk)
	}
}
