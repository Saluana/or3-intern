package tools

import (
	"context"
	"errors"
	"os"
	"path/filepath"
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
		Deliver: func(ctx context.Context, ch, to, text string, meta map[string]any) error {
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
		Deliver: func(ctx context.Context, ch, to, text string, meta map[string]any) error {
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
		Deliver: func(ctx context.Context, ch, to, text string, meta map[string]any) error {
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
		Deliver: func(ctx context.Context, ch, to, text string, meta map[string]any) error {
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
		Deliver: func(ctx context.Context, ch, to, text string, meta map[string]any) error {
			return nil
		},
	}
	_, err := tool.Execute(context.Background(), map[string]any{
		"text": "  ",
	})
	if err == nil {
		t.Fatal("expected error for whitespace-only text without media")
	}
}

func TestSendMessage_MediaOnlySuccess(t *testing.T) {
	root := t.TempDir()
	mediaPath := filepath.Join(root, "image.png")
	if err := os.WriteFile(mediaPath, []byte("image-bytes"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	var gotText string
	var gotMeta map[string]any
	tool := &SendMessage{
		Deliver: func(ctx context.Context, ch, to, text string, meta map[string]any) error {
			gotText = text
			gotMeta = meta
			return nil
		},
		AllowedRoot:   root,
		MaxMediaBytes: 1024,
	}
	if _, err := tool.Execute(context.Background(), map[string]any{
		"media": []any{mediaPath},
	}); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if gotText != "" {
		t.Fatalf("expected empty text for media-only message, got %q", gotText)
	}
	wantPath, err := canonicalizePath(mediaPath)
	if err != nil {
		t.Fatalf("canonicalizePath: %v", err)
	}
	paths, ok := gotMeta["media_paths"].([]string)
	if !ok || len(paths) != 1 || paths[0] != wantPath {
		t.Fatalf("expected media_paths to be passed through, got %#v", gotMeta)
	}
}

func TestSendMessage_UsesContextDefaultsWhenKeysOmitted(t *testing.T) {
	var gotChannel, gotTo string
	tool := &SendMessage{
		Deliver: func(ctx context.Context, ch, to, text string, meta map[string]any) error {
			gotChannel = ch
			gotTo = to
			return nil
		},
	}
	ctx := ContextWithDelivery(context.Background(), "discord", "channel-1")
	if _, err := tool.Execute(ctx, map[string]any{"text": "hello"}); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if gotChannel != "discord" || gotTo != "channel-1" {
		t.Fatalf("expected context delivery target, got %q/%q", gotChannel, gotTo)
	}
}

func TestSendMessage_MissingTextDoesNotBecomeNilString(t *testing.T) {
	tool := &SendMessage{
		Deliver: func(ctx context.Context, ch, to, text string, meta map[string]any) error {
			return nil
		},
	}
	if _, err := tool.Execute(context.Background(), map[string]any{}); err == nil {
		t.Fatal("expected empty message error when text and media are both omitted")
	}
}

func TestSendMessage_MediaOutsideAllowedRoot(t *testing.T) {
	root := t.TempDir()
	other := t.TempDir()
	mediaPath := filepath.Join(other, "image.png")
	if err := os.WriteFile(mediaPath, []byte("image-bytes"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	tool := &SendMessage{
		Deliver: func(ctx context.Context, ch, to, text string, meta map[string]any) error {
			return nil
		},
		AllowedRoot:   root,
		MaxMediaBytes: 1024,
	}
	if _, err := tool.Execute(context.Background(), map[string]any{
		"text":  "hello",
		"media": []any{mediaPath},
	}); err == nil {
		t.Fatal("expected error for media outside allowed root")
	}
}
