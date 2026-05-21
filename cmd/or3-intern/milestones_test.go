package main

import (
	"bytes"
	"strings"
	"testing"

	"or3-intern/internal/config"
)

func TestMarkMilestone(t *testing.T) {
	cfg := config.Default()
	if HasMilestone(cfg, MilestoneSetupComplete) {
		t.Fatal("expected no milestone initially")
	}

	MarkMilestone(&cfg, MilestoneSetupComplete)
	if !HasMilestone(cfg, MilestoneSetupComplete) {
		t.Fatal("expected milestone to be set")
	}
	if cfg.Milestones[MilestoneSetupComplete] == "" {
		t.Fatal("expected milestone timestamp to be set")
	}
}

func TestMarkMilestone_NilConfig(t *testing.T) {
	// Should not panic
	MarkMilestone(nil, MilestoneSetupComplete)
}

func TestHasMilestone_EmptyConfig(t *testing.T) {
	cfg := config.Default()
	if HasMilestone(cfg, "nonexistent") {
		t.Fatal("expected false for nonexistent milestone")
	}
}

func TestPrintSetupSuccess(t *testing.T) {
	var out bytes.Buffer
	cfg := config.Default()
	PrintSetupSuccess(&out, cfg, true)
	got := out.String()
	if !strings.Contains(got, "You did it") {
		t.Fatalf("expected success message, got %q", got)
	}
	if !strings.Contains(got, "or3-intern chat") {
		t.Fatalf("expected chat instruction, got %q", got)
	}
	if !strings.Contains(got, "or3-intern pair --auto") {
		t.Fatalf("expected pair instruction, got %q", got)
	}
	if !strings.Contains(got, "or3-intern health") {
		t.Fatalf("expected health instruction, got %q", got)
	}
	if !strings.Contains(got, "or3-intern settings") {
		t.Fatalf("expected settings instruction, got %q", got)
	}
}

func TestPrintPairingSuccess(t *testing.T) {
	var out bytes.Buffer
	PrintPairingSuccess(&out, "My iPhone", "Chat only")
	got := out.String()
	if !strings.Contains(got, "Device connected") {
		t.Fatalf("expected success message, got %q", got)
	}
	if !strings.Contains(got, "My iPhone") {
		t.Fatalf("expected device name, got %q", got)
	}
	if !strings.Contains(got, "Chat only") {
		t.Fatalf("expected role, got %q", got)
	}
}

func TestPrintFirstChatSuccess(t *testing.T) {
	var out bytes.Buffer
	PrintFirstChatSuccess(&out)
	got := out.String()
	if !strings.Contains(got, "first conversation") {
		t.Fatalf("expected first conversation message, got %q", got)
	}
	if !strings.Contains(got, "Tips:") {
		t.Fatalf("expected tips section, got %q", got)
	}
}

func TestMilestoneCopy(t *testing.T) {
	tests := []struct {
		milestone string
		want      string
	}{
		{MilestoneSetupComplete, "Setup complete"},
		{MilestonePairingComplete, "Device paired"},
		{MilestoneFirstChatComplete, "First chat complete"},
		{"custom_milestone", "custom milestone"},
	}
	for _, tt := range tests {
		got := MilestoneCopy(tt.milestone)
		if got != tt.want {
			t.Errorf("MilestoneCopy(%q) = %q, want %q", tt.milestone, got, tt.want)
		}
	}
}
