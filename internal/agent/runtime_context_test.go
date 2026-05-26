package agent

import (
	"runtime"
	"strings"
	"testing"
)

func TestRenderRuntimeContextIncludesHostAndTime(t *testing.T) {
	b := &Builder{WorkspaceDir: "/tmp/or3-workspace"}
	body := b.renderRuntimeContext("work")
	if !strings.Contains(body, "Host OS:") {
		t.Fatalf("expected host OS line, got %q", body)
	}
	if runtime.GOOS == "darwin" && !strings.Contains(body, "macOS") {
		t.Fatalf("expected macOS on darwin, got %q", body)
	}
	if !strings.Contains(body, "Local time:") || !strings.Contains(body, "Working directory: /tmp/or3-workspace") {
		t.Fatalf("expected time and cwd, got %q", body)
	}
	if !strings.Contains(body, "CPU arch:") {
		t.Fatalf("expected cpu arch, got %q", body)
	}
}

func TestRenderRuntimeContextIncludesToolPolicyMode(t *testing.T) {
	body := (&Builder{}).renderRuntimeContext("ask")
	if !strings.Contains(body, "Tool policy mode: ask") {
		t.Fatalf("expected mode line, got %q", body)
	}
	if !strings.Contains(body, "Ask mode:") || !strings.Contains(body, "do not retry") {
		t.Fatalf("expected ask summary with retry guidance, got %q", body)
	}
}

func TestBuildWithOptions_RuntimeContextIncludesPolicyFromMeta(t *testing.T) {
	d := openTestDB(t)
	b := &Builder{DB: d, HistoryMax: 5}
	pp, _, err := b.BuildWithOptions(t.Context(), BuildOptions{
		SessionKey:  "sess-runtime",
		UserMessage: "hello",
		EventMeta:   map[string]any{"tool_policy_mode": "work"},
	})
	if err != nil {
		t.Fatalf("BuildWithOptions: %v", err)
	}
	sys := systemPromptText(pp.System[0].Content)
	if !strings.Contains(sys, "Tool policy mode: work") || !strings.Contains(sys, "Work mode:") {
		t.Fatalf("expected work mode in runtime context, got %s", sys)
	}
}

func TestToolPolicyModeSummaryUnknownModeEmpty(t *testing.T) {
	if got := toolPolicyModeSummary("bogus"); got != "" {
		t.Fatalf("expected empty summary for unknown mode, got %q", got)
	}
}
