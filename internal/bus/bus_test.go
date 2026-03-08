package bus

import (
	"context"
	"testing"
	"time"
)

func TestNew_DefaultBuffer(t *testing.T) {
	b := New(0)
	if b == nil {
		t.Fatal("expected non-nil bus")
	}
	// should accept at least 128 events without blocking
	for i := 0; i < 128; i++ {
		ok := b.Publish(Event{Type: EventUserMessage, Message: "test"})
		if !ok {
			t.Fatalf("expected publish to succeed at i=%d", i)
		}
	}
}

func TestNew_CustomBuffer(t *testing.T) {
	b := New(4)
	if b == nil {
		t.Fatal("expected non-nil bus")
	}
	// first 4 succeed
	for i := 0; i < 4; i++ {
		ok := b.Publish(Event{Type: EventUserMessage})
		if !ok {
			t.Fatalf("expected publish to succeed at i=%d", i)
		}
	}
	// 5th should fail (buffer full)
	ok := b.Publish(Event{Type: EventUserMessage})
	if ok {
		t.Fatal("expected publish to fail on full buffer")
	}
}

func TestPublish_Success(t *testing.T) {
	b := New(10)
	ev := Event{
		Type:       EventUserMessage,
		SessionKey: "session1",
		Channel:    "cli",
		From:       "user",
		Message:    "hello",
		Meta:       map[string]any{"key": "val"},
	}
	ok := b.Publish(ev)
	if !ok {
		t.Fatal("expected publish to succeed")
	}

	got := <-b.Channel()
	if got.Type != EventUserMessage {
		t.Errorf("expected type %s, got %s", EventUserMessage, got.Type)
	}
	if got.SessionKey != "session1" {
		t.Errorf("expected session key 'session1', got %q", got.SessionKey)
	}
	if got.Message != "hello" {
		t.Errorf("expected message 'hello', got %q", got.Message)
	}
	if got.Meta["key"] != "val" {
		t.Errorf("expected meta key 'val', got %v", got.Meta["key"])
	}
}

func TestChannel_IsReadOnly(t *testing.T) {
	b := New(1)
	ch := b.Channel()
	if ch == nil {
		t.Fatal("expected non-nil channel")
	}
}

func TestEventTypes(t *testing.T) {
	cases := []EventType{EventUserMessage, EventCron, EventHeartbeat, EventSystem, EventWebhook, EventFileChange}
	for _, et := range cases {
		b := New(1)
		b.Publish(Event{Type: et})
		ev := <-b.Channel()
		if ev.Type != et {
			t.Errorf("expected type %s, got %s", et, ev.Type)
		}
	}
}

func TestBus_EventFlow(t *testing.T) {
	b := New(10)
	_ = context.Background()

	events := []Event{
		{Type: EventUserMessage, SessionKey: "s1", Message: "msg1"},
		{Type: EventCron, SessionKey: "s2", Message: "cron1"},
		{Type: EventSystem, SessionKey: "s3", Message: "sys1"},
	}

	for _, ev := range events {
		b.Publish(ev)
	}

	ch := b.Channel()
	for _, want := range events {
		select {
		case got := <-ch:
			if got.Type != want.Type || got.Message != want.Message {
				t.Errorf("got {%s,%s}, want {%s,%s}", got.Type, got.Message, want.Type, want.Message)
			}
		case <-time.After(100 * time.Millisecond):
			t.Fatal("timeout waiting for event")
		}
	}
}

func TestPublish_Overflow(t *testing.T) {
	b := New(2)
	b.Publish(Event{Type: EventUserMessage})
	b.Publish(Event{Type: EventUserMessage})
	// third publish should drop
	ok := b.Publish(Event{Type: EventUserMessage})
	if ok {
		t.Fatal("expected overflow publish to return false")
	}
}
