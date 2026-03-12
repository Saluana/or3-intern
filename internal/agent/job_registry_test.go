package agent

import (
	"context"
	"testing"
	"time"
)

func TestJobRegistry_FanoutAndWait(t *testing.T) {
	registry := NewJobRegistry(time.Minute, 16)
	job := registry.Register("turn")
	snapshotA, chA, unsubscribeA, ok := registry.Subscribe(job.ID)
	if !ok {
		t.Fatal("expected subscription to succeed")
	}
	defer unsubscribeA()
	snapshotB, chB, unsubscribeB, ok := registry.Subscribe(job.ID)
	if !ok {
		t.Fatal("expected second subscription to succeed")
	}
	defer unsubscribeB()
	if len(snapshotA.Events) != 0 || len(snapshotB.Events) != 0 {
		t.Fatalf("expected empty initial snapshots, got %#v %#v", snapshotA, snapshotB)
	}
	registry.Publish(job.ID, "started", map[string]any{"status": "running"})
	registry.Publish(job.ID, "text_delta", map[string]any{"content": "hello"})
	registry.Complete(job.ID, "completed", map[string]any{"final_text": "hello"})

	for _, ch := range []<-chan JobEvent{chA, chB} {
		seenDelta := false
		seenDone := false
		for event := range ch {
			if event.Type == "text_delta" && event.Data["content"] == "hello" {
				seenDelta = true
			}
			if event.Type == "completion" && event.Data["final_text"] == "hello" {
				seenDone = true
			}
		}
		if !seenDelta || !seenDone {
			t.Fatalf("expected fanout stream to include delta and completion; delta=%v done=%v", seenDelta, seenDone)
		}
	}

	snapshot, ok := registry.Wait(context.Background(), job.ID)
	if !ok {
		t.Fatal("expected wait to succeed")
	}
	if snapshot.Status != "completed" {
		t.Fatalf("unexpected final status: %q", snapshot.Status)
	}
}

func TestJobRegistry_CleansUpExpiredJobs(t *testing.T) {
	registry := NewJobRegistry(time.Nanosecond, 16)
	job := registry.Register("turn")
	registry.Complete(job.ID, "completed", map[string]any{"final_text": "done"})
	time.Sleep(time.Millisecond)
	registry.Register("turn")
	if _, ok := registry.Snapshot(job.ID); ok {
		t.Fatalf("expected expired job %q to be cleaned up", job.ID)
	}
}

func TestJobRegistry_DoesNotEvictLiveJobsWhenBounded(t *testing.T) {
	registry := NewJobRegistry(time.Hour, 2)
	live := registry.RegisterWithID("job-live", "turn")
	registry.Publish(live.ID, "started", map[string]any{"status": "running"})

	doneA := registry.RegisterWithID("job-done-a", "turn")
	registry.Complete(doneA.ID, "completed", map[string]any{"final_text": "a"})
	doneB := registry.RegisterWithID("job-done-b", "turn")
	registry.Complete(doneB.ID, "completed", map[string]any{"final_text": "b"})

	registry.mu.Lock()
	registry.cleanupLocked(time.Now())
	registry.mu.Unlock()

	if _, ok := registry.Snapshot(live.ID); !ok {
		t.Fatal("expected live job to remain tracked")
	}
	if len(registry.jobs) != 2 {
		t.Fatalf("expected registry to trim only terminal jobs down to limit, got %d entries", len(registry.jobs))
	}
}

func TestJobRegistry_BoundsPerJobEventHistory(t *testing.T) {
	registry := NewJobRegistry(time.Minute, 16)
	registry.maxEvents = 3
	job := registry.RegisterWithID("job-events", "turn")
	for i := 0; i < 5; i++ {
		registry.Publish(job.ID, "text_delta", map[string]any{"content": i})
	}

	snapshot, ok := registry.Snapshot(job.ID)
	if !ok {
		t.Fatal("expected snapshot to succeed")
	}
	if len(snapshot.Events) != 3 {
		t.Fatalf("expected event history to be capped at 3, got %d", len(snapshot.Events))
	}
	if snapshot.Events[0].Sequence != 3 || snapshot.Events[2].Sequence != 5 {
		t.Fatalf("expected trimmed history to retain newest sequence numbers, got %#v", snapshot.Events)
	}
}
