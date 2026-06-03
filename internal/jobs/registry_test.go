package jobs

import (
	"context"
	"testing"
	"time"
)

func TestRegistryFanoutAndWait(t *testing.T) {
	registry := NewRegistry(time.Minute, 16)
	job := registry.Register("turn")
	_, events, unsubscribe, ok := registry.Subscribe(job.ID)
	if !ok {
		t.Fatal("expected subscription")
	}
	defer unsubscribe()

	registry.Publish(job.ID, "text_delta", map[string]any{"content": "hello"})
	registry.Complete(job.ID, "completed", map[string]any{"final_text": "done"})

	seenDelta := false
	seenDone := false
	for event := range events {
		if event.Type == "text_delta" && event.Data["content"] == "hello" {
			seenDelta = true
		}
		if event.Type == "completion" && event.Data["final_text"] == "done" {
			seenDone = true
		}
	}
	if !seenDelta || !seenDone {
		t.Fatalf("expected delta and completion events; delta=%v done=%v", seenDelta, seenDone)
	}

	snapshot, ok := registry.Wait(context.Background(), job.ID)
	if !ok {
		t.Fatal("expected wait to succeed")
	}
	if snapshot.Status != "completed" {
		t.Fatalf("expected completed status, got %q", snapshot.Status)
	}
}

func TestRegistryRetentionKeepsLiveJobsWhenBounded(t *testing.T) {
	registry := NewRegistry(time.Hour, 2)
	live := registry.RegisterWithID("job-live", "turn")
	registry.Publish(live.ID, "started", map[string]any{"status": "running"})

	doneA := registry.RegisterWithID("job-done-a", "turn")
	registry.Complete(doneA.ID, "completed", nil)
	doneB := registry.RegisterWithID("job-done-b", "turn")
	registry.Complete(doneB.ID, "completed", nil)

	registry.CleanupForTest(time.Now())

	if _, ok := registry.Snapshot(live.ID); !ok {
		t.Fatal("expected live job to remain tracked")
	}
	if got := registry.TrackedCountForTest(); got != 2 {
		t.Fatalf("expected bounded registry to retain 2 jobs, got %d", got)
	}
}
