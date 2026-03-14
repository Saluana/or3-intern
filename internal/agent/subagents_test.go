package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"or3-intern/internal/artifacts"
	"or3-intern/internal/bus"
	"or3-intern/internal/db"
	"or3-intern/internal/providers"
	"or3-intern/internal/tools"
)

func TestSubagentManager_SuccessPersistsAndNotifies(t *testing.T) {
	d := openRuntimeTestDB(t)
	deliver := &mockDeliverer{}
	artifactsDir := t.TempDir()
	providerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{
				"message": map[string]any{"role": "assistant", "content": strings.Repeat("x", 64)},
			}},
		})
	}))
	defer providerServer.Close()
	provider := providers.New(providerServer.URL, "k", 5*time.Second)
	provider.HTTP = providerServer.Client()
	rt := &Runtime{
		DB:               d,
		Provider:         provider,
		Model:            "gpt-4",
		Tools:            tools.NewRegistry(),
		Builder:          &Builder{DB: d, HistoryMax: 10},
		MaxToolLoops:     2,
		MaxToolBytes:     10,
		ToolPreviewBytes: 8,
		Artifacts:        &artifacts.Store{Dir: artifactsDir, DB: d},
		Deliver:          deliver,
	}
	if _, err := d.AppendMessage(context.Background(), "parent", "user", "start background work", nil); err != nil {
		t.Fatalf("AppendMessage parent: %v", err)
	}
	mgr := &SubagentManager{DB: d, Runtime: rt, Deliver: deliver, MaxConcurrent: 1, MaxQueued: 8, TaskTimeout: 2 * time.Second}
	if err := mgr.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = mgr.Stop(context.Background()) }()

	job, err := mgr.Enqueue(context.Background(), tools.SpawnRequest{ParentSessionKey: "parent", Task: "background task", Channel: "cli", To: "user"})
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	stored := waitForSubagentJob(t, d, job.ID, db.SubagentStatusSucceeded)
	if stored.ArtifactID == "" || stored.ResultPreview == "" {
		t.Fatalf("expected artifact-backed success result, got %#v", stored)
	}
	msgs, err := d.GetLastMessages(context.Background(), "parent", 20)
	if err != nil {
		t.Fatalf("GetLastMessages parent: %v", err)
	}
	if !containsMessage(msgs, "Background job "+job.ID+" completed") {
		t.Fatalf("expected parent completion note, got %#v", msgs)
	}
	if len(deliver.messages) == 0 || !strings.Contains(deliver.messages[0], "Background job "+job.ID+" finished") {
		t.Fatalf("expected completion delivery, got %#v", deliver.messages)
	}
	childMsgs, err := d.GetLastMessages(context.Background(), job.ChildSessionKey, 20)
	if err != nil {
		t.Fatalf("GetLastMessages child: %v", err)
	}
	if len(childMsgs) < 2 {
		t.Fatalf("expected child-session history, got %#v", childMsgs)
	}
}

func TestSubagentManager_FailurePersistsAndNotifies(t *testing.T) {
	d := openRuntimeTestDB(t)
	deliver := &mockDeliverer{}
	providerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "provider down", http.StatusBadGateway)
	}))
	defer providerServer.Close()
	provider := providers.New(providerServer.URL, "k", 5*time.Second)
	provider.HTTP = providerServer.Client()
	rt := &Runtime{
		DB:           d,
		Provider:     provider,
		Model:        "gpt-4",
		Tools:        tools.NewRegistry(),
		Builder:      &Builder{DB: d, HistoryMax: 10},
		MaxToolLoops: 2,
		Deliver:      deliver,
	}
	if _, err := d.AppendMessage(context.Background(), "parent", "user", "start background work", nil); err != nil {
		t.Fatalf("AppendMessage parent: %v", err)
	}
	mgr := &SubagentManager{DB: d, Runtime: rt, Deliver: deliver, MaxConcurrent: 1, MaxQueued: 8, TaskTimeout: 2 * time.Second}
	if err := mgr.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = mgr.Stop(context.Background()) }()

	job, err := mgr.Enqueue(context.Background(), tools.SpawnRequest{ParentSessionKey: "parent", Task: "background task", Channel: "cli", To: "user"})
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	stored := waitForSubagentJob(t, d, job.ID, db.SubagentStatusFailed)
	if stored.ErrorText == "" {
		t.Fatalf("expected persisted failure error, got %#v", stored)
	}
	msgs, err := d.GetLastMessages(context.Background(), "parent", 20)
	if err != nil {
		t.Fatalf("GetLastMessages parent: %v", err)
	}
	if !containsMessage(msgs, "Background job "+job.ID+" failed") {
		t.Fatalf("expected parent failure note, got %#v", msgs)
	}
	if len(deliver.messages) == 0 || !strings.Contains(deliver.messages[0], "Background job "+job.ID+" failed") {
		t.Fatalf("expected failure delivery, got %#v", deliver.messages)
	}
}

