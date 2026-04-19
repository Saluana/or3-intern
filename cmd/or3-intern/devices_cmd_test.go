package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"or3-intern/internal/approval"
)

func testDevicesBroker(t *testing.T) *approval.Broker {
	t.Helper()
	// reuse the approval broker helper from approvals_cmd_test.go
	b := testApprovalBroker(t)
	return b
}

// seedPairingRequest creates a pairing request and returns (id, code).
func seedPairingRequest(t *testing.T, broker *approval.Broker) (int64, string) {
	t.Helper()
	req, code, err := broker.CreatePairingRequest(context.Background(), approval.PairingRequestInput{
		Role:        approval.RoleOperator,
		DisplayName: "test-device",
		Origin:      "test",
	})
	if err != nil {
		t.Fatalf("CreatePairingRequest: %v", err)
	}
	return req.ID, code
}

func TestRunDevicesCommand_NilBroker(t *testing.T) {
	var out bytes.Buffer
	err := runDevicesCommand(context.Background(), nil, []string{"list"}, &out, &out)
	if err == nil {
		t.Fatal("expected error for nil broker")
	}
}

func TestRunDevicesCommand_NoArgs(t *testing.T) {
	broker := testDevicesBroker(t)
	var out bytes.Buffer
	err := runDevicesCommand(context.Background(), broker, nil, &out, &out)
	if err == nil || !strings.Contains(err.Error(), "usage") {
		t.Fatalf("expected usage error, got %v", err)
	}
}

func TestRunDevicesCommand_List_Empty(t *testing.T) {
	broker := testDevicesBroker(t)
	var out bytes.Buffer
	if err := runDevicesCommand(context.Background(), broker, []string{"list"}, &out, &out); err != nil {
		t.Fatalf("list: %v", err)
	}
	if strings.TrimSpace(out.String()) != "" {
		t.Errorf("expected empty device list, got %q", out.String())
	}
}

func TestRunDevicesCommand_Requests_ShowsPending(t *testing.T) {
	broker := testDevicesBroker(t)
	_, _ = seedPairingRequest(t, broker)

	var out bytes.Buffer
	if err := runDevicesCommand(context.Background(), broker, []string{"requests"}, &out, &out); err != nil {
		t.Fatalf("requests: %v", err)
	}
	if !strings.Contains(out.String(), "pending") {
		t.Errorf("expected 'pending' in requests output, got %q", out.String())
	}
}

func TestRunDevicesCommand_Approve(t *testing.T) {
	broker := testDevicesBroker(t)
	id, _ := seedPairingRequest(t, broker)

	var out bytes.Buffer
	idStr := sprint64(id)
	if err := runDevicesCommand(context.Background(), broker, []string{"approve", idStr}, &out, &out); err != nil {
		t.Fatalf("approve: %v", err)
	}
	if !strings.Contains(out.String(), "approved pairing request") {
		t.Errorf("expected 'approved pairing request' in output, got %q", out.String())
	}
}

func TestRunDevicesCommand_Approve_InvalidID(t *testing.T) {
	broker := testDevicesBroker(t)
	var out bytes.Buffer
	if err := runDevicesCommand(context.Background(), broker, []string{"approve", "notanumber"}, &out, &out); err == nil {
		t.Fatal("expected error for invalid ID")
	}
}

func TestRunDevicesCommand_Approve_MissingID(t *testing.T) {
	broker := testDevicesBroker(t)
	var out bytes.Buffer
	if err := runDevicesCommand(context.Background(), broker, []string{"approve"}, &out, &out); err == nil {
		t.Fatal("expected error for missing ID")
	}
}

func TestRunDevicesCommand_Deny(t *testing.T) {
	broker := testDevicesBroker(t)
	id, _ := seedPairingRequest(t, broker)

	var out bytes.Buffer
	idStr := sprint64(id)
	if err := runDevicesCommand(context.Background(), broker, []string{"deny", idStr}, &out, &out); err != nil {
		t.Fatalf("deny: %v", err)
	}
	if !strings.Contains(out.String(), "denied pairing request") {
		t.Errorf("expected 'denied pairing request' in output, got %q", out.String())
	}
}

