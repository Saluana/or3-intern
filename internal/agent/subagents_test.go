package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
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

func TestSubagentManager_EnqueueServicePersistsMetadataAndLifecycle(t *testing.T) {
	d := openRuntimeTestDB(t)
	registry := NewJobRegistry(time.Minute, 16)
	mgr := &SubagentManager{
		DB:        d,
		Jobs:      registry,
		MaxQueued: 8,
		started:   true,
		notifyCh:  make(chan struct{}, 1),
	}

	req := ServiceSubagentRequest{
		ParentSessionKey: "parent",
		Task:             "service task",
		PromptSnapshot: []providers.ChatMessage{{
			Role:    "system",
			Content: "snapshot",
		}},
		AllowedTools:   []string{"bash", "view"},
		RestrictTools:  true,
		ProfileName:    " ops ",
		Channel:        "slack",
		ReplyTo:        "user-1",
		Meta:           map[string]any{"request_id": "req-1", "workspace_id": "ws-1", "network_session_id": "net-1", "ignored": "nope"},
		Timeout:        95 * time.Second,
		ApprovalToken:  " token-1 ",
		RequesterActor: " alice ",
		RequesterRole:  " admin ",
	}

	job, err := mgr.EnqueueService(context.Background(), req)
	if err != nil {
		t.Fatalf("EnqueueService: %v", err)
	}

	req.AllowedTools[0] = "mutated"
	req.PromptSnapshot[0].Role = "user"
	req.Meta["request_id"] = "changed"

	stored, ok, err := d.GetSubagentJob(context.Background(), job.ID)
	if err != nil {
		t.Fatalf("GetSubagentJob: %v", err)
	}
	if !ok {
		t.Fatal("expected stored subagent job")
	}
	if stored.Channel != "slack" || stored.ReplyTo != "user-1" || stored.Status != db.SubagentStatusQueued {
		t.Fatalf("unexpected stored job: %#v", stored)
	}

	metadata := parseSubagentJobMetadata(stored.MetadataJSON)
	if metadata.ProfileName != "ops" || !metadata.RestrictTools || metadata.TimeoutSeconds != 95 {
		t.Fatalf("unexpected metadata core fields: %#v", metadata)
	}
	if metadata.ApprovalToken != "token-1" || metadata.RequesterActor != "alice" || metadata.RequesterRole != "admin" {
		t.Fatalf("unexpected requester metadata: %#v", metadata)
	}
	if got := metadata.AllowedTools; len(got) != 2 || got[0] != "bash" || got[1] != "view" {
		t.Fatalf("unexpected allowed tools: %#v", got)
	}
	if got := metadata.PromptSnapshot; len(got) != 1 || got[0].Role != "system" {
		t.Fatalf("unexpected prompt snapshot: %#v", got)
	}
	if metadata.ServiceMeta["request_id"] != "req-1" {
		t.Fatalf("expected cloned service metadata, got %#v", metadata.ServiceMeta)
	}
	if len(mgr.notifyCh) != 1 {
		t.Fatalf("expected enqueue to signal workers once, got %d", len(mgr.notifyCh))
	}

	snapshot, ok := registry.Snapshot(job.ID)
	if !ok {
		t.Fatal("expected queued job snapshot")
	}
	if len(snapshot.Events) != 1 || snapshot.Events[0].Type != "queued" {
		t.Fatalf("expected queued lifecycle event, got %#v", snapshot.Events)
	}
	event := snapshot.Events[0].Data
	if event["status"] != db.SubagentStatusQueued || event["child_session_key"] != job.ChildSessionKey {
		t.Fatalf("unexpected queued event payload: %#v", event)
	}
	if event["request_id"] != "req-1" || event["workspace_id"] != "ws-1" || event["network_session_id"] != "net-1" {
		t.Fatalf("expected service metadata in event payload, got %#v", event)
	}
	if _, ok := event["ignored"]; ok {
		t.Fatalf("expected non-whitelisted service metadata to stay hidden, got %#v", event)
	}
}