func TestSubagentManager_DoesNotBlockForegroundTurn(t *testing.T) {
	d := openRuntimeTestDB(t)
	deliver := &mockDeliverer{}
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	providerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req providers.ChatCompletionRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		last := req.Messages[len(req.Messages)-1]
		content := contentToString(last.Content)
		if strings.Contains(content, "long task") {
			select {
			case started <- struct{}{}:
			default:
			}
			<-release
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{
				"message": map[string]any{"role": "assistant", "content": "ok"},
			}},
		})
	}))
	defer providerServer.Close()
	provider := providers.New(providerServer.URL, "k", 5*time.Second)
	provider.HTTP = providerServer.Client()
	rt := buildSimpleRuntime(t, provider, d, deliver)
	mgr := &SubagentManager{DB: d, Runtime: rt, Deliver: deliver, MaxConcurrent: 1, MaxQueued: 8, TaskTimeout: 2 * time.Second}
	if err := mgr.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = mgr.Stop(context.Background()) }()

	if _, err := mgr.Enqueue(context.Background(), tools.SpawnRequest{ParentSessionKey: "parent", Task: "long task", Channel: "cli", To: "user"}); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("expected background job to start")
	}
	start := time.Now()
	err := rt.Handle(context.Background(), bus.Event{Type: bus.EventUserMessage, SessionKey: "parent", Channel: "cli", From: "user", Message: "foreground follow-up"})
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if elapsed > 300*time.Millisecond {
		t.Fatalf("expected foreground turn to stay non-blocking, elapsed=%v", elapsed)
	}
	close(release)
}

func TestSubagentManager_TimeoutMarksFailure(t *testing.T) {
	d := openRuntimeTestDB(t)
	deliver := &mockDeliverer{}
	providerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{
				"message": map[string]any{"role": "assistant", "content": "slow"},
			}},
		})
	}))
	defer providerServer.Close()
	provider := providers.New(providerServer.URL, "k", 5*time.Second)
	provider.HTTP = providerServer.Client()
	rt := &Runtime{
		DB:           d,
		Provider:     provider,
		Model:        "gpt-4",
		Tools:        tools.NewRegistry(),
		Builder:      &Builder{DB: d, HistoryMax: 10},
		MaxToolLoops: 2,
		Deliver:      deliver,
	}
	if _, err := d.AppendMessage(context.Background(), "parent", "user", "start background work", nil); err != nil {
		t.Fatalf("AppendMessage parent: %v", err)
	}
	mgr := &SubagentManager{DB: d, Runtime: rt, Deliver: deliver, MaxConcurrent: 1, MaxQueued: 8, TaskTimeout: 50 * time.Millisecond}
	if err := mgr.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() {
		if err := mgr.Stop(context.Background()); err != nil {
			t.Fatalf("Stop: %v", err)
		}
	}()

	job, err := mgr.Enqueue(context.Background(), tools.SpawnRequest{ParentSessionKey: "parent", Task: "background task", Channel: "cli", To: "user"})
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	stored := waitForSubagentJob(t, d, job.ID, db.SubagentStatusFailed)
	if !strings.Contains(strings.ToLower(stored.ErrorText), "timeout") && !strings.Contains(strings.ToLower(stored.ErrorText), "deadline") {
		t.Fatalf("expected timeout-related failure, got %#v", stored)
	}
}

func TestSubagentManager_FinalizeFailureDoesNotDeliver(t *testing.T) {
	d := openRuntimeTestDB(t)
	deliver := &mockDeliverer{}
	job := db.SubagentJob{
		ID:               "job-finalize-fail",
		ParentSessionKey: "parent",
		ChildSessionKey:  "parent:subagent:job-finalize-fail",
		Task:             "background task",
	}
	if err := d.EnqueueSubagentJob(context.Background(), job); err != nil {
		t.Fatalf("EnqueueSubagentJob: %v", err)
	}
	mgr := &SubagentManager{DB: d, Deliver: deliver}
	mgr.finalizeJob(context.Background(), job, db.SubagentStatusSucceeded, "preview", "", "", true)
	if len(deliver.messages) != 0 {
		t.Fatalf("expected no delivery on finalize failure, got %#v", deliver.messages)
	}
	msgs, err := d.GetLastMessages(context.Background(), job.ParentSessionKey, 10)
	if err != nil {
		t.Fatalf("GetLastMessages: %v", err)
	}
	if len(msgs) != 0 {
		t.Fatalf("expected no persisted parent summary on finalize failure, got %#v", msgs)
	}
}

