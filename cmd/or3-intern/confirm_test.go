package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestConfirmDestructiveActionInteractiveRequiresYes(t *testing.T) {
	var out bytes.Buffer
	ok, err := confirmDestructiveAction(destructiveConfirmation{
		Action:      "Revoke paired device",
		ItemName:    "Grandma's phone",
		Consequence: "This device will stop being able to use this computer.",
		Undo:        "There is no undo.",
		Stdin:       strings.NewReader("no\n"),
		Stdout:      &out,
		StdinTTY:    true,
		StdoutTTY:   true,
	})
	if err != nil {
		t.Fatalf("confirm: %v", err)
	}
	if ok {
		t.Fatal("expected non-yes answer to cancel")
	}
	text := out.String()
	for _, want := range []string{"Grandma's phone", "Consequence:", "Undo:", "Type yes"} {
		if !strings.Contains(text, want) {
			t.Fatalf("expected prompt to contain %q, got %q", want, text)
		}
	}
}

func TestConfirmDestructiveActionInteractiveAcceptsYes(t *testing.T) {
	var out bytes.Buffer
	ok, err := confirmDestructiveAction(destructiveConfirmation{
		Action:    "Delete scheduled task",
		ItemName:  "Morning report",
		Stdin:     strings.NewReader("yes\n"),
		Stdout:    &out,
		StdinTTY:  true,
		StdoutTTY: true,
	})
	if err != nil {
		t.Fatalf("confirm: %v", err)
	}
	if !ok {
		t.Fatal("expected yes answer to confirm")
	}
}

func TestConfirmDestructiveActionForceAndNonInteractiveSkipPrompt(t *testing.T) {
	for _, tc := range []destructiveConfirmation{
		{Force: true, StdinTTY: true, StdoutTTY: true},
		{Force: false, StdinTTY: false, StdoutTTY: true},
		{Force: false, StdinTTY: true, StdoutTTY: false},
	} {
		var out bytes.Buffer
		tc.Stdout = &out
		ok, err := confirmDestructiveAction(tc)
		if err != nil {
			t.Fatalf("confirm: %v", err)
		}
		if !ok {
			t.Fatal("expected prompt to be skipped")
		}
		if out.Len() != 0 {
			t.Fatalf("expected no prompt output, got %q", out.String())
		}
	}
}

func TestSplitForceFlag(t *testing.T) {
	args, force, err := splitForceFlag([]string{"device-1", "--force"})
	if err != nil {
		t.Fatalf("splitForceFlag: %v", err)
	}
	if !force || len(args) != 1 || args[0] != "device-1" {
		t.Fatalf("unexpected result args=%v force=%t", args, force)
	}
}