func TestRunDevicesCommand_Deny_InvalidID(t *testing.T) {
	broker := testDevicesBroker(t)
	var out bytes.Buffer
	if err := runDevicesCommand(context.Background(), broker, []string{"deny", "bad"}, &out, &out); err == nil {
		t.Fatal("expected error for invalid ID")
	}
}

func TestRunDevicesCommand_RevokeAndListDevice(t *testing.T) {
	broker := testDevicesBroker(t)
	ctx := context.Background()

	// Create pairing request, approve it, then exchange for device token.
	id, code := seedPairingRequest(t, broker)
	if _, approveErr := broker.ApprovePairingRequest(ctx, id, "cli"); approveErr != nil {
		t.Fatalf("ApprovePairingRequest: %v", approveErr)
	}
	device, _, err := broker.ExchangePairingCode(ctx, approval.PairingExchangeInput{RequestID: id, Code: code})
	if err != nil {
		t.Fatalf("ExchangePairingCode: %v", err)
	}

	// Confirm device shows in list before revocation.
	var out bytes.Buffer
	if err := runDevicesCommand(ctx, broker, []string{"list"}, &out, &out); err != nil {
		t.Fatalf("list: %v", err)
	}
	if !strings.Contains(out.String(), device.DeviceID) {
		t.Errorf("expected device %s in list, got %q", device.DeviceID, out.String())
	}

	// Revoke.
	out.Reset()
	if err := runDevicesCommand(ctx, broker, []string{"revoke", device.DeviceID}, &out, &out); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if !strings.Contains(out.String(), "revoked device") {
		t.Errorf("expected 'revoked device' in output, got %q", out.String())
	}
}

func TestRunDevicesCommand_Revoke_MissingID(t *testing.T) {
	broker := testDevicesBroker(t)
	var out bytes.Buffer
	if err := runDevicesCommand(context.Background(), broker, []string{"revoke"}, &out, &out); err == nil {
		t.Fatal("expected error for missing device ID")
	}
}

func TestRunDevicesCommand_Rotate(t *testing.T) {
	broker := testDevicesBroker(t)
	ctx := context.Background()

	// Create, approve, and exchange pairing request to get a device.
	id, code := seedPairingRequest(t, broker)
	if _, err := broker.ApprovePairingRequest(ctx, id, "cli"); err != nil {
		t.Fatalf("ApprovePairingRequest: %v", err)
	}
	device, _, err := broker.ExchangePairingCode(ctx, approval.PairingExchangeInput{RequestID: id, Code: code})
	if err != nil {
		t.Fatalf("ExchangePairingCode: %v", err)
	}

	var out bytes.Buffer
	if err := runDevicesCommand(ctx, broker, []string{"rotate", device.DeviceID}, &out, &out); err != nil {
		t.Fatalf("rotate: %v", err)
	}
	text := out.String()
	if !strings.Contains(text, "rotated device") {
		t.Errorf("expected 'rotated device' in output, got %q", text)
	}
	if !strings.Contains(text, "token: ") {
		t.Errorf("expected 'token: ' in output, got %q", text)
	}
}

func TestRunDevicesCommand_Rotate_MissingID(t *testing.T) {
	broker := testDevicesBroker(t)
	var out bytes.Buffer
	if err := runDevicesCommand(context.Background(), broker, []string{"rotate"}, &out, &out); err == nil {
		t.Fatal("expected error for missing device ID")
	}
}

