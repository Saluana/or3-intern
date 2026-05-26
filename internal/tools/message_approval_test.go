package tools

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"or3-intern/internal/approval"
	"or3-intern/internal/config"
	"or3-intern/internal/db"
)

func TestSendMessage_RequiresApprovalWhenBrokerAskMode(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "message-approval.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	approvalCfg := config.Default().Security.Approvals
	approvalCfg.Enabled = true
	approvalCfg.HostID = "test-host"
	approvalCfg.MessageSend.Mode = config.ApprovalModeAsk
	broker := &approval.Broker{
		DB: database, Config: approvalCfg, HostID: approvalCfg.HostID,
		SignKey: []byte("0123456789abcdef0123456789abcdef"),
	}
	tool := &SendMessage{
		Deliver: func(ctx context.Context, channel, to, text string, meta map[string]any) error {
			t.Fatal("deliver should not run before approval")
			return nil
		},
		DefaultChannel: "slack",
		DefaultTo:      "U123",
		ApprovalBroker: broker,
	}
	_, err = tool.Execute(context.Background(), map[string]any{"text": "hello"})
	var approvalErr *ApprovalRequiredError
	if err == nil {
		t.Fatal("expected approval required error")
	}
	if !errors.As(err, &approvalErr) {
		t.Fatalf("expected ApprovalRequiredError, got %T: %v", err, err)
	}
}
