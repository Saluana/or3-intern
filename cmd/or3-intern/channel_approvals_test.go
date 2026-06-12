package main

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"testing"

	"or3-intern/internal/approval"
	"or3-intern/internal/bus"
	rootchannels "or3-intern/internal/channels"
	"or3-intern/internal/config"
	"or3-intern/internal/db"
)

type captureChannel struct {
	mu      sync.Mutex
	name    string
	texts   []string
	targets []string
	metas   []map[string]any
}

func (c *captureChannel) Name() string                          { return c.name }
func (c *captureChannel) Start(context.Context, *bus.Bus) error { return nil }
func (c *captureChannel) Stop(context.Context) error            { return nil }
func (c *captureChannel) Deliver(_ context.Context, to, text string, meta map[string]any) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.targets = append(c.targets, to)
	c.texts = append(c.texts, text)
	c.metas = append(c.metas, meta)
	return nil
}

func (c *captureChannel) lastText() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.texts) == 0 {
		return ""
	}
	return c.texts[len(c.texts)-1]
}

func TestChannelApprovalHandler_ApprovesMatchingRequester(t *testing.T) {
	broker, cleanup := buildServiceTestBroker(t, func(cfg *config.ApprovalConfig) {
		cfg.Exec.Mode = config.ApprovalModeAsk
	})
	defer cleanup()

	ctx := approval.ContextWithRequesterContext(context.Background(), approval.RequesterContext{
		Channel:     "telegram",
		SessionKey:  "telegram:123",
		From:        "456",
		ReplyTarget: "123",
		ReplyMeta:   map[string]any{"reply_to_message_id": int64(99)},
	})
	decision, err := broker.EvaluateExec(ctx, approval.ExecEvaluation{ExecutablePath: "/bin/echo", Argv: []string{"ok"}, WorkingDir: "/tmp", ToolName: "exec", SessionID: "telegram:123"})
	if err != nil {
		t.Fatalf("EvaluateExec: %v", err)
	}
	manager := rootchannels.NewManager()
	channel := &captureChannel{name: "telegram"}
	if err := manager.Register(channel); err != nil {
		t.Fatalf("Register: %v", err)
	}
	handler := &channelApprovalHandler{Broker: broker, Channels: manager}
	handled, err := handler.Handle(context.Background(), bus.Event{Type: bus.EventUserMessage, SessionKey: "telegram:123", Channel: "telegram", From: "456", Message: "/approve  " + strconvFormatInt(decision.RequestID), Meta: map[string]any{"chat_id": "123"}})
	if err != nil || !handled {
		t.Fatalf("Handle: handled=%t err=%v", handled, err)
	}
	rec, err := broker.DB.GetApprovalRequest(context.Background(), decision.RequestID)
	if err != nil {
		t.Fatalf("GetApprovalRequest: %v", err)
	}
	if rec.Status != approval.StatusApproved {
		t.Fatalf("expected approved request, got %s", rec.Status)
	}
	if !strings.Contains(channel.lastText(), "was approved") {
		t.Fatalf("expected approval ack, got %q", channel.lastText())
	}
}

func TestChannelApprovalHandler_RejectsMismatchedRequester(t *testing.T) {
	broker, cleanup := buildServiceTestBroker(t, func(cfg *config.ApprovalConfig) {
		cfg.Exec.Mode = config.ApprovalModeAsk
	})
	defer cleanup()

	ctx := approval.ContextWithRequesterContext(context.Background(), approval.RequesterContext{Channel: "slack", SessionKey: "slack:C1:U1", From: "U1", ReplyTarget: "C1"})
	decision, err := broker.EvaluateExec(ctx, approval.ExecEvaluation{ExecutablePath: "/bin/echo", Argv: []string{"ok"}, WorkingDir: "/tmp", ToolName: "exec", SessionID: "slack:C1:U1"})
	if err != nil {
		t.Fatalf("EvaluateExec: %v", err)
	}
	manager := rootchannels.NewManager()
	channel := &captureChannel{name: "slack"}
	if err := manager.Register(channel); err != nil {
		t.Fatalf("Register: %v", err)
	}
	handler := &channelApprovalHandler{Broker: broker, Channels: manager}
	handled, err := handler.Handle(context.Background(), bus.Event{Type: bus.EventUserMessage, SessionKey: "slack:C1:U1", Channel: "slack", From: "U2", Message: "approve " + strconvFormatInt(decision.RequestID), Meta: map[string]any{"channel_id": "C1"}})
	if err != nil || !handled {
		t.Fatalf("Handle: handled=%t err=%v", handled, err)
	}
	rec, err := broker.DB.GetApprovalRequest(context.Background(), decision.RequestID)
	if err != nil {
		t.Fatalf("GetApprovalRequest: %v", err)
	}
	if rec.Status != approval.StatusPending {
		t.Fatalf("expected pending request, got %s", rec.Status)
	}
	if !strings.Contains(channel.lastText(), "cannot accept") {
		t.Fatalf("expected rejection message, got %q", channel.lastText())
	}
}

func TestChannelApprovalHandler_AllowsLinkedScopeSessionMismatchWhenRequesterMatches(t *testing.T) {
	req := db.ApprovalRequestRecord{
		RequesterContextJSON: approval.MarshalRequesterContext(approval.RequesterContext{
			Channel:     "telegram",
			SessionKey:  "cli:default",
			From:        "456",
			ReplyTarget: "123",
		}),
	}
	ev := bus.Event{
		SessionKey: "telegram:123",
		Channel:    "telegram",
		From:       "456",
		Meta:       map[string]any{"chat_id": "123"},
	}
	if !channelApprovalRequestMatchesEvent(req, ev) {
		t.Fatal("expected requester/channel target match to allow linked scope session mismatch")
	}
}

func strconvFormatInt(value int64) string {
	return strconv.FormatInt(value, 10)
}