func TestRunDevicesCommand_Rotate_RevokedDeviceFails(t *testing.T) {
	broker := testDevicesBroker(t)
	ctx := context.Background()

	id, code := seedPairingRequest(t, broker)
	if _, err := broker.ApprovePairingRequest(ctx, id, "cli"); err != nil {
		t.Fatalf("ApprovePairingRequest: %v", err)
	}
	device, _, err := broker.ExchangePairingCode(ctx, approval.PairingExchangeInput{RequestID: id, Code: code})
	if err != nil {
		t.Fatalf("ExchangePairingCode: %v", err)
	}
	if err := broker.RevokeDevice(ctx, device.DeviceID, "cli"); err != nil {
		t.Fatalf("RevokeDevice: %v", err)
	}

	var out bytes.Buffer
	if err := runDevicesCommand(ctx, broker, []string{"rotate", device.DeviceID}, &out, &out); err == nil {
		t.Fatal("expected revoked device rotation to fail")
	}
}

func TestRunDevicesCommand_UnknownSubcommand(t *testing.T) {
	broker := testDevicesBroker(t)
	var out bytes.Buffer
	err := runDevicesCommand(context.Background(), broker, []string{"zap"}, &out, &out)
	if err == nil {
		t.Fatal("expected error for unknown subcommand")
	}
}

// TestRunDevicesCommand_CLI_ActorStamping verifies that CLI-triggered pairing
// actions use the "cli" actor string in audit-visible state changes.
func TestRunDevicesCommand_CLI_ActorStamping(t *testing.T) {
	broker := testDevicesBroker(t)
	ctx := context.Background()
	id, _ := seedPairingRequest(t, broker)

	// Approve using the CLI command (actor = "cli").
	var out bytes.Buffer
	if err := runDevicesCommand(ctx, broker, []string{"approve", sprint64(id)}, &out, &out); err != nil {
		t.Fatalf("approve: %v", err)
	}

	// Verify the pairing request records the "cli" approver.
	reqs, err := broker.ListPairingRequests(ctx, "approved", 10)
	if err != nil {
		t.Fatalf("ListPairingRequests: %v", err)
	}
	if len(reqs) == 0 {
		t.Fatal("expected approved pairing request")
	}
	if reqs[0].ApproverID != "cli" {
		t.Errorf("expected approver 'cli', got %q", reqs[0].ApproverID)
	}
}

// TestRunApprovalsCommand_CLI_ActorStamping verifies that CLI approval
// resolution stamps the "cli" actor into the resolved approval request.
func TestRunApprovalsCommand_CLI_ActorStamping(t *testing.T) {
	broker := testApprovalBroker(t)
	ctx := context.Background()

	// Seed a pending request.
	decision, err := broker.EvaluateExec(ctx, approval.ExecEvaluation{
		ExecutablePath: "/bin/sh",
		Argv:           []string{"/bin/sh", "-c", "echo actor-test"},
		WorkingDir:     "/tmp",
		ToolName:       "exec",
	})
	if err != nil || decision.RequestID == 0 {
		t.Fatalf("EvaluateExec: %v (id=%d)", err, decision.RequestID)
	}

	var out bytes.Buffer
	if err := runApprovalsCommand(ctx, broker, []string{"approve", sprint64(decision.RequestID)}, &out, &out); err != nil {
		t.Fatalf("approve: %v", err)
	}

	// Confirm that the request now shows status "approved".
	reqs, listErr := broker.ListApprovalRequests(ctx, "approved", 10)
	if listErr != nil {
		t.Fatalf("ListApprovalRequests: %v", listErr)
	}
	if len(reqs) == 0 {
		t.Fatal("expected approved approval request")
	}
	if reqs[0].ResolverActorID != "cli" {
		t.Errorf("expected resolver_actor_id 'cli', got %q", reqs[0].ResolverActorID)
	}
}

// Ensure "Now" field defaults don't break when nil (belt-and-suspenders).
func TestRunDevicesCommand_BrokerNowDefault(t *testing.T) {
	broker := testDevicesBroker(t)
	broker.Now = nil // should fall back to time.Now
	var out bytes.Buffer
	if err := runDevicesCommand(context.Background(), broker, []string{"list"}, &out, &out); err != nil {
		t.Fatalf("list with nil Now: %v", err)
	}
}

// Compile-time check that time is imported when not used directly.
var _ = time.Time{}
