package triggers

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"or3-intern/internal/bus"
	"or3-intern/internal/config"
)

func TestFileWatcherPublishesChange(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "watched.txt")
	if err := os.WriteFile(filePath, []byte("initial"), 0o644); err != nil {
		t.Fatal(err)
	}

	b := bus.New(16)
	cfg := config.FileWatchConfig{
		Enabled:         true,
		Paths:           []string{filePath},
		PollSeconds:     1,
		DebounceSeconds: 0,
	}
	fw := NewFileWatcher(cfg, b, "test-session")

	// Manually do first poll to establish baseline
	fw.poll(context.Background())

	// Modify file
	time.Sleep(10 * time.Millisecond) // ensure mtime difference
	if err := os.WriteFile(filePath, []byte("changed content"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Poll again - should publish event now
	fw.poll(context.Background())

	select {
	case ev := <-b.Channel():
		if ev.Type != "file_change" {
			t.Errorf("expected EventFileChange, got %q", ev.Type)
		}
		if ev.SessionKey != "test-session" {
			t.Errorf("expected 'test-session', got %q", ev.SessionKey)
		}
		absPath, _ := filepath.Abs(filePath)
		if ev.From != absPath {
			t.Errorf("expected From=%q, got %q", absPath, ev.From)
		}
		structured, ok := ev.Meta[MetaKeyStructuredEvent].(map[string]any)
		if !ok || structured["source"] != "filewatch" {
			t.Fatalf("expected structured filewatch metadata, got %#v", ev.Meta)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for file change event")
	}
}

func TestFileWatcherDebounce(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "debounce.txt")
	if err := os.WriteFile(filePath, []byte("v1"), 0o644); err != nil {
		t.Fatal(err)
	}

	b := bus.New(16)
	cfg := config.FileWatchConfig{
		Enabled:         true,
		Paths:           []string{filePath},
		PollSeconds:     1,
		DebounceSeconds: 60, // very large debounce window
	}
	fw := NewFileWatcher(cfg, b, "test-session")

	// Establish baseline
	fw.poll(context.Background())

	// First change - should publish
	time.Sleep(10 * time.Millisecond)
	if err := os.WriteFile(filePath, []byte("v2"), 0o644); err != nil {
		t.Fatal(err)
	}
	fw.poll(context.Background())

	// Second change very quickly (within debounce) - should NOT publish
	time.Sleep(10 * time.Millisecond)
	if err := os.WriteFile(filePath, []byte("v3"), 0o644); err != nil {
		t.Fatal(err)
	}
	fw.poll(context.Background())

	// Should have exactly one event
	count := 0
drain:
	for {
		select {
		case <-b.Channel():
			count++
		default:
			break drain
		}
	}
	if count != 1 {
		t.Errorf("expected 1 event (debounce), got %d", count)
	}
}

func TestFileWatcherFirstObservationNoEvent(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "baseline.txt")
	if err := os.WriteFile(filePath, []byte("initial"), 0o644); err != nil {
		t.Fatal(err)
	}

	b := bus.New(16)
	cfg := config.FileWatchConfig{
		Enabled:         true,
		Paths:           []string{filePath},
		PollSeconds:     1,
		DebounceSeconds: 0,
	}
	fw := NewFileWatcher(cfg, b, "test-session")

	// First poll - just baseline, no event
	fw.poll(context.Background())

	select {
	case ev := <-b.Channel():
		t.Errorf("unexpected event on first poll: %+v", ev)
	default:
		// correct: no event
	}
}

func TestFileWatcherPublishesStructuredTasksForChangedFile(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "structured.json")
	if err := os.WriteFile(filePath, []byte("initial"), 0o644); err != nil {
		t.Fatal(err)
	}

	b := bus.New(16)
	fw := NewFileWatcher(config.FileWatchConfig{
		Enabled:         true,
		Paths:           []string{filePath},
		PollSeconds:     1,
		DebounceSeconds: 0,
	}, b, "test-session")

	fw.poll(context.Background())
	time.Sleep(10 * time.Millisecond)
	if err := os.WriteFile(filePath, []byte(`{"tasks":[{"tool":"echo_tool"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	fw.poll(context.Background())

	select {
	case ev := <-b.Channel():
		structuredTasks, ok := ev.Meta[MetaKeyStructuredTasks].(map[string]any)
		if !ok {
			t.Fatalf("expected structured tasks metadata, got %#v", ev.Meta)
		}
		first, ok := firstStructuredFileTask(structuredTasks)
		if !ok || first["tool"] != "echo_tool" {
			t.Fatalf("expected echo_tool task, got %#v", structuredTasks)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for file change event")
	}
}

func firstStructuredFileTask(structuredTasks map[string]any) (map[string]any, bool) {
	if rawTasks, ok := structuredTasks["tasks"].([]any); ok && len(rawTasks) > 0 {
		first, ok := rawTasks[0].(map[string]any)
		return first, ok
	}
	if rawTasks, ok := structuredTasks["tasks"].([]map[string]any); ok && len(rawTasks) > 0 {
		return rawTasks[0], true
	}
	return nil, false
}
