package tools

import (
	"context"
	"errors"
	"testing"
)

func TestSendMessage_NoDeliver(t *testing.T) {
	tool := &SendMessage{}
	_, err := tool.Execute(context.Background(), map[string]any{
		"text": "hello",
	})
	if err == nil {
		t.Fatal("expected error when deliver is nil")
	}
}

func TestSendMessage_Success(t *testing.T) {
	var gotChannel, gotTo, gotText string
	tool := &SendMessage{
		Deliver: func(ctx context.Context, ch, to, text string) error {
			gotChannel = ch
			gotTo = to
			gotText = text
			return nil
		},
		DefaultChannel: "cli",
		DefaultTo:      "user",
	}
	out, err := tool.Execute(context.Background(), map[string]any{
		"text":    "hello world",
		"channel": "",
		"to":      "",
	})
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	if out != "ok" {
		t.Errorf("expected 'ok', got %q", out)
	}
	if gotChannel != "cli" {
		t.Errorf("expected channel 'cli', got %q", gotChannel)
	}
	if gotTo != "user" {
		t.Errorf("expected to 'user', got %q", gotTo)
	}
	if gotText != "hello world" {
		t.Errorf("expected text 'hello world', got %q", gotText)
	}
}

func TestSendMessage_CustomChannelAndTo(t *testing.T) {
	var gotChannel, gotTo string
	tool := &SendMessage{
		Deliver: func(ctx context.Context, ch, to, text string) error {
			gotChannel = ch
			gotTo = to
			return nil
		},
		DefaultChannel: "default-ch",
		DefaultTo:      "default-to",
	}
	tool.Execute(context.Background(), map[string]any{
		"text":    "msg",
		"channel": "custom-ch",
		"to":      "custom-to",
	})
	if gotChannel != "custom-ch" {
		t.Errorf("expected channel 'custom-ch', got %q", gotChannel)
	}
	if gotTo != "custom-to" {
		t.Errorf("expected to 'custom-to', got %q", gotTo)
	}
}

func TestSendMessage_EmptyText(t *testing.T) {
	tool := &SendMessage{
		Deliver: func(ctx context.Context, ch, to, text string) error {
			return nil
		},
	}
	_, err := tool.Execute(context.Background(), map[string]any{
		"text": "",
	})
	if err == nil {
		t.Fatal("expected error for empty text")
	}
}

func TestSendMessage_DeliverError(t *testing.T) {
	tool := &SendMessage{
		Deliver: func(ctx context.Context, ch, to, text string) error {
			return errors.New("deliver failed")
		},
	}
	_, err := tool.Execute(context.Background(), map[string]any{
		"text": "test",
	})
	if err == nil {
		t.Fatal("expected error when deliver returns error")
	}
}

func TestSendMessage_Name(t *testing.T) {
	tool := &SendMessage{}
	if tool.Name() != "send_message" {
		t.Errorf("expected 'send_message', got %q", tool.Name())
	}
}

func TestSendMessage_Description(t *testing.T) {
	tool := &SendMessage{}
	if tool.Description() == "" {
		t.Error("expected non-empty description")
	}
}

func TestSendMessage_Schema(t *testing.T) {
	tool := &SendMessage{}
	schema := tool.Schema()
	if schema["type"] != "function" {
		t.Errorf("expected 'function', got %v", schema["type"])
	}
}

func TestSendMessage_TextOnlyWhitespace(t *testing.T) {
	tool := &SendMessage{
		Deliver: func(ctx context.Context, ch, to, text string) error {
			return nil
		},
	}
	_, err := tool.Execute(context.Background(), map[string]any{
		"text": "  ",
	})
	if err == nil {
		t.Fatal("expected error for whitespace-only text")
	}
}
