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