func TestSubagentManager_StartDoesNotHalfStartOnError(t *testing.T) {
	d := openRuntimeTestDB(t)
	mgr := &SubagentManager{DB: d, Runtime: &Runtime{}, MaxConcurrent: 2}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := mgr.Start(ctx); err == nil {
		t.Fatal("expected Start to fail with canceled context")
	}
	if mgr.started {
		t.Fatal("expected manager to remain stopped on startup failure")
	}
}

func TestSubagentManager_StartReconcilesRunningJobs(t *testing.T) {
	d := openRuntimeTestDB(t)
	if _, err := d.AppendMessage(context.Background(), "parent", "user", "start", nil); err != nil {
		t.Fatalf("AppendMessage parent: %v", err)
	}
	job := db.SubagentJob{
		ID:               "job-restart",
		ParentSessionKey: "parent",
		ChildSessionKey:  "parent:subagent:job-restart",
		Task:             "background task",
	}
	if err := d.EnqueueSubagentJob(context.Background(), job); err != nil {
		t.Fatalf("EnqueueSubagentJob: %v", err)
	}
	if err := d.MarkSubagentRunning(context.Background(), job.ID); err != nil {
		t.Fatalf("MarkSubagentRunning: %v", err)
	}
	mgr := &SubagentManager{DB: d, Runtime: &Runtime{}, MaxConcurrent: 1}
	if err := mgr.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() {
		if err := mgr.Stop(context.Background()); err != nil {
			t.Fatalf("Stop: %v", err)
		}
	}()

	stored := waitForSubagentJob(t, d, job.ID, db.SubagentStatusInterrupted)
	if !strings.Contains(stored.ErrorText, "restart") {
		t.Fatalf("expected restart reconciliation reason, got %#v", stored)
	}
	msgs, err := d.GetLastMessages(context.Background(), "parent", 10)
	if err != nil {
		t.Fatalf("GetLastMessages: %v", err)
	}
	if !containsMessage(msgs, "Background job "+job.ID+" failed") {
		t.Fatalf("expected reconciled parent summary, got %#v", msgs)
	}
}

func TestSubagentManager_AbortQueuedJobIsAtomic(t *testing.T) {
	d := openRuntimeTestDB(t)

	queued := db.SubagentJob{
		ID:               "job-queued-abort",
		ParentSessionKey: "parent",
		ChildSessionKey:  "parent:subagent:job-queued-abort",
		Task:             "background task",
	}
	if err := d.EnqueueSubagentJob(context.Background(), queued); err != nil {
		t.Fatalf("EnqueueSubagentJob queued: %v", err)
	}

	job, aborted, err := d.AbortQueuedSubagentJob(context.Background(), queued.ID, "subagent aborted before execution")
	if err != nil {
		t.Fatalf("AbortQueuedSubagentJob queued: %v", err)
	}
	if !aborted || job.Status != db.SubagentStatusInterrupted {
		t.Fatalf("expected queued job to abort atomically, got job=%#v aborted=%v", job, aborted)
	}

	running := db.SubagentJob{
		ID:               "job-running-abort",
		ParentSessionKey: "parent",
		ChildSessionKey:  "parent:subagent:job-running-abort",
		Task:             "background task",
	}
	if err := d.EnqueueSubagentJob(context.Background(), running); err != nil {
		t.Fatalf("EnqueueSubagentJob running: %v", err)
	}
	if err := d.MarkSubagentRunning(context.Background(), running.ID); err != nil {
		t.Fatalf("MarkSubagentRunning: %v", err)
	}

	job, aborted, err = d.AbortQueuedSubagentJob(context.Background(), running.ID, "subagent aborted before execution")
	if err != nil {
		t.Fatalf("AbortQueuedSubagentJob running: %v", err)
	}
	if aborted {
		t.Fatalf("expected running job not to be aborted via queued-only helper, got %#v", job)
	}
	stored, ok, err := d.GetSubagentJob(context.Background(), running.ID)
	if err != nil {
		t.Fatalf("GetSubagentJob running: %v", err)
	}
	if !ok || stored.Status != db.SubagentStatusRunning {
		t.Fatalf("expected running job to stay running, got %#v ok=%v", stored, ok)
	}
}

func waitForSubagentJob(t *testing.T, d *db.DB, id string, want string) db.SubagentJob {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		job, ok, err := d.GetSubagentJob(context.Background(), id)
		if err != nil {
			t.Fatalf("GetSubagentJob: %v", err)
		}
		if ok && job.Status == want {
			return job
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for job %s status %s", id, want)
	return db.SubagentJob{}
}

func containsMessage(msgs []db.Message, needle string) bool {
	for _, msg := range msgs {
		if strings.Contains(msg.Content, needle) {
			return true
		}
	}
	return false
}
