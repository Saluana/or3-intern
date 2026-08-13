package main

import (
	"bytes"
	"context"
	"fmt"
	"strconv"
	"strings"
	"testing"

	"or3-intern/internal/approval"
	"or3-intern/internal/config"
)

func TestRunDevicesCommand_CreateLifecycle(t *testing.T) {
	broker := testApprovalBroker(t)
	ctx := context.Background()
	var out bytes.Buffer
	var stderr bytes.Buffer

	if err := runDevicesCommand(ctx, broker, []string{"create"}, &out, &stderr); err != nil {
		t.Fatalf("devices create: %v (stderr: %s)", err, stderr.String())
	}
	token := outputValueAfter(out.String(), "Token (shown once):")
	if token == "" {
		t.Fatalf("expected token in output, got %q", out.String())
	}
	device, err := broker.AuthenticateDeviceToken(ctx, token, approval.RoleOperator)
	if err != nil {
		t.Fatalf("created token did not authenticate: %v", err)
	}
	if device.DeviceID != "or3-chat-local" || device.DisplayName != "OR3 Chat" {
		t.Fatalf("unexpected created device: %#v", device)
	}

	out.Reset()
	if err := runDevicesCommand(ctx, broker, []string{"list"}, &out, &stderr); err != nil {
		t.Fatalf("devices list: %v", err)
	}
	if !strings.Contains(out.String(), "or3-chat-local") || strings.Contains(out.String(), token) {
		t.Fatalf("list should identify the device without exposing its token, got %q", out.String())
	}

	out.Reset()
	if err := runDevicesCommand(ctx, broker, []string{"rotate", "or3-chat-local", "--force"}, &out, &stderr); err != nil {
		t.Fatalf("devices rotate: %v", err)
	}
	rotatedToken := outputValueAfter(out.String(), "Token (shown once):")
	if rotatedToken == "" || rotatedToken == token {
		t.Fatalf("expected a new token, got %q", out.String())
	}
	if _, err := broker.AuthenticateDeviceToken(ctx, token); err == nil {
		t.Fatal("old token still authenticated after rotation")
	}
	if _, err := broker.AuthenticateDeviceToken(ctx, rotatedToken, approval.RoleOperator); err != nil {
		t.Fatalf("rotated token did not authenticate: %v", err)
	}

	out.Reset()
	if err := runDevicesCommand(ctx, broker, []string{"revoke", "or3-chat-local", "--force"}, &out, &stderr); err != nil {
		t.Fatalf("devices revoke: %v", err)
	}
	if _, err := broker.AuthenticateDeviceToken(ctx, rotatedToken); err == nil {
		t.Fatal("token still authenticated after revocation")
	}
}

func TestRunDevicesCommand_CreateRejectsExistingDevice(t *testing.T) {
	broker := testApprovalBroker(t)
	ctx := context.Background()
	var out bytes.Buffer

	if err := runDevicesCommand(ctx, broker, []string{"create", "--id", "brave"}, &out, &out); err != nil {
		t.Fatalf("first create: %v", err)
	}
	err := runDevicesCommand(ctx, broker, []string{"create", "--id", "brave"}, &out, &out)
	if err == nil || !strings.Contains(err.Error(), "devices rotate") {
		t.Fatalf("expected rotate guidance for duplicate device, got %v", err)
	}
}