func TestSubagentManager_AbortPaths(t *testing.T) {
	t.Run("cancel path uses registry", func(t *testing.T) {
		d := openRuntimeTestDB(t)
		registry := NewJobRegistry(time.Minute, 16)
		job := registry.RegisterWithID("job-cancel", "subagent")
		cancelled := make(chan struct{}, 1)
		if !registry.AttachCancel(job.ID, func() {
			cancelled <- struct{}{}
		}) {
			t.Fatal("expected AttachCancel to succeed")
		}
		mgr := &SubagentManager{DB: d, Jobs: registry}

		if err := mgr.Abort(context.Background(), job.ID); err != nil {
			t.Fatalf("Abort: %v", err)
		}
		select {
		case <-cancelled:
		case <-time.After(time.Second):
			t.Fatal("expected registry cancel callback to run")
		}
	})

	t.Run("queued job aborts through db", func(t *testing.T) {
		d := openRuntimeTestDB(t)
		job := db.SubagentJob{
			ID:               "job-queued-manager-abort",
			ParentSessionKey: "parent",
			ChildSessionKey:  "parent:subagent:job-queued-manager-abort",
			Task:             "background task",
		}
		if err := d.EnqueueSubagentJob(context.Background(), job); err != nil {
			t.Fatalf("EnqueueSubagentJob: %v", err)
		}
		mgr := &SubagentManager{DB: d}

		if err := mgr.Abort(context.Background(), job.ID); err != nil {
			t.Fatalf("Abort queued: %v", err)
		}
		stored := waitForSubagentJob(t, d, job.ID, db.SubagentStatusInterrupted)
		if !strings.Contains(stored.ErrorText, "aborted before execution") {
			t.Fatalf("expected interrupted queued job, got %#v", stored)
		}
	})

	t.Run("unknown job returns not found", func(t *testing.T) {
		mgr := &SubagentManager{DB: openRuntimeTestDB(t)}
		err := mgr.Abort(context.Background(), "missing-job")
		if err == nil || !strings.Contains(err.Error(), "job not found") {
			t.Fatalf("expected job not found error, got %v", err)
		}
	})

	t.Run("running job is not abortable without cancel handle", func(t *testing.T) {
		d := openRuntimeTestDB(t)
		job := db.SubagentJob{
			ID:               "job-running-manager-abort",
			ParentSessionKey: "parent",
			ChildSessionKey:  "parent:subagent:job-running-manager-abort",
			Task:             "background task",
		}
		if err := d.EnqueueSubagentJob(context.Background(), job); err != nil {
			t.Fatalf("EnqueueSubagentJob: %v", err)
		}
		if err := d.MarkSubagentRunning(context.Background(), job.ID); err != nil {
			t.Fatalf("MarkSubagentRunning: %v", err)
		}
		mgr := &SubagentManager{DB: d}

		err := mgr.Abort(context.Background(), job.ID)
		if err == nil || !strings.Contains(err.Error(), "not abortable") {
			t.Fatalf("expected not abortable error, got %v", err)
		}
	})
}

func TestSubagentManager_DeliverCompletionFallsBackToRuntime(t *testing.T) {
	deliver := &mockDeliverer{}
	mgr := &SubagentManager{Runtime: &Runtime{Deliver: deliver}}
	job := db.SubagentJob{
		ID:      "job-runtime-deliver",
		Task:    "background task",
		Channel: "cli",
		ReplyTo: "user",
	}

	mgr.deliverCompletion(context.Background(), job, true, "all done", "", "")

	if len(deliver.messages) != 1 {
		t.Fatalf("expected runtime deliverer to receive one message, got %#v", deliver.messages)
	}
	if deliver.channel != "cli" || deliver.to != "user" || !strings.Contains(deliver.messages[0], "Background job job-runtime-deliver finished") {
		t.Fatalf("unexpected runtime delivery: channel=%q to=%q messages=%#v", deliver.channel, deliver.to, deliver.messages)
	}
}

