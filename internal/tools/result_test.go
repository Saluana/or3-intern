package tools

import (
	"fmt"
	"strings"
	"testing"
)

func TestEncodeToolFailureExecSummaryIncludesStructuredDetail(t *testing.T) {
	out := EncodeToolResult(ToolResult{
		Kind:    "exec",
		OK:      true,
		Summary: "Command completed with bounded stdout/stderr previews",
		Preview: "stdout:\n{\n  \"error\": {\n    \"code\": 401,\n    \"message\": \"Authentication failed\",\n    \"reason\": \"authError\"\n  }\n}\n\nstderr:\nerror[auth]: Authentication failed: invalid_grant",
	})

	got := EncodeToolFailure("exec", map[string]any{"program": "gws", "args": []any{"tasks", "tasklists", "list"}}, out, fmt.Errorf("exec failed: exit status 2"))
	result, ok := DecodeToolResult(got)
	if !ok {
		t.Fatalf("expected structured tool result, got %q", got)
	}
	if !strings.Contains(result.Summary, "exit status 2") {
		t.Fatalf("expected exit status in summary, got %#v", result)
	}
	if !strings.Contains(result.Summary, "error[auth]: Authentication failed: invalid_grant") {
		t.Fatalf("expected auth detail in summary, got %#v", result)
	}
}

func TestEncodeToolFailureTrimsDuplicateToolPrefix(t *testing.T) {
	got := EncodeToolFailure("exec", nil, "", fmt.Errorf("exec failed: exit status 3"))
	result, ok := DecodeToolResult(got)
	if !ok {
		t.Fatalf("expected structured tool result, got %q", got)
	}
	if result.Summary != "exec failed: exit status 3" {
		t.Fatalf("expected duplicate prefix to be trimmed, got %#v", result)
	}
}
