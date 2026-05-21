package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestHealthCommand_SafeBaseline(t *testing.T) {
	cfg := safeDoctorConfig()
	var out bytes.Buffer
	if err := runHealthCommand("", cfg, "", nil, strings.NewReader(""), &out, &out); err != nil {
		t.Fatalf("runHealthCommand: %v", err)
	}
	if !strings.Contains(out.String(), "[ok] configuration looks safe") {
		t.Fatalf("expected ok output, got %q", out.String())
	}
}

func TestHealthCommand_CheckFlag(t *testing.T) {
	cfg := safeDoctorConfig()
	var out bytes.Buffer
	if err := runHealthCommand("", cfg, "", []string{"--check"}, strings.NewReader(""), &out, &out); err != nil {
		t.Fatalf("runHealthCommand --check: %v", err)
	}
	if !strings.Contains(out.String(), "[ok] configuration looks safe") {
		t.Fatalf("expected ok output, got %q", out.String())
	}
}

func TestHealthCommand_JSONOutput(t *testing.T) {
	cfg := safeDoctorConfig()
	var out bytes.Buffer
	if err := runHealthCommand("", cfg, "", []string{"--json"}, strings.NewReader(""), &out, &out); err != nil {
		t.Fatalf("runHealthCommand --json: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatalf("expected JSON output, got %v (%q)", err, out.String())
	}
	if payload["summary"] == nil || payload["findings"] == nil {
		t.Fatalf("expected summary and findings in JSON output, got %#v", payload)
	}
}

func TestHealthCommand_FixRepairs(t *testing.T) {
	cfg := safeDoctorConfig()
	cfg.Hardening.PrivilegedTools = true
	cfg.Hardening.EnableExecShell = true
	var out bytes.Buffer
	if err := runHealthCommand("", cfg, "", []string{"--fix"}, strings.NewReader(""), &out, &out); err != nil {
		t.Fatalf("runHealthCommand --fix: %v", err)
	}
}

func TestHealthCommand_ShowsWarnings(t *testing.T) {
	cfg := safeDoctorConfig()
	cfg.Hardening.PrivilegedTools = true
	cfg.Hardening.EnableExecShell = true
	var out bytes.Buffer
	if err := runHealthCommand("", cfg, "", nil, strings.NewReader(""), &out, &out); err != nil {
		t.Fatalf("runHealthCommand: %v", err)
	}
	text := out.String()
	if !strings.Contains(text, "exec") {
		t.Fatalf("expected exec warning in %q", text)
	}
}

func TestHealthCommand_UsesSameEngineAsDoctor(t *testing.T) {
	cfg := safeDoctorConfig()
	cfg.Hardening.PrivilegedTools = true
	cfg.Hardening.EnableExecShell = true

	var healthOut bytes.Buffer
	if err := runHealthCommand("", cfg, "", nil, strings.NewReader(""), &healthOut, &healthOut); err != nil {
		t.Fatalf("runHealthCommand: %v", err)
	}

	var doctorOut bytes.Buffer
	if err := runDoctorCommand("", cfg, "", nil, strings.NewReader(""), &doctorOut, &doctorOut); err != nil {
		t.Fatalf("runDoctorCommand: %v", err)
	}

	if healthOut.String() != doctorOut.String() {
		t.Fatalf("expected health and doctor to produce same output.\nHealth: %q\nDoctor: %q", healthOut.String(), doctorOut.String())
	}
}

func TestHelpTopic_Health(t *testing.T) {
	var out bytes.Buffer
	if err := printHelpTopic(&out, []string{"health"}); err != nil {
		t.Fatalf("printHelpTopic health: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "or3-intern health") {
		t.Fatalf("expected health usage, got %q", got)
	}
	if !strings.Contains(got, "Check if OR3 is ready to work") {
		t.Fatalf("expected health summary, got %q", got)
	}
	if !strings.Contains(got, "--check") {
		t.Fatalf("expected --check flag in help, got %q", got)
	}
}

func TestHelpTopic_DoctorPointsToHealth(t *testing.T) {
	var out bytes.Buffer
	if err := printHelpTopic(&out, []string{"doctor"}); err != nil {
		t.Fatalf("printHelpTopic doctor: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "Use `health` instead") {
		t.Fatalf("expected doctor help to reference health, got %q", got)
	}
}

func TestRootHelp_ShowsHealthInSimpleCommands(t *testing.T) {
	var out bytes.Buffer
	printRootHelp(&out)
	got := out.String()
	if !strings.Contains(got, "health") {
		t.Fatalf("expected health in root help, got %q", got)
	}
}
