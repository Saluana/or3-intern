package triggers

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"or3-intern/internal/bus"
	"or3-intern/internal/config"
)

func newTestFileWatcher(t *testing.T, paths []string, pollSec, debounceSec int) (*FileWatcher, *bus.Bus) {
	t.Helper()
	b := bus.New(16)
	cfg := config.FileWatchConfig{
		Enabled:         true,
		Paths:           paths,
		PollSeconds:     pollSec,
		DebounceSeconds: debounceSec,
	}
	fw := NewFileWatcher(cfg, b, "test-session")
	return fw, b
}

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
	fw.poll(nil)

	// Modify file
	time.Sleep(10 * time.Millisecond) // ensure mtime difference
	if err := os.WriteFile(filePath, []byte("changed content"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Poll again - should publish event now
	fw.poll(nil)

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
	fw.poll(nil)

	// First change - should publish
	time.Sleep(10 * time.Millisecond)
	if err := os.WriteFile(filePath, []byte("v2"), 0o644); err != nil {
		t.Fatal(err)
	}
	fw.poll(nil)

	// Second change very quickly (within debounce) - should NOT publish
	time.Sleep(10 * time.Millisecond)
	if err := os.WriteFile(filePath, []byte("v3"), 0o644); err != nil {
		t.Fatal(err)
	}
	fw.poll(nil)

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
	fw.poll(nil)

	select {
	case ev := <-b.Channel():
		t.Errorf("unexpected event on first poll: %+v", ev)
	default:
		// correct: no event
	}
}
