package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	remoteconnect "or3-intern/internal/connect"
)

func TestConnectStatusIsFriendlyWhenNotConnected(t *testing.T) {
	var stdout bytes.Buffer
	err := runConnectCommand(context.Background(), filepath.Join(t.TempDir(), "config.json"), []string{"status", "--state-dir", t.TempDir()}, &stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("runConnectCommand: %v", err)
	}
	if !strings.Contains(stdout.String(), "Remote access is not connected") {
		t.Fatalf("unexpected output: %s", stdout.String())
	}
}

func TestConnectStatusDoesNotPrintCredentials(t *testing.T) {
	stateDir := t.TempDir()
	if err := remoteconnect.SaveState(stateDir, remoteconnect.State{
		CloudURL:        "https://or3.chat",
		EnvironmentName: "Studio Mac",
		Hostname:        "studio.example.test",
		ControlToken:    "control-secret",
	}, "tunnel-secret"); err != nil {
		t.Fatalf("SaveState: %v", err)
	}
	var stdout bytes.Buffer
	if err := runConnectCommand(context.Background(), "", []string{"status", "--state-dir", stateDir}, &stdout, &bytes.Buffer{}); err != nil {
		t.Fatalf("runConnectCommand: %v", err)
	}
	output := stdout.String()
	if strings.Contains(output, "control-secret") || strings.Contains(output, "tunnel-secret") {
		t.Fatalf("status leaked credentials: %s", output)
	}
	if !strings.Contains(output, "Studio Mac") {
		t.Fatalf("status omitted computer name: %s", output)
	}
}

func TestConnectHelpIsDiscoverable(t *testing.T) {
	var output bytes.Buffer
	if err := printHelpTopic(&output, []string{"connect"}); err != nil {
		t.Fatalf("printHelpTopic: %v", err)
	}
	if !strings.Contains(output.String(), "or3-intern connect") {
		t.Fatalf("connect help missing: %s", output.String())
	}
}

func TestDefaultConnectStateRespectsOverride(t *testing.T) {
	override := filepath.Join(t.TempDir(), "state")
	t.Setenv("OR3_CONNECT_HOME", override)
	if got := remoteconnect.DefaultStateDir(); got != override {
		t.Fatalf("DefaultStateDir() = %q, want %q", got, override)
	}
	if _, err := os.Stat(override); !os.IsNotExist(err) {
		t.Fatalf("state lookup should not create the directory")
	}
}
