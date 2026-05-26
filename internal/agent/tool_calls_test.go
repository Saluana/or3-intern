package agent

import (
	"strings"
	"testing"

	"or3-intern/internal/tools"
)

func TestAvailableNormalizedToolCallsMapsDoctorStatusExecAlias(t *testing.T) {
	registry := tools.NewRegistry()
	registry.Register(&toolPolicyStubTool{name: "doctor_status"})

	got := availableNormalizedToolCalls([]NormalizedToolCall{{
		ID:            "call_1",
		Name:          "exec",
		ArgumentsJSON: `{"command":"or3-intern status --advanced"}`,
	}}, registry)

	if len(got) != 1 {
		t.Fatalf("expected one mapped tool call, got %#v", got)
	}
	if got[0].Name != "doctor_status" {
		t.Fatalf("expected exec to map to doctor_status, got %q", got[0].Name)
	}
	if got[0].ArgumentsJSON != "{}" {
		t.Fatalf("expected doctor_status args to be empty object, got %q", got[0].ArgumentsJSON)
	}
}

func TestUnavailableNormalizedToolCallPromptMentionsAskModeForWriteTools(t *testing.T) {
	registry := tools.NewRegistry()
	registry.Register(&toolPolicyStubTool{name: "read_file", groups: []string{tools.ToolGroupRead}})

	got := unavailableNormalizedToolCallPrompt([]NormalizedToolCall{{
		Name: "write_file",
	}}, registry)

	if !strings.Contains(got, "Ask") || !strings.Contains(got, "will not succeed") {
		t.Fatalf("expected Ask-mode write guidance, got %q", got)
	}
	if strings.Contains(got, "edit_file") && strings.Contains(got, "instead") {
		t.Fatalf("expected no misleading alternate-tool advice, got %q", got)
	}
}

func TestUnavailableNormalizedToolCallPromptMentionsDoctorStatusInsteadOfExec(t *testing.T) {
	registry := tools.NewRegistry()
	registry.Register(&toolPolicyStubTool{name: "doctor_status"})

	got := unavailableNormalizedToolCallPrompt([]NormalizedToolCall{{
		Name: "exec",
	}}, registry)

	if !strings.Contains(got, "cannot use exec") || !strings.Contains(got, "doctor_status") {
		t.Fatalf("expected Doctor-specific exec guidance, got %q", got)
	}
}
