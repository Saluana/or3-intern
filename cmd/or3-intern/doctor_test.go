package main

import (
	"bytes"
	"strings"
	"testing"

	"or3-intern/internal/config"
)

func TestRunDoctorCommand_PrintsWarnings(t *testing.T) {
	cfg := config.Default()
	cfg.Tools.RestrictToWorkspace = false
	cfg.Hardening.PrivilegedTools = true
	cfg.Triggers.Webhook.Enabled = true
	cfg.Triggers.Webhook.Secret = ""
	cfg.Triggers.Webhook.Addr = "0.0.0.0:8765"
	var out bytes.Buffer
	if err := runDoctorCommand(cfg, nil, &out, &out); err != nil {
		t.Fatalf("runDoctorCommand: %v", err)
	}
	text := out.String()
	if !strings.Contains(text, "filesystem") || !strings.Contains(text, "privileged-exec") || !strings.Contains(text, "webhook") {
		t.Fatalf("expected grouped warnings, got %q", text)
	}
}

func TestRunDoctorCommand_StrictFailsOnWarnings(t *testing.T) {
	cfg := config.Default()
	cfg.Tools.RestrictToWorkspace = false
	var out bytes.Buffer
	if err := runDoctorCommand(cfg, []string{"--strict"}, &out, &out); err == nil {
		t.Fatal("expected strict doctor run to fail on warnings")
	}
}
