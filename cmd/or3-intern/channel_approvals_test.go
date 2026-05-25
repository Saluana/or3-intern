package main

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"testing"

	"or3-intern/internal/approval"
	"or3-intern/internal/bus"
	rootchannels "or3-intern/internal/channels"
	"or3-intern/internal/config"
	"or3-intern/internal/db"
	"or3-intern/internal/tools"
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

func TestServiceServer_DeliverApprovedResumeCompletionPreservesThread(t *testing.T) {
	manager := rootchannels.NewManager()
	channel := &captureChannel{name: "slack"}
	if err := manager.Register(channel); err != nil {
		t.Fatalf("Register: %v", err)
	}
	server := &serviceServer{channelDeliverer: manager}
	server.deliverApprovedResumeCompletion(context.Background(), db.ApprovalRequestRecord{
		ID: 7,
		RequesterContextJSON: approval.MarshalRequesterContext(approval.RequesterContext{
			Channel:     "slack",
			SessionKey:  "slack:C1:U1",
			From:        "U1",
			ReplyTarget: "C1",
			ReplyMeta:   map[string]any{"thread_ts": "123.45"},
		}),
	}, "done")
	if channel.lastText() != "done" {
		t.Fatalf("expected delivered completion, got %q", channel.lastText())
	}
	channel.mu.Lock()
	defer channel.mu.Unlock()
	if len(channel.targets) != 1 || channel.targets[0] != "C1" {
		t.Fatalf("expected Slack channel target C1, got %#v", channel.targets)
	}
	if len(channel.metas) != 1 || channel.metas[0]["thread_ts"] != "123.45" {
		t.Fatalf("expected thread metadata, got %#v", channel.metas)
	}
}

func TestChannelApprovalHandler_DeliversChainedApprovalAfterResume(t *testing.T) {
	broker, cleanup := buildServiceTestBroker(t, func(cfg *config.ApprovalConfig) {
		cfg.Exec.Mode = config.ApprovalModeAsk
	})
	defer cleanup()

	ctx := approval.ContextWithRequesterContext(context.Background(), approval.RequesterContext{
		Channel:     "telegram",
		SessionKey:  "telegram:123",
		From:        "456",
		ReplyTarget: "123",
		ReplyMeta:   map[string]any{"reply_to_message_id": int64(23)},
	})
	decision, err := broker.EvaluateExec(ctx, approval.ExecEvaluation{ExecutablePath: "/bin/echo", Argv: []string{"next"}, WorkingDir: "/tmp", ToolName: "exec", SessionID: "telegram:123"})
	if err != nil {
		t.Fatalf("EvaluateExec: %v", err)
	}
	manager := rootchannels.NewManager()
	channel := &captureChannel{name: "telegram"}
	if err := manager.Register(channel); err != nil {
		t.Fatalf("Register: %v", err)
	}
	handler := &channelApprovalHandler{Broker: broker, Channels: manager}
	delivered := handler.deliverResumeApprovalRequired(context.Background(), db.ApprovalRequestRecord{}, &tools.ApprovalRequiredError{ToolName: "exec", RequestID: decision.RequestID})
	if !delivered {
		t.Fatal("expected chained approval prompt to be delivered")
	}
	text := channel.lastText()
	if !strings.Contains(text, "One more approval is needed") || !strings.Contains(text, "/approve "+strconvFormatInt(decision.RequestID)) {
		t.Fatalf("expected chained approval instructions, got %q", text)
	}
	channel.mu.Lock()
	defer channel.mu.Unlock()
	if len(channel.targets) != 1 || channel.targets[0] != "123" {
		t.Fatalf("expected Telegram chat target 123, got %#v", channel.targets)
	}
	if len(channel.metas) != 1 || fmt.Sprint(channel.metas[0]["reply_to_message_id"]) != "23" {
		t.Fatalf("expected Telegram reply metadata, got %#v", channel.metas)
	}
}

func TestServiceServer_DeliverApprovedResumeApprovalRequiredPreservesThread(t *testing.T) {
	broker, cleanup := buildServiceTestBroker(t, func(cfg *config.ApprovalConfig) {
		cfg.Exec.Mode = config.ApprovalModeAsk
	})
	defer cleanup()

	ctx := approval.ContextWithRequesterContext(context.Background(), approval.RequesterContext{
		Channel:     "slack",
		SessionKey:  "slack:C1:U1",
		From:        "U1",
		ReplyTarget: "C1",
		ReplyMeta:   map[string]any{"thread_ts": "123.45"},
	})
	decision, err := broker.EvaluateExec(ctx, approval.ExecEvaluation{ExecutablePath: "/bin/echo", Argv: []string{"next"}, WorkingDir: "/tmp", ToolName: "exec", SessionID: "slack:C1:U1"})
	if err != nil {
		t.Fatalf("EvaluateExec: %v", err)
	}
	manager := rootchannels.NewManager()
	channel := &captureChannel{name: "slack"}
	if err := manager.Register(channel); err != nil {
		t.Fatalf("Register: %v", err)
	}
	server := &serviceServer{broker: broker, channelDeliverer: manager}
	delivered := server.deliverApprovedResumeApprovalRequired(context.Background(), db.ApprovalRequestRecord{}, &tools.ApprovalRequiredError{ToolName: "exec", RequestID: decision.RequestID})
	if !delivered {
		t.Fatal("expected chained approval prompt to be delivered")
	}
	text := channel.lastText()
	if !strings.Contains(text, "One more approval is needed") || !strings.Contains(text, "/approve "+strconvFormatInt(decision.RequestID)) {
		t.Fatalf("expected chained approval instructions, got %q", text)
	}
	channel.mu.Lock()
	defer channel.mu.Unlock()
	if len(channel.targets) != 1 || channel.targets[0] != "C1" {
		t.Fatalf("expected Slack channel target C1, got %#v", channel.targets)
	}
	if len(channel.metas) != 1 || channel.metas[0]["thread_ts"] != "123.45" {
		t.Fatalf("expected thread metadata, got %#v", channel.metas)
	}
}

func strconvFormatInt(value int64) string {
	return strconv.FormatInt(value, 10)
}
