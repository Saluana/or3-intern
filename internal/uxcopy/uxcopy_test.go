package uxcopy

import (
	"errors"
	"testing"

	"or3-intern/internal/safetymode"
)

func TestLabelForSetting(t *testing.T) {
	copy := LabelForSetting("tools.restrictToWorkspace")
	if copy.Label == "tools.restrictToWorkspace" || copy.Hint == "" {
		t.Fatalf("unexpected setting copy: %#v", copy)
	}
}

func TestProblemForFinding(t *testing.T) {
	copy := ProblemForFinding("security.audit_disabled", "audit logging is disabled")
	if copy.Title != "Safety log is off" {
		t.Fatalf("unexpected problem copy: %#v", copy)
	}
}

func TestTranslateError(t *testing.T) {
	tests := []struct {
		err     string
		command string
	}{
		{err: "approval broker unavailable", command: "or3-intern status"},
		{err: "approval required for exec", command: "or3-intern approvals list pending"},
		{err: "audit logger unavailable", command: "or3-intern status"},
		{err: "unknown tool in tool_policy", command: "or3-intern settings"},
		{err: "runtime unavailable", command: "or3-intern status"},
		{err: "service auth missing", command: "or3-intern connect-device"},
		{err: "workspace missing", command: "or3-intern settings --section workspace"},
		{err: "sandbox not found", command: "or3-intern status --advanced"},
		{err: "provider api key missing", command: "or3-intern settings --section provider"},
	}
	for _, tc := range tests {
		translated := TranslateError(errors.New(tc.err))
		if translated.Title == "" || translated.Command != tc.command || translated.Advanced == "" {
			t.Fatalf("unexpected translated error for %q: %#v", tc.err, translated)
		}
	}
}

func TestSafetyModeLabel(t *testing.T) {
	if got := SafetyModeLabel(safetymode.ModeBalanced, false, ""); got != "Balanced" {
		t.Fatalf("unexpected label: %q", got)
	}
	if got := SafetyModeLabel(safetymode.ModeCustom, true, safetymode.ModeBalanced); got != "Custom based on Balanced" {
		t.Fatalf("unexpected custom label: %q", got)
	}
}