func TestSubagentManager_ConcurrentEnqueueSignalsWithoutBlocking(t *testing.T) {
	d := openRuntimeTestDB(t)
	mgr := &SubagentManager{
		DB:        d,
		MaxQueued: 64,
		started:   true,
		notifyCh:  make(chan struct{}, 2),
	}

	const total = 12
	var wg sync.WaitGroup
	errCh := make(chan error, total)
	jobIDs := make(chan string, total)
	start := make(chan struct{})
	for i := 0; i < total; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			job, err := mgr.Enqueue(context.Background(), tools.SpawnRequest{
				ParentSessionKey: "parent",
				Task:             fmt.Sprintf("task-%d", i),
				Channel:          "cli",
				To:               "user",
			})
			if err != nil {
				errCh <- err
				return
			}
			jobIDs <- job.ID
		}(i)
	}
	close(start)
	wg.Wait()
	close(errCh)
	close(jobIDs)

	for err := range errCh {
		if err != nil {
			t.Fatalf("concurrent Enqueue returned error: %v", err)
		}
	}

	seen := map[string]struct{}{}
	for id := range jobIDs {
		seen[id] = struct{}{}
	}
	if len(seen) != total {
		t.Fatalf("expected %d distinct jobs, got %d", total, len(seen))
	}

	queued, err := d.ListQueuedSubagentJobs(context.Background())
	if err != nil {
		t.Fatalf("ListQueuedSubagentJobs: %v", err)
	}
	if len(queued) != total {
		t.Fatalf("expected %d queued jobs, got %d", total, len(queued))
	}
	if got := len(mgr.notifyCh); got != cap(mgr.notifyCh) {
		t.Fatalf("expected notify channel to saturate at %d, got %d", cap(mgr.notifyCh), got)
	}
}

func TestSubagentManager_WorkerLoopRecoversFromPanics(t *testing.T) {
	d := openRuntimeTestDB(t)
	deliver := &mockDeliverer{}
	if _, err := d.AppendMessage(context.Background(), "parent", "user", "start background work", nil); err != nil {
		t.Fatalf("AppendMessage parent: %v", err)
	}

	first := db.SubagentJob{
		ID:               "job-panic-first",
		ParentSessionKey: "parent",
		ChildSessionKey:  "parent:subagent:job-panic-first",
		Task:             "background task",
		Channel:          "cli",
		ReplyTo:          "user",
	}
	if err := d.EnqueueSubagentJob(context.Background(), first); err != nil {
		t.Fatalf("EnqueueSubagentJob first: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	mgr := &SubagentManager{
		DB:            d,
		Deliver:       deliver,
		MaxConcurrent: 1,
		MaxQueued:     8,
		TaskTimeout:   2 * time.Second,
		ctx:           ctx,
		cancel:        cancel,
		notifyCh:      make(chan struct{}, 1),
		started:       true,
	}
	mgr.wg = sync.WaitGroup{}
	mgr.wg.Add(1)
	go mgr.workerLoop()
	t.Cleanup(func() {
		cancel()
		done := make(chan struct{})
		go func() {
			mgr.wg.Wait()
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("workerLoop did not stop after cancel")
		}
	})
	mgr.signal()

	waitForSubagentJobStatus(t, d, first.ID, db.SubagentStatusRunning)

	providerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	mgr.Runtime = buildSimpleRuntime(t, provider, d, deliver)

	second := db.SubagentJob{
		ID:               "job-panic-second",
		ParentSessionKey: "parent",
		ChildSessionKey:  "parent:subagent:job-panic-second",
		Task:             "background task",
		Channel:          "cli",
		ReplyTo:          "user",
	}
	if err := d.EnqueueSubagentJob(context.Background(), second); err != nil {
		t.Fatalf("EnqueueSubagentJob second: %v", err)
	}
	mgr.signal()

	stored := waitForSubagentJob(t, d, second.ID, db.SubagentStatusSucceeded)
	if stored.ResultPreview == "" {
		t.Fatalf("expected recovered worker to finish second job, got %#v", stored)
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

func waitForSubagentJobStatus(t *testing.T, d *db.DB, id string, want string) db.SubagentJob {
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
		time.Sleep(10 * time.Millisecond)
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