func TestRunPairingCommand_RequestApproveExchange(t *testing.T) {
	broker := testApprovalBroker(t)
	broker.Config.Pairing.Mode = config.ApprovalModeAsk
	ctx := context.Background()
	var out bytes.Buffer
	var stderr bytes.Buffer

	if err := runPairingCommand(ctx, broker, []string{
		"request",
		"--id", "brave-browser",
		"--name", "OR3 Chat in Brave",
		"--channel", "web",
		"--identity", "brendon",
	}, &out, &stderr); err != nil {
		t.Fatalf("pairing request: %v (stderr: %s)", err, stderr.String())
	}
	requestID, err := strconv.ParseInt(outputLineValue(out.String(), "request_id"), 10, 64)
	if err != nil {
		t.Fatalf("invalid request ID output: %q", out.String())
	}
	code := outputLineValue(out.String(), "pairing_code")
	if len(code) != 6 {
		t.Fatalf("expected six-digit pairing code, got %q", code)
	}

	out.Reset()
	if err := runPairingCommand(ctx, broker, []string{"approve-code", code}, &out, &stderr); err != nil {
		t.Fatalf("pairing approve-code: %v", err)
	}
	if !strings.Contains(out.String(), fmt.Sprintf("approved %d", requestID)) {
		t.Fatalf("unexpected approval output: %q", out.String())
	}

	out.Reset()
	if err := runPairingCommand(ctx, broker, []string{"exchange", strconv.FormatInt(requestID, 10), code}, &out, &stderr); err != nil {
		t.Fatalf("pairing exchange: %v", err)
	}
	token := outputValueAfter(out.String(), "Token (shown once):")
	device, err := broker.AuthenticateDeviceToken(ctx, token, approval.RoleOperator)
	if err != nil {
		t.Fatalf("exchanged token did not authenticate: %v", err)
	}
	if device.DeviceID != "brave-browser" || device.Metadata["channel"] != "web" || device.Metadata["identity"] != "brendon" {
		t.Fatalf("unexpected paired device: %#v", device)
	}
}

func TestRunPairingCommand_Deny(t *testing.T) {
	broker := testApprovalBroker(t)
	broker.Config.Pairing.Mode = config.ApprovalModeAsk
	ctx := context.Background()
	var out bytes.Buffer

	if err := runPairingCommand(ctx, broker, []string{"request", "--id", "denied-device"}, &out, &out); err != nil {
		t.Fatalf("pairing request: %v", err)
	}
	requestID := outputLineValue(out.String(), "request_id")
	out.Reset()
	if err := runPairingCommand(ctx, broker, []string{"deny", requestID}, &out, &out); err != nil {
		t.Fatalf("pairing deny: %v", err)
	}
	if !strings.Contains(out.String(), "denied "+requestID) {
		t.Fatalf("unexpected deny output: %q", out.String())
	}
}

func TestRunDevicesAndPairingCommand_UsageAndValidation(t *testing.T) {
	broker := testApprovalBroker(t)
	var out bytes.Buffer
	for name, run := range map[string]func() error{
		"devices usage": func() error { return runDevicesCommand(context.Background(), broker, nil, &out, &out) },
		"pairing usage": func() error { return runPairingCommand(context.Background(), broker, nil, &out, &out) },
		"invalid role": func() error {
			return runDevicesCommand(context.Background(), broker, []string{"create", "--role", "superuser"}, &out, &out)
		},
		"partial binding": func() error {
			return runPairingCommand(context.Background(), broker, []string{"request", "--channel", "slack"}, &out, &out)
		},
	} {
		t.Run(name, func(t *testing.T) {
			if err := run(); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestPreRuntimeCommands_DispatchDevicesAndPairing(t *testing.T) {
	broker := testApprovalBroker(t)
	broker.Config.Pairing.Mode = config.ApprovalModeAsk
	var out bytes.Buffer

	handled, err := runPreRuntimeCommand(context.Background(), "devices", "", config.Config{}, broker.DB, nil, nil, broker, []string{"create", "--id", "dispatch-device"}, &out, &out)
	if err != nil || !handled {
		t.Fatalf("devices dispatch: handled=%v err=%v", handled, err)
	}
	out.Reset()
	handled, err = runPreRuntimeCommand(context.Background(), "pairing", "", config.Config{}, broker.DB, nil, nil, broker, []string{"request", "--id", "dispatch-pairing"}, &out, &out)
	if err != nil || !handled {
		t.Fatalf("pairing dispatch: handled=%v err=%v", handled, err)
	}
	if !commandHandledBeforeRuntimeBootstrap("devices") || !commandHandledBeforeRuntimeBootstrap("pairing") {
		t.Fatal("device commands should return before provider and runner bootstrap")
	}
}

func outputLineValue(output, key string) string {
	prefix := key + ":"
	for _, line := range strings.Split(output, "\n") {
		if strings.HasPrefix(line, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(line, prefix))
		}
	}
	return ""
}

func outputValueAfter(output, marker string) string {
	lines := strings.Split(output, "\n")
	for index, line := range lines {
		if strings.TrimSpace(line) == marker && index+1 < len(lines) {
			return strings.TrimSpace(lines[index+1])
		}
	}
	return ""
}
