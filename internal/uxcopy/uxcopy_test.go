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
	translated := TranslateError(errors.New("approval broker unavailable"))
	if translated.Title == "" || translated.Command == "" {
		t.Fatalf("unexpected translated error: %#v", translated)
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
