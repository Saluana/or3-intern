package heartbeat

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"or3-intern/internal/bus"
	"or3-intern/internal/config"
)

func TestServiceStartStopDisabledNoop(t *testing.T) {
	svc := New(config.HeartbeatConfig{Enabled: false}, "", bus.New(1))
	svc.Start(context.Background())
	svc.Stop()
}

func TestServiceProcessTickPublishesHeartbeatEvent(t *testing.T) {
	dir := t.TempDir()
	tasksPath := filepath.Join(dir, "HEARTBEAT.md")
	if err := os.WriteFile(tasksPath, []byte("# Heartbeat\n- run checks"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	eventBus := bus.New(1)
	svc := New(config.HeartbeatConfig{
		Enabled:         true,
		IntervalMinutes: 5,
		TasksFile:       tasksPath,
		SessionKey:      "heartbeat:test",
	}, "", eventBus)

	svc.processTick()

	select {
	case ev := <-eventBus.Channel():
		if ev.Type != bus.EventHeartbeat {
			t.Fatalf("expected heartbeat event, got %q", ev.Type)
		}
		if ev.SessionKey != "heartbeat:test" {
			t.Fatalf("expected session key heartbeat:test, got %q", ev.SessionKey)
		}
		if ev.Channel != DefaultChannel || ev.From != DefaultFrom {
			t.Fatalf("unexpected delivery metadata: %q/%q", ev.Channel, ev.From)
		}
		if ev.Message != SeedMessage {
			t.Fatalf("unexpected seed message: %q", ev.Message)
		}
		if ev.Meta[MetaKeyHeartbeat] != true {
			t.Fatalf("expected heartbeat meta flag, got %#v", ev.Meta)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("expected heartbeat event to be published")
	}
}

func TestServiceProcessTickUsesWorkspaceFallback(t *testing.T) {
	workspace := t.TempDir()
	workspaceHeartbeat := filepath.Join(workspace, "HEARTBEAT.md")
	if err := os.WriteFile(workspaceHeartbeat, []byte("- workspace task"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	eventBus := bus.New(1)
	svc := New(config.HeartbeatConfig{
		Enabled:         true,
		IntervalMinutes: 5,
		TasksFile:       filepath.Join(t.TempDir(), "missing.md"),
	}, workspace, eventBus)

	svc.processTick()

	select {
	case ev := <-eventBus.Channel():
		if got := ev.Meta["tasks_path"]; got != workspaceHeartbeat {
			t.Fatalf("expected workspace fallback path %q, got %#v", workspaceHeartbeat, got)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("expected heartbeat event to be published from workspace fallback")
	}
}

func TestServiceProcessTickSkipsMissingAndCommentOnlyFiles(t *testing.T) {
	eventBus := bus.New(1)
	var logs []string
	svc := New(config.HeartbeatConfig{
		Enabled:         true,
		IntervalMinutes: 5,
		TasksFile:       filepath.Join(t.TempDir(), "missing.md"),
	}, "", eventBus)
	svc.logf = func(format string, args ...any) {
		logs = append(logs, format)
	}
	svc.processTick()
	if len(logs) == 0 {
		t.Fatal("expected missing file log entry")
	}

	dir := t.TempDir()
	commentOnly := filepath.Join(dir, "HEARTBEAT.md")
	if err := os.WriteFile(commentOnly, []byte("# Header\n<!-- note -->"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	svc.Config.TasksFile = commentOnly
	logs = nil
	svc.processTick()
	if len(logs) != 0 {
		t.Fatalf("expected comment-only file to skip quietly, got %#v", logs)
	}
	select {
	case ev := <-eventBus.Channel():
		t.Fatalf("unexpected event published: %#v", ev)
	default:
	}
}

func TestServiceCoalescesPendingAndAvoidsOverlap(t *testing.T) {
	dir := t.TempDir()
	tasksPath := filepath.Join(dir, "HEARTBEAT.md")
	if err := os.WriteFile(tasksPath, []byte("- run checks"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	eventBus := bus.New(2)
	svc := New(config.HeartbeatConfig{
		Enabled:         true,
		IntervalMinutes: 5,
		TasksFile:       tasksPath,
	}, "", eventBus)
	svc.tickQueue = make(chan struct{}, 1)

	if !svc.enqueueTick("test") {
		t.Fatal("expected first tick enqueue to succeed")
	}
	if svc.enqueueTick("test") {
		t.Fatal("expected second tick enqueue to be dropped")
	}

	svc.processTick()
	select {
	case ev := <-eventBus.Channel():
		done, ok := ev.Meta[MetaKeyDone].(func())
		if !ok || done == nil {
			t.Fatalf("expected completion callback in meta, got %#v", ev.Meta)
		}
	default:
		t.Fatal("expected heartbeat event")
	}

	svc.processTick()
	select {
	case ev := <-eventBus.Channel():
		t.Fatalf("expected overlapping tick to be skipped, got %#v", ev)
	default:
	}
}

func TestServiceRunPublisherDropsBufferedTickDuringShutdown(t *testing.T) {
	dir := t.TempDir()
	tasksPath := filepath.Join(dir, "HEARTBEAT.md")
	if err := os.WriteFile(tasksPath, []byte("- run checks"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	eventBus := bus.New(1)
	svc := New(config.HeartbeatConfig{
		Enabled:         true,
		IntervalMinutes: 5,
		TasksFile:       tasksPath,
	}, "", eventBus)
	svc.tickQueue = make(chan struct{}, 1)
	svc.tickQueue <- struct{}{}
	svc.stopping.Store(true)
	svc.wg.Add(1)

	svc.runPublisher(context.Background())

	select {
	case ev := <-eventBus.Channel():
		t.Fatalf("expected buffered tick to be dropped during shutdown, got %#v", ev)
	default:
	}
}

func TestHasActiveInstructions(t *testing.T) {
	if HasActiveInstructions("# Header\n<!-- note -->\n") {
		t.Fatal("expected headings and comments only to be inactive")
	}
	if !HasActiveInstructions("# Header\n- task\n") {
		t.Fatal("expected list item to count as active instructions")
	}
}

func TestLoadTasksFileUsesWorkspaceFallback(t *testing.T) {
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "HEARTBEAT.md"), []byte("- task"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	path, text, err := LoadTasksFile("", workspace)
	if err != nil {
		t.Fatalf("LoadTasksFile: %v", err)
	}
	if !strings.HasSuffix(path, "HEARTBEAT.md") {
		t.Fatalf("expected workspace heartbeat path, got %q", path)
	}
	if text != "- task" {
		t.Fatalf("unexpected text: %q", text)
	}
}
