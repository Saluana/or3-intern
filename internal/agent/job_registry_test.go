package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"or3-intern/internal/tools"
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

func TestJobRegistry_SubscribeAfterTerminalReturnsSnapshotAndClosedChannel(t *testing.T) {
	registry := NewJobRegistry(time.Minute, 16)
	job := registry.RegisterWithID("job-terminal", "turn")
	registry.Complete(job.ID, "completed", map[string]any{"final_text": "done"})

	snapshot, events, unsubscribe, ok := registry.Subscribe(job.ID)
	if !ok {
		t.Fatal("expected terminal subscription to succeed")
	}
	defer unsubscribe()
	if snapshot.Status != "completed" || len(snapshot.Events) == 0 {
		t.Fatalf("expected completed snapshot with events, got %#v", snapshot)
	}
	select {
	case _, open := <-events:
		if open {
			t.Fatal("expected terminal subscription channel to be closed")
		}
	case <-time.After(time.Second):
		t.Fatal("expected terminal subscription channel to close immediately")
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

func TestJobRegistry_PublishIgnoresTerminalJobs(t *testing.T) {
	registry := NewJobRegistry(time.Minute, 16)
	job := registry.RegisterWithID("job-terminal-guard", "turn")
	if !registry.Complete(job.ID, "completed", map[string]any{"final_text": "done"}) {
		t.Fatal("expected completion to succeed")
	}

	snapshot, ok := registry.Snapshot(job.ID)
	if !ok {
		t.Fatal("expected snapshot to succeed")
	}
	eventCount := len(snapshot.Events)
	if registry.Publish(job.ID, "late_event", map[string]any{"content": "ignored"}) {
		t.Fatal("expected publish to reject terminal job")
	}

	snapshot, ok = registry.Snapshot(job.ID)
	if !ok {
		t.Fatal("expected snapshot after terminal publish")
	}
	if len(snapshot.Events) != eventCount {
		t.Fatalf("expected late publish to leave history unchanged, got %#v", snapshot.Events)
	}
}

func TestJobObserverToolResultIncludesApprovalRequestID(t *testing.T) {
	registry := NewJobRegistry(0, 0)
	job := registry.Register("turn")
	observer := registry.Observer(job.ID)

	observer.OnToolResult(context.Background(), "exec", "", &tools.ApprovalRequiredError{
		ToolName:  "exec",
		RequestID: 42,
	})

	snapshot, ok := registry.Snapshot(job.ID)
	if !ok {
		t.Fatal("expected job snapshot")
	}
	if len(snapshot.Events) != 1 {
		t.Fatalf("expected one event, got %d", len(snapshot.Events))
	}
	data := snapshot.Events[0].Data
	if data["code"] != "approval_required" {
		t.Fatalf("expected approval_required code, got %+v", data)
	}
	if data["request_id"] != int64(42) {
		t.Fatalf("expected request_id 42, got %+v", data)
	}
	if data["approval_id"] != int64(42) {
		t.Fatalf("expected approval_id 42, got %+v", data)
	}
}

func TestJobObserverToolLifecyclePublishesEnrichedPayloads(t *testing.T) {
	registry := NewJobRegistry(0, 0)
	job := registry.Register("turn")
	observer := registry.Observer(job.ID)
	lifecycle, ok := observer.(ToolLifecycleObserver)
	if !ok {
		t.Fatal("expected lifecycle observer")
	}

	lifecycle.OnToolLifecycle(context.Background(), ToolLifecycleEvent{
		ToolCallID:       "call_1",
		Name:             "read_file",
		Status:           "running",
		Arguments:        `{"path":"README.md"}`,
		ArgumentsPreview: `{"path":"README.md"}`,
	})
	lifecycle.OnToolLifecycle(context.Background(), ToolLifecycleEvent{
		ToolCallID:    "call_1",
		Name:          "read_file",
		Status:        "completed",
		ResultPreview: "ok",
		ArtifactID:    "artifact_1",
	})

	snapshot, ok := registry.Snapshot(job.ID)
	if !ok || len(snapshot.Events) != 2 {
		t.Fatalf("expected two events, ok=%v snapshot=%#v", ok, snapshot)
	}
	call := snapshot.Events[0].Data
	if call["tool_call_id"] != "call_1" || call["status"] != "running" || call["arguments_preview"] == "" {
		t.Fatalf("unexpected call event: %#v", call)
	}
	result := snapshot.Events[1].Data
	if result["artifact_id"] != "artifact_1" || result["result_preview"] != "ok" {
		t.Fatalf("unexpected result event: %#v", result)
	}
}

func TestJobObserverPublishesConversationEvents(t *testing.T) {
	registry := NewJobRegistry(time.Minute, 16)
	job := registry.RegisterWithID("job-observer-events", "turn")
	observer := JobObserver{registry: registry, jobID: job.ID}

	observer.OnTextDelta(context.Background(), "")
	observer.OnTextDelta(context.Background(), "hello")
	observer.OnToolCall(context.Background(), "exec", strings.Repeat("x", 520))
	observer.OnCompletion(context.Background(), "done", true)
	observer.OnError(context.Background(), errors.New("boom"))

	snapshot, ok := registry.Snapshot(job.ID)
	if !ok {
		t.Fatal("expected snapshot")
	}
	if len(snapshot.Events) != 4 {
		t.Fatalf("expected four events, got %#v", snapshot.Events)
	}
	if snapshot.Events[0].Type != "text_delta" || snapshot.Events[0].Data["content"] != "hello" {
		t.Fatalf("unexpected text delta event: %#v", snapshot.Events[0])
	}
	if snapshot.Events[1].Type != "tool_call" || snapshot.Events[1].Data["name"] != "exec" {
		t.Fatalf("unexpected tool call event: %#v", snapshot.Events[1])
	}
	if preview := fmt.Sprint(snapshot.Events[1].Data["arguments_preview"]); !strings.HasSuffix(preview, "...") {
		t.Fatalf("expected bounded tool call preview, got %q", preview)
	}
	if snapshot.Events[2].Type != "assistant" || snapshot.Events[2].Data["streamed"] != true {
		t.Fatalf("unexpected completion event: %#v", snapshot.Events[2])
	}
	if snapshot.Events[3].Type != "runtime_error" || snapshot.Events[3].Data["public_code"] != PublicErrorUnknown {
		t.Fatalf("unexpected error event: %#v", snapshot.Events[3])
	}
}

func TestJobObserverNilRegistryIsSafe(t *testing.T) {
	observer := JobObserver{jobID: "job-nil"}
	observer.OnTextDelta(context.Background(), "hello")
	observer.OnToolCall(context.Background(), "exec", "{}")
	observer.OnToolResult(context.Background(), "exec", "ok", nil)
	observer.OnCompletion(context.Background(), "done", false)
	observer.OnError(context.Background(), errors.New("boom"))
}

func TestJobRegistry_FailMarksTerminalError(t *testing.T) {
	registry := NewJobRegistry(time.Minute, 16)
	job := registry.RegisterWithID("job-fail", "turn")

	if !registry.Fail(job.ID, "boom", map[string]any{"detail": "x"}) {
		t.Fatal("expected Fail to succeed")
	}

	snapshot, ok := registry.Wait(context.Background(), job.ID)
	if !ok {
		t.Fatal("expected failed job snapshot")
	}
	if snapshot.Status != "failed" {
		t.Fatalf("expected failed status, got %#v", snapshot)
	}
	if len(snapshot.Events) != 1 || snapshot.Events[0].Type != "error" {
		t.Fatalf("expected single error event, got %#v", snapshot.Events)
	}
	if snapshot.Events[0].Data["message"] != "boom" || snapshot.Events[0].Data["detail"] != "x" {
		t.Fatalf("unexpected fail payload: %#v", snapshot.Events[0].Data)
	}
}

func TestJobRegistry_AttachCancelHandlesMissingEntry(t *testing.T) {
	registry := NewJobRegistry(time.Minute, 16)
	if registry.AttachCancel("missing", func() {}) {
		t.Fatal("expected AttachCancel to reject missing job")
	}

	job := registry.RegisterWithID("job-cancel", "turn")
	cancelled := false
	if !registry.AttachCancel(job.ID, func() { cancelled = true }) {
		t.Fatal("expected AttachCancel to succeed for existing job")
	}
	if !registry.Cancel(job.ID) {
		t.Fatal("expected Cancel to invoke attached cancel func")
	}
	if !cancelled {
		t.Fatal("expected cancel func to run")
	}
}

func TestJobRegistry_WaitReturnsFalseOnCanceledContext(t *testing.T) {
	registry := NewJobRegistry(time.Minute, 16)
	job := registry.RegisterWithID("job-wait-cancel", "turn")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if snapshot, ok := registry.Wait(ctx, job.ID); ok {
		t.Fatalf("expected canceled wait to fail, got %#v", snapshot)
	}
}

func TestJobRegistry_ConcurrentRegisterPublishSubscribe(t *testing.T) {
	registry := NewJobRegistry(time.Minute, 256)

	const workers = 10
	const eventsPerWorker = 4
	var wg sync.WaitGroup
	errCh := make(chan error, workers)
	start := make(chan struct{})
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start

			jobID := fmt.Sprintf("job-concurrent-%d", i)
			job := registry.RegisterWithID(jobID, "turn")
			if job.ID != jobID {
				errCh <- fmt.Errorf("unexpected job id %q", job.ID)
				return
			}
			_, ch, unsubscribe, ok := registry.Subscribe(jobID)
			if !ok {
				errCh <- fmt.Errorf("subscribe failed for %s", jobID)
				return
			}

			for j := 0; j < eventsPerWorker; j++ {
				registry.Publish(jobID, "text_delta", map[string]any{"content": fmt.Sprintf("%d-%d", i, j)})
			}
			registry.Complete(jobID, "completed", map[string]any{"worker": i})

			seen := 0
			for range ch {
				seen++
			}
			unsubscribe()
			if seen == 0 {
				errCh <- fmt.Errorf("expected streamed events for %s", jobID)
				return
			}
			snapshot, ok := registry.Wait(context.Background(), jobID)
			if !ok || snapshot.Status != "completed" {
				errCh <- fmt.Errorf("wait failed for %s: ok=%v snapshot=%#v", jobID, ok, snapshot)
				return
			}
			if len(snapshot.Events) != eventsPerWorker+1 {
				errCh <- fmt.Errorf("unexpected event count for %s: %d", jobID, len(snapshot.Events))
			}
		}(i)
	}

	close(start)
	wg.Wait()
	close(errCh)

	for err := range errCh {
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestJobRegistry_DoesNotEvictWhenOnlyLiveJobsRemain(t *testing.T) {
	registry := NewJobRegistry(time.Hour, 2)

	jobA := registry.RegisterWithID("job-live-a", "turn")
	jobB := registry.RegisterWithID("job-live-b", "turn")
	jobC := registry.RegisterWithID("job-live-c", "turn")

	registry.mu.Lock()
	registry.cleanupLocked(time.Now())
	size := len(registry.jobs)
	registry.mu.Unlock()

	if size != 3 {
		t.Fatalf("expected all live jobs to remain tracked, got %d", size)
	}
	for _, id := range []string{jobA.ID, jobB.ID, jobC.ID} {
		if _, ok := registry.Snapshot(id); !ok {
			t.Fatalf("expected live job %q to remain accessible", id)
		}
	}
}
