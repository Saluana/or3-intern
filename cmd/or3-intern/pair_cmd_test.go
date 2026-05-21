package main

import (
	"strings"
	"testing"
)

func TestParsePairArgs_Auto(t *testing.T) {
	opts, err := parsePairArgs([]string{"--auto"})
	if err != nil {
		t.Fatalf("parsePairArgs: %v", err)
	}
	if opts.DeviceName != "" {
		t.Fatal("expected empty device name by default")
	}
	if !opts.Auto {
		t.Fatal("expected auto to be true")
	}
	if opts.Role != "" {
		t.Fatal("expected empty role by default")
	}
	if opts.Manual {
		t.Fatal("expected manual to be false by default")
	}
}

func TestParsePairArgs_AutoWithName(t *testing.T) {
	opts, err := parsePairArgs([]string{"--auto", "--name", "My iPhone"})
	if err != nil {
		t.Fatalf("parsePairArgs: %v", err)
	}
	if opts.DeviceName != "My iPhone" {
		t.Fatalf("expected device name 'My iPhone', got %q", opts.DeviceName)
	}
}

func TestParsePairArgs_AutoWithRole(t *testing.T) {
	opts, err := parsePairArgs([]string{"--auto", "--role", "operator"})
	if err != nil {
		t.Fatalf("parsePairArgs: %v", err)
	}
	if opts.Role != "operator" {
		t.Fatalf("expected role 'operator', got %q", opts.Role)
	}
}

func TestParsePairArgs_InvalidRole(t *testing.T) {
	_, err := parsePairArgs([]string{"--auto", "--role", "owner"})
	if err == nil {
		t.Fatal("expected invalid role error")
	}
	if !strings.Contains(err.Error(), "viewer, operator, or admin") {
		t.Fatalf("expected role guidance, got %q", err.Error())
	}
}

func TestParsePairArgs_JSON(t *testing.T) {
	opts, err := parsePairArgs([]string{"--auto", "--json"})
	if err != nil {
		t.Fatalf("parsePairArgs: %v", err)
	}
	if !opts.JSON {
		t.Fatal("expected JSON option")
	}
}

func TestParsePairArgs_Manual(t *testing.T) {
	opts, err := parsePairArgs([]string{"--manual"})
	if err != nil {
		t.Fatalf("parsePairArgs: %v", err)
	}
	if !opts.Manual {
		t.Fatal("expected manual to be true")
	}
}

func TestParsePairArgs_AutoAndManualConflict(t *testing.T) {
	_, err := parsePairArgs([]string{"--auto", "--manual"})
	if err == nil {
		t.Fatal("expected conflict error")
	}
	if !strings.Contains(err.Error(), "not both") {
		t.Fatalf("expected conflict guidance, got %q", err.Error())
	}
}

func TestParsePairArgs_NoArgs(t *testing.T) {
	_, err := parsePairArgs([]string{})
	if err == nil {
		t.Fatal("expected error for no args")
	}
	if !strings.Contains(err.Error(), "pair --auto") {
		t.Fatalf("expected error to mention pair --auto, got %q", err.Error())
	}
}

func TestParsePairArgs_UnexpectedArgs(t *testing.T) {
	_, err := parsePairArgs([]string{"--auto", "extra"})
	if err == nil {
		t.Fatal("expected unexpected argument error")
	}
	if !strings.Contains(err.Error(), "unexpected arguments") {
		t.Fatalf("expected unexpected argument guidance, got %q", err.Error())
	}
}

func TestHelpTopic_Pair(t *testing.T) {
	var out strings.Builder
	if err := printHelpTopic(&out, []string{"pair"}); err != nil {
		t.Fatalf("printHelpTopic pair: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "or3-intern pair --auto") {
		t.Fatalf("expected pair --auto in help, got %q", got)
	}
	if !strings.Contains(got, "automatic readiness checks") {
		t.Fatalf("expected automatic readiness checks in help, got %q", got)
	}
}

func TestRootHelp_ShowsPairInSimpleCommands(t *testing.T) {
	var out strings.Builder
	printRootHelp(&out)
	got := out.String()
	if !strings.Contains(got, "pair --auto") {
		t.Fatalf("expected pair --auto in root help, got %q", got)
	}
}
