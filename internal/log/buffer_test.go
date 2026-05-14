package log

import (
	"testing"
	"time"
)

func TestBufferCapsSnapshotAndNotifiesSubscribers(t *testing.T) {
	buffer := NewBuffer(2)
	entries, unsubscribe := buffer.Subscribe(1)
	defer unsubscribe()

	buffer.Append(Entry{Level: LevelInfo, Component: "service", Message: "first"})
	buffer.Append(Entry{Level: LevelWarn, Component: "service", Message: "second"})
	buffer.Append(Entry{Level: LevelError, Component: "service", Message: "third trace=trace-a session=session-a"})

	snapshot := buffer.Snapshot(Filter{MinLevel: LevelInfo})
	if len(snapshot) != 2 {
		t.Fatalf("expected capped snapshot of 2 entries, got %d", len(snapshot))
	}
	if snapshot[0].Message != "second" || snapshot[1].Message != "third trace=trace-a session=session-a" {
		t.Fatalf("unexpected snapshot order: %#v", snapshot)
	}
	if snapshot[1].TraceID != "trace-a" || snapshot[1].Session != "session-a" {
		t.Fatalf("expected trace/session to be extracted, got %#v", snapshot[1])
	}

	select {
	case entry := <-entries:
		if entry.Message == "" {
			t.Fatalf("expected subscriber entry, got %#v", entry)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for subscriber entry")
	}
}

func TestFilterMatchesLevelComponentTraceAndSession(t *testing.T) {
	entry := Entry{
		Level:     LevelWarn,
		Component: "service_turn",
		Message:   "approval required",
		TraceID:   "trace-a",
		Session:   "session-a",
	}
	filter := Filter{MinLevel: LevelInfo, Component: "service_turn", TraceID: "trace-a", Session: "session-a"}
	if !filter.Matches(entry) {
		t.Fatal("expected matching filter")
	}
	filter.TraceID = "trace-b"
	if filter.Matches(entry) {
		t.Fatal("expected mismatched trace to be rejected")
	}
	filter = Filter{MinLevel: LevelError}
	if filter.Matches(entry) {
		t.Fatal("expected warn entry to be below error filter")
	}
}
