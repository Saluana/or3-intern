package main

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"

	"or3-intern/internal/approval"
	"or3-intern/internal/config"
	"or3-intern/internal/db"
)

func testPairingBroker(t *testing.T) *approval.Broker {
	t.Helper()
	database, err := db.Open(filepath.Join(t.TempDir(), "pairing.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	approvalCfg := config.Default().Security.Approvals
	approvalCfg.Enabled = true
	approvalCfg.Pairing.Mode = config.ApprovalModeAsk
	return &approval.Broker{
		DB:      database,
		Config:  approvalCfg,
		HostID:  approvalCfg.HostID,
		SignKey: []byte("0123456789abcdef0123456789abcdef"),
	}
}

func TestRunPairingCommand_RequestApproveExchangeRoundTrip(t *testing.T) {
	broker := testPairingBroker(t)
	ctx := context.Background()

	var out bytes.Buffer
	if err := runPairingCommand(ctx, broker, []string{"request", "--channel", "slack", "--identity", "U42", "--name", "Slack User"}, &out, &out); err != nil {
		t.Fatalf("request: %v", err)
	}
	text := out.String()
	if !strings.Contains(text, "device_id: slack:U42") || !strings.Contains(text, "code: ") {
		t.Fatalf("unexpected request output: %q", text)
	}
	lines := strings.Split(strings.TrimSpace(text), "\n")
	var requestID, code string
	for _, line := range lines {
		if strings.HasPrefix(line, "id: ") {
			requestID = strings.TrimSpace(strings.TrimPrefix(line, "id: "))
		}
		if strings.HasPrefix(line, "code: ") {
			code = strings.TrimSpace(strings.TrimPrefix(line, "code: "))
		}
	}
	if requestID == "" || code == "" {
		t.Fatalf("missing request id or code in output: %q", text)
	}

	out.Reset()
	if err := runPairingCommand(ctx, broker, []string{"approve", requestID}, &out, &out); err != nil {
		t.Fatalf("approve: %v", err)
	}
	if !strings.Contains(out.String(), "approved pairing request") {
		t.Fatalf("unexpected approve output: %q", out.String())
	}

	out.Reset()
	if err := runPairingCommand(ctx, broker, []string{"exchange", requestID, code}, &out, &out); err != nil {
		t.Fatalf("exchange: %v", err)
	}
	if !strings.Contains(out.String(), "paired device slack:U42") || !strings.Contains(out.String(), "token: ") {
		t.Fatalf("unexpected exchange output: %q", out.String())
	}

	allowed, err := broker.IsPairedChannelIdentity(ctx, "slack", "U42")
	if err != nil {
		t.Fatalf("IsPairedChannelIdentity: %v", err)
	}
	if !allowed {
		t.Fatal("expected exchanged channel identity to be paired")
	}
}

func TestRunPairingCommand_RejectsExtraArgs(t *testing.T) {
	broker := testPairingBroker(t)
	ctx := context.Background()
	var out bytes.Buffer
	for _, args := range [][]string{{"list", "pending", "extra"}, {"request", "extra"}, {"exchange", "1", "code", "extra"}} {
		if err := runPairingCommand(ctx, broker, args, &out, &out); err == nil {
			t.Fatalf("expected args %v to fail", args)
		}
	}
}
