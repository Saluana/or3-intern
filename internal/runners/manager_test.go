package runners

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"or3-intern/internal/config"
	"or3-intern/internal/db"
	"or3-intern/internal/jobs"
)

type stubRunnerAdapter struct {
	id   RunnerID
	spec RunnerSpec
	cmd  CommandSpec
	err  error
}

type blockingNativeContinuationRuntime struct {
	fakeRuntime
	started    chan struct{}
	startOnce  sync.Once
	stopCalled chan struct{}
}

type approvalOnContextDoneRuntime struct {
	fakeRuntime
}

func (r *approvalOnContextDoneRuntime) Execute(ctx context.Context, _ NativeRuntimeExecuteRequest) (ProcessOutput, error) {
	<-ctx.Done()
	return ProcessOutput{ExitCode: -1}, errNativeApprovalRequired
}

func (r *blockingNativeContinuationRuntime) ContinuePendingTurn(ctx context.Context, _ NativeRuntimeExecuteRequest) (ProcessOutput, error) {
	r.startOnce.Do(func() { close(r.started) })
	<-ctx.Done()
	// A runtime may surface its pending request while cancellation is being
	// delivered. The manager must still finalize the run instead of pausing it
	// again after Stop/Abort.
	return ProcessOutput{ExitCode: -1, StderrPreview: ctx.Err().Error()}, errNativeApprovalRequired
}

func (r *blockingNativeContinuationRuntime) Stop(context.Context) error {
	r.stopCalled <- struct{}{}
	return nil
}

func (a *stubRunnerAdapter) ID() RunnerID        { return a.id }
func (a *stubRunnerAdapter) DisplayName() string { return a.spec.DisplayName }
func (a *stubRunnerAdapter) Spec() RunnerSpec    { return a.spec }
func (a *stubRunnerAdapter) Detect(context.Context, DetectOptions) RunnerInfo {
	return RunnerInfo{ID: string(a.id), DisplayName: a.spec.DisplayName, BinaryName: a.spec.Binary, Status: RunnerStatusAvailable}
}
func (a *stubRunnerAdapter) BuildCommand(RunnerRunRequest) (CommandSpec, error) {
	if a.err != nil {
		return CommandSpec{}, a.err
	}
	cmd := a.cmd
	if cmd.RunnerID == "" {
		cmd.RunnerID = a.id
	}
	return cmd, nil
}

func newTestManager(t *testing.T) (*Manager, *db.DB, *jobs.Registry) {
	t.Helper()
	database := openRunnerTestDB(t)
	jobs := jobs.NewRegistry(0, 0)
	manager := &Manager{
		DB:       database,
		Jobs:     jobs,
		Registry: NewDefaultRegistry(),
		Cfg: config.RunnersConfig{
			DefaultMode:           string(RunnerModeSafeEdit),
			DefaultIsolation:      string(IsolationHostWorkspaceWrite),
			DefaultTimeoutSeconds: 60,
			MaxTimeoutSeconds:     120,
			EventChunkMaxBytes:    256,
			PreviewMaxBytes:       4096,
		},
		MaxQueued: 16,
	}
	return manager, database, jobs
}

func mustInsertRunnerRun(t *testing.T, database *db.DB, run db.RunnerRun) db.RunnerRun {
	t.Helper()
	if run.ID == "" {
		run.ID = fmt.Sprintf("acr-%d", time.Now().UnixNano())
	}
	if run.JobID == "" {
		run.JobID = fmt.Sprintf("job-%d", time.Now().UnixNano())
	}
	if run.ParentSessionKey == "" {
		run.ParentSessionKey = "parent-session"
	}
	if run.RunnerID == "" {
		run.RunnerID = string(RunnerOpenCode)
	}
	if run.Task == "" {
		run.Task = "test task"
	}
	if run.Cwd == "" {
		run.Cwd = t.TempDir()
	}
	if run.TimeoutSeconds == 0 {
		run.TimeoutSeconds = 60
	}
	if run.MetaJSON == "" {
		run.MetaJSON = "{}"
	}
	if run.RequestedAt == 0 {
		run.RequestedAt = db.NowMS()
	}
	if err := database.EnqueueRunnerRun(context.Background(), run); err != nil {
		t.Fatalf("EnqueueRunnerRun: %v", err)
	}
	return run
}

func mustGetRunnerRun(t *testing.T, database *db.DB, id string) db.RunnerRun {
	t.Helper()
	run, ok, err := database.GetRunnerRun(context.Background(), id)
	if err != nil || !ok {
		t.Fatalf("GetRunnerRun(%q): ok=%v err=%v", id, ok, err)
	}
	return run
}

func TestManagerStartStopAndReconcile(t *testing.T) {
	var nilManager *Manager
	if err := nilManager.Start(context.Background()); err == nil || !strings.Contains(err.Error(), "nil") {
		t.Fatalf("expected nil manager error, got %v", err)
	}
	if err := (&Manager{}).Start(context.Background()); err == nil || !strings.Contains(err.Error(), "db not configured") {
		t.Fatalf("expected db error, got %v", err)
	}

	database := openRunnerTestDB(t)
	jobs := jobs.NewRegistry(0, 0)
	run := mustInsertRunnerRun(t, database, db.RunnerRun{
		ID:             "acr-reconcile",
		JobID:          "job-reconcile",
		RunnerID:       string(RunnerOpenCode),
		Status:         db.RunnerRunStatusRunning,
		StartedAt:      db.NowMS(),
		TimeoutSeconds: 30,
	})
	jobs.RegisterWithID(run.JobID, "runner:opencode")

	manager := &Manager{
		DB:   database,
		Jobs: jobs,
		Cfg: config.RunnersConfig{
			DefaultMode:           string(RunnerModeSafeEdit),
			DefaultIsolation:      string(IsolationHostWorkspaceWrite),
			DefaultTimeoutSeconds: 60,
			MaxTimeoutSeconds:     120,
		},
	}
	if err := manager.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = manager.Stop(context.Background()) })
	if err := manager.Start(context.Background()); err != nil {
		t.Fatalf("Start second call: %v", err)
	}
	if manager.Process == nil || manager.Registry == nil {
		t.Fatalf("expected process and registry initialization")
	}
	if manager.MaxConcurrent != 1 || manager.MaxQueued != 16 || manager.TaskTimeout != 900*time.Second {
		t.Fatalf("unexpected defaults: concurrent=%d queued=%d timeout=%s", manager.MaxConcurrent, manager.MaxQueued, manager.TaskTimeout)
	}
	stored := mustGetRunnerRun(t, database, run.ID)
	if stored.Status != db.RunnerRunStatusAborted || stored.ErrorMessage != "aborted by service restart" {
		t.Fatalf("unexpected reconciled run: %#v", stored)
	}
	snap, ok := jobs.Snapshot(run.JobID)
	if !ok || snap.Status != "aborted" {
		t.Fatalf("unexpected job snapshot: ok=%v snapshot=%#v", ok, snap)
	}
	if err := manager.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
}

func TestApprovalRequiredRowsAreAbortedOnRestart(t *testing.T) {
	database := openRunnerTestDB(t)
	jobsRegistry := jobs.NewRegistry(0, 0)
	run := mustInsertRunnerRun(t, database, db.RunnerRun{
		ID:       "acr-reconcile-approval",
		JobID:    "job-reconcile-approval",
		RunnerID: string(RunnerCodex),
		Status:   db.RunnerRunStatusApprovalRequired,
	})
	jobsRegistry.RegisterWithID(run.JobID, "runner:codex")
	sess, err := database.CreateOrGetRunnerChatSession(context.Background(), db.RunnerChatSession{
		ID:               "rcs-reconcile-approval",
		AppSessionKey:    "app-reconcile-approval",
		RunnerID:         string(RunnerCodex),
		ContinuationMode: string(ContinuationNative),
	})
	if err != nil {
		t.Fatalf("CreateOrGetRunnerChatSession: %v", err)
	}
	turn, err := database.CreateRunnerChatTurn(context.Background(), db.RunnerChatTurn{
		ID:               "rct-reconcile-approval",
		SessionID:        sess.ID,
		Status:           db.RunnerChatTurnStatusApprovalRequired,
		UserMessage:      "waiting",
		ContinuationMode: string(ContinuationNative),
	})
	if err != nil {
		t.Fatalf("CreateRunnerChatTurn: %v", err)
	}

	manager := &Manager{
		DB:   database,
		Jobs: jobsRegistry,
		Cfg: config.RunnersConfig{
			DefaultMode:           string(RunnerModeSafeEdit),
			DefaultIsolation:      string(IsolationHostWorkspaceWrite),
			DefaultTimeoutSeconds: 60,
			MaxTimeoutSeconds:     120,
		},
	}
	if err := manager.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := manager.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	finalRun := mustGetRunnerRun(t, database, run.ID)
	if finalRun.Status != db.RunnerRunStatusAborted || finalRun.ErrorMessage != "aborted by service restart" {
		t.Fatalf("runner approval row not reconciled: %#v", finalRun)
	}

	chatManager := &ChatManager{DB: database}
	if err := chatManager.ReconcileOnStartup(context.Background()); err != nil {
		t.Fatalf("ReconcileOnStartup: %v", err)
	}
	finalTurn, err := database.GetRunnerChatTurn(context.Background(), turn.ID)
	if err != nil {
		t.Fatalf("GetRunnerChatTurn: %v", err)
	}
	if finalTurn.Status != db.RunnerChatTurnStatusAborted || finalTurn.ErrorMessage != "service restarted" {
		t.Fatalf("chat approval row not reconciled: %#v", finalTurn)
	}
}

func TestManagerRecoverRunPanicFinalizesRun(t *testing.T) {
	manager, database, jobs := newTestManager(t)
	run := mustInsertRunnerRun(t, database, db.RunnerRun{
		ID:     "acr-panic",
		JobID:  "job-panic",
		Status: db.RunnerRunStatusRunning,
	})
	jobs.RegisterWithID(run.JobID, "agent-run")

	func() {
		defer manager.recoverRunPanic(run)
		panic("boom")
	}()

	stored := mustGetRunnerRun(t, database, run.ID)
	if stored.Status != db.RunnerRunStatusFailed {
		t.Fatalf("expected recovered panic to finalize failed, got %q", stored.Status)
	}
	if !strings.Contains(stored.ErrorMessage, "internal failure") {
		t.Fatalf("expected redacted recovery message, got %q", stored.ErrorMessage)
	}
	snapshot, ok := jobs.Snapshot(run.JobID)
	if !ok || snapshot.Status != db.RunnerRunStatusFailed {
		t.Fatalf("expected job registry to be failed, ok=%v snapshot=%#v", ok, snapshot)
	}
}

func TestManagerStopTimeout(t *testing.T) {
	manager := &Manager{DB: openRunnerTestDB(t), started: true, cancel: func() {}}
	unblock := make(chan struct{})
	manager.wg.Add(1)
	go func() {
		<-unblock
		manager.wg.Done()
	}()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := manager.Stop(ctx); !errors.Is(err, context.Canceled) {
		close(unblock)
		t.Fatalf("expected context canceled, got %v", err)
	}
	close(unblock)
}

func TestManagerPauseApprovalMovesRunOutOfRunningAndAbortFinalizes(t *testing.T) {
	manager, database, jobsRegistry := newTestManager(t)
	run := mustInsertRunnerRun(t, database, db.RunnerRun{
		ID:     "acr-approval-pause",
		JobID:  "job-approval-pause",
		Status: db.RunnerRunStatusRunning,
	})
	jobsRegistry.RegisterWithID(run.JobID, "runner:codex")

	manager.pauseRunForNativeApproval(run, ProcessOutput{ExitCode: -1, StderrPreview: "approval required"})
	paused := mustGetRunnerRun(t, database, run.ID)
	if paused.Status != db.RunnerRunStatusApprovalRequired {
		t.Fatalf("expected approval_required run, got %q", paused.Status)
	}
	if err := manager.Abort(context.Background(), run.JobID); err != nil {
		t.Fatalf("Abort paused run: %v", err)
	}
	final := mustGetRunnerRun(t, database, run.ID)
	if final.Status != db.RunnerRunStatusAborted {
		t.Fatalf("expected paused run aborted, got %q", final.Status)
	}
}

func TestManagerStopCancelsTrackedApprovalContinuation(t *testing.T) {
	manager, database, jobsRegistry := newTestManager(t)
	runtime := &blockingNativeContinuationRuntime{
		fakeRuntime: fakeRuntime{id: RunnerCodex},
		started:     make(chan struct{}),
		stopCalled:  make(chan struct{}, 1),
	}
	registry := &RunnerRuntimeRegistry{}
	registry.Register(runtime)
	manager.Runtimes = registry
	metaJSON, err := json.Marshal(map[string]any{
		"runner_chat_session_id":        "chat-session",
		"runner_chat_turn_id":           "chat-turn",
		"runner_chat_continuation_mode": string(ContinuationNative),
		"runner_chat_user_message":      "continue",
	})
	if err != nil {
		t.Fatalf("marshal run metadata: %v", err)
	}
	run := mustInsertRunnerRun(t, database, db.RunnerRun{
		ID:             "acr-approval-continuation",
		JobID:          "job-approval-continuation",
		RunnerID:       string(RunnerCodex),
		Status:         db.RunnerRunStatusApprovalRequired,
		MetaJSON:       string(metaJSON),
		TimeoutSeconds: 0,
	})
	jobsRegistry.RegisterWithID(run.JobID, "runner:codex")

	result := make(chan error, 1)
	go func() { result <- manager.ResumeNativeRunAfterApproval(context.Background(), run) }()
	select {
	case <-runtime.started:
	case <-time.After(time.Second):
		t.Fatal("native continuation did not start")
	}

	stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := manager.Stop(stopCtx); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	select {
	case <-result:
	case <-time.After(time.Second):
		t.Fatal("native continuation did not exit after Stop")
	}
	select {
	case <-runtime.stopCalled:
	case <-time.After(time.Second):
		t.Fatal("runtime Stop was not called")
	}
	final := mustGetRunnerRun(t, database, run.ID)
	if final.Status != db.RunnerRunStatusAborted {
		t.Fatalf("expected stopped continuation aborted, got %q", final.Status)
	}
	if snapshot, ok := jobsRegistry.Snapshot(run.JobID); !ok || snapshot.Status != "aborted" {
		t.Fatalf("expected stopped continuation job aborted, ok=%v snapshot=%#v", ok, snapshot)
	}
}

func TestManagerTimeoutWhileApprovalIsPendingFinalizesTimedOut(t *testing.T) {
	manager, database, jobsRegistry := newTestManager(t)
	runtime := &approvalOnContextDoneRuntime{fakeRuntime: fakeRuntime{id: RunnerCodex}}
	registry := &RunnerRuntimeRegistry{}
	registry.Register(runtime)
	manager.Runtimes = registry
	manager.Cfg.RuntimeMode = map[string]string{string(RunnerCodex): string(RuntimeModeNative)}
	manager.ctx = context.Background()
	metaJSON, err := json.Marshal(map[string]any{
		"runner_chat_session_id":        "chat-session-timeout",
		"runner_chat_turn_id":           "chat-turn-timeout",
		"runner_chat_continuation_mode": string(ContinuationNative),
		"runner_chat_user_message":      "continue",
	})
	if err != nil {
		t.Fatalf("marshal run metadata: %v", err)
	}
	run := mustInsertRunnerRun(t, database, db.RunnerRun{
		ID:             "acr-approval-timeout",
		JobID:          "job-approval-timeout",
		RunnerID:       string(RunnerCodex),
		Status:         db.RunnerRunStatusRunning,
		MetaJSON:       string(metaJSON),
		TimeoutSeconds: 1,
	})
	jobsRegistry.RegisterWithID(run.JobID, "runner:codex")
	manager.executeRun(run)
	final := mustGetRunnerRun(t, database, run.ID)
	if final.Status != db.RunnerRunStatusTimedOut {
		t.Fatalf("expected timed-out run, got %q", final.Status)
	}
}

func TestManagerPersistEventReportsReplayDegradedWhenStoreFails(t *testing.T) {
	manager, database, jobsRegistry := newTestManager(t)
	run := db.RunnerRun{ID: "acr-persist-failure", JobID: "job-persist-failure"}
	jobsRegistry.RegisterWithID(run.JobID, "runner:opencode")
	_, events, unsubscribe, ok := jobsRegistry.Subscribe(run.JobID)
	if !ok {
		t.Fatal("expected job subscription")
	}
	defer unsubscribe()
	if err := database.Close(); err != nil {
		t.Fatalf("close database: %v", err)
	}

	if manager.persistEvent(run, RunnerRunEvent{Type: "output", Seq: 4, Chunk: "live"}) {
		t.Fatal("expected persistence failure")
	}
	select {
	case event := <-events:
		if event.Type != "warning" || event.Data["code"] != "runner_event_persistence_degraded" {
			t.Fatalf("expected replay degradation warning, got %#v", event)
		}
		if event.Data["message"] == "" {
			t.Fatalf("expected user-safe degradation message, got %#v", event.Data)
		}
	case <-time.After(time.Second):
		t.Fatal("expected persistence warning event")
	}
	if manager.persistEvent(run, RunnerRunEvent{Type: "output", Seq: 5, Chunk: "still live"}) {
		t.Fatal("expected degraded persistence to remain disabled for this run")
	}
	select {
	case event := <-events:
		t.Fatalf("did not expect repeated persistence warning, got %#v", event)
	default:
	}
}

func TestManagerPersistEventBoundsOversizedOutputAndWarnsOnce(t *testing.T) {
	manager, database, jobsRegistry := newTestManager(t)
	manager.Cfg.MaxPersistedOutputBytes = 128
	run := mustInsertRunnerRun(t, database, db.RunnerRun{ID: "acr-budget-single", JobID: "job-budget-single", Status: db.RunnerRunStatusRunning})
	jobsRegistry.RegisterWithID(run.JobID, "runner:test")
	_, events, unsubscribe, ok := jobsRegistry.Subscribe(run.JobID)
	if !ok {
		t.Fatal("expected job subscription")
	}
	defer unsubscribe()

	if !manager.persistEvent(run, RunnerRunEvent{Type: "output", Seq: 1, Stream: "stdout", Chunk: strings.Repeat("x", 512)}) {
		t.Fatal("expected bounded oversized event to retain a durable marker")
	}
	stored, err := database.ListRunnerRunEvents(context.Background(), run.JobID, 0, 10)
	if err != nil {
		t.Fatalf("ListRunnerRunEvents: %v", err)
	}
	if len(stored) != 1 || stored[0].Type != "output_truncated" {
		t.Fatalf("expected output_truncated event, got %#v", stored)
	}
	if got := len(stored[0].Chunk) + len(stored[0].PayloadJSON); got > 128 {
		t.Fatalf("persisted event used %d bytes, cap is 128", got)
	}
	var marker map[string]any
	if err := json.Unmarshal([]byte(stored[0].PayloadJSON), &marker); err != nil || marker["truncated"] != true {
		t.Fatalf("invalid truncation marker: payload=%q err=%v", stored[0].PayloadJSON, err)
	}
	select {
	case event := <-events:
		if event.Type != "output_truncated" {
			t.Fatalf("expected output_truncated warning, got %#v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("expected truncation warning")
	}
	manager.persistEvent(run, RunnerRunEvent{Type: "output", Seq: 2, Stream: "stdout", Chunk: strings.Repeat("y", 512)})
	select {
	case event := <-events:
		t.Fatalf("unexpected repeated truncation warning: %#v", event)
	default:
	}
}

func TestManagerPersistEventAggregateBudgetPreservesCompletion(t *testing.T) {
	manager, database, jobsRegistry := newTestManager(t)
	manager.Cfg.MaxPersistedOutputBytes = 64
	run := mustInsertRunnerRun(t, database, db.RunnerRun{ID: "acr-budget-aggregate", JobID: "job-budget-aggregate", Status: db.RunnerRunStatusRunning})
	jobsRegistry.RegisterWithID(run.JobID, "runner:test")
	manager.persistEvent(run, RunnerRunEvent{Type: "output", Seq: 1, Stream: "stdout", Chunk: strings.Repeat("a", 48)})
	manager.persistEvent(run, RunnerRunEvent{Type: "structured", Seq: 2, Payload: json.RawMessage(strings.Repeat("{", 100))})
	if !manager.persistEvent(run, RunnerRunEvent{Type: "completion", Seq: 3, Status: db.RunnerRunStatusSucceeded, Payload: json.RawMessage(`{"exit_code":0}`)}) {
		t.Fatal("completion should remain durable after output budget exhaustion")
	}
	stored, err := database.ListRunnerRunEvents(context.Background(), run.JobID, 0, 10)
	if err != nil {
		t.Fatalf("ListRunnerRunEvents: %v", err)
	}
	var total int
	var completion bool
	for _, event := range stored {
		total += len(event.Chunk) + len(event.PayloadJSON)
		if event.Type == "completion" {
			completion = true
		}
	}
	if total > 64 {
		t.Fatalf("budgeted durable bytes=%d, cap is 64", total)
	}
	if !completion {
		t.Fatalf("completion event missing from durable history: %#v", stored)
	}
}

func TestManagerPersistEventBudgetStateCleansUpAtFinalization(t *testing.T) {
	manager, database, _ := newTestManager(t)
	manager.Cfg.MaxPersistedOutputBytes = 64
	run := mustInsertRunnerRun(t, database, db.RunnerRun{ID: "acr-budget-cleanup", JobID: "job-budget-cleanup", Status: db.RunnerRunStatusRunning})
	manager.persistEvent(run, RunnerRunEvent{Type: "output", Seq: 1, Chunk: strings.Repeat("z", 48)})
	manager.mu.Lock()
	if len(manager.eventPersistenceStates) != 1 {
		manager.mu.Unlock()
		t.Fatalf("expected one active budget state, got %d", len(manager.eventPersistenceStates))
	}
	manager.mu.Unlock()
	manager.finalizeRun(context.Background(), run, db.RunnerRunStatusSucceeded, "", ProcessOutput{ExitCode: 0})
	manager.mu.Lock()
	defer manager.mu.Unlock()
	if len(manager.eventPersistenceStates) != 0 {
		t.Fatalf("budget state leaked after finalization: %d", len(manager.eventPersistenceStates))
	}
}

func TestManagerReconcileInterruptedRunCleansApprovalPersistenceState(t *testing.T) {
	manager, database, _ := newTestManager(t)
	run := mustInsertRunnerRun(t, database, db.RunnerRun{
		ID:     "acr-budget-reconcile-cleanup",
		JobID:  "job-budget-reconcile-cleanup",
		Status: db.RunnerRunStatusApprovalRequired,
	})
	manager.eventPersistenceState(run.ID)
	manager.eventPersistenceWarnings.Store(run.ID, struct{}{})

	manager.reconcileInterruptedRun(run, "aborted by service restart")
	manager.mu.Lock()
	defer manager.mu.Unlock()
	if len(manager.eventPersistenceStates) != 0 {
		t.Fatalf("budget state leaked after restart reconciliation: %d", len(manager.eventPersistenceStates))
	}
	if _, warned := manager.eventPersistenceWarnings.Load(run.ID); warned {
		t.Fatal("persistence warning state leaked after restart reconciliation")
	}
}

func TestManagerEnqueueRejectsInvalidRequests(t *testing.T) {
	restrictDir := t.TempDir()
	newCaseManager := func(t *testing.T) *Manager {
		t.Helper()
		manager, _, _ := newTestManager(t)
		manager.RestrictDir = restrictDir
		manager.Cfg.MaxTimeoutSeconds = 30
		manager.Registry.detectCache[RunnerCodex] = runnerDetectCacheEntry{
			info:      RunnerInfo{Status: RunnerStatusMissing},
			fetchedAt: time.Now(),
		}
		return manager
	}

	cases := []struct {
		name        string
		mutate      func(*Manager)
		req         RunnerRunRequest
		wantErrText string
	}{
		{
			name:        "missing parent session",
			req:         RunnerRunRequest{Task: "task", RunnerID: string(RunnerOpenCode)},
			wantErrText: "missing parent session",
		},
		{
			name:        "empty task",
			req:         RunnerRunRequest{ParentSessionKey: "sess", RunnerID: string(RunnerOpenCode)},
			wantErrText: "empty task",
		},
		{
			name:        "missing runner id",
			req:         RunnerRunRequest{ParentSessionKey: "sess", Task: "task"},
			wantErrText: "missing runner_id",
		},
		{
			name:        "unknown runner",
			req:         RunnerRunRequest{ParentSessionKey: "sess", Task: "task", RunnerID: "missing-runner"},
			wantErrText: "unknown runner",
		},
		{
			name:        "unknown legacy runner id rejected",
			req:         RunnerRunRequest{ParentSessionKey: "sess", Task: "task", RunnerID: "or3-intern"},
			wantErrText: "unknown runner",
		},
		{
			name:        "runner disabled",
			mutate:      func(m *Manager) { m.Cfg.Disabled = []string{string(RunnerOpenCode)} },
			req:         RunnerRunRequest{ParentSessionKey: "sess", Task: "task", RunnerID: string(RunnerOpenCode)},
			wantErrText: "disabled by config",
		},
		{
			name:        "runner missing",
			req:         RunnerRunRequest{ParentSessionKey: "sess", Task: "task", RunnerID: string(RunnerCodex)},
			wantErrText: "not installed",
		},
		{
			name: "runner auth missing",
			mutate: func(m *Manager) {
				m.Registry.detectCache[RunnerOpenCode] = runnerDetectCacheEntry{
					info:      RunnerInfo{Status: RunnerStatusAuthMissing},
					fetchedAt: time.Now(),
				}
			},
			req:         RunnerRunRequest{ParentSessionKey: "sess", Task: "task", RunnerID: string(RunnerOpenCode)},
			wantErrText: "not authenticated",
		},
		{
			name: "runner not functional",
			mutate: func(m *Manager) {
				m.Registry.detectCache[RunnerOpenCode] = runnerDetectCacheEntry{
					info:      RunnerInfo{Status: RunnerStatusError},
					fetchedAt: time.Now(),
				}
			},
			req:         RunnerRunRequest{ParentSessionKey: "sess", Task: "task", RunnerID: string(RunnerOpenCode)},
			wantErrText: "not functional",
		},
		{
			name:        "invalid cwd",
			req:         RunnerRunRequest{ParentSessionKey: "sess", Task: "task", RunnerID: string(RunnerOpenCode), Cwd: "../outside"},
			wantErrText: "invalid cwd",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			manager := newCaseManager(t)
			if tc.mutate != nil {
				tc.mutate(manager)
			}
			_, err := manager.Enqueue(context.Background(), tc.req)
			if err == nil || !strings.Contains(err.Error(), tc.wantErrText) {
				t.Fatalf("expected error containing %q, got %v", tc.wantErrText, err)
			}
		})
	}
}

func TestManagerEnqueueQueueFullAndAbortLifecycle(t *testing.T) {
	manager, database, jobs := newTestManager(t)
	manager.MaxQueued = 1
	ctx := context.Background()
	first, err := manager.Enqueue(ctx, RunnerRunRequest{ParentSessionKey: "sess", Task: "first", RunnerID: string(RunnerOpenCode)})
	if err != nil {
		t.Fatalf("Enqueue first: %v", err)
	}
	if _, err := manager.Enqueue(ctx, RunnerRunRequest{ParentSessionKey: "sess", Task: "second", RunnerID: string(RunnerOpenCode)}); !errors.Is(err, db.ErrRunnerRunQueueFull) {
		t.Fatalf("expected queue full, got %v", err)
	}
	if err := manager.Abort(ctx, first.JobID); err != nil {
		t.Fatalf("Abort queued: %v", err)
	}
	stored := mustGetRunnerRun(t, database, first.ID)
	if stored.Status != db.RunnerRunStatusAborted {
		t.Fatalf("expected queued run aborted, got %#v", stored)
	}
	snap, ok := jobs.Snapshot(first.JobID)
	if !ok || snap.Status != "aborted" {
		t.Fatalf("unexpected queued snapshot: ok=%v snapshot=%#v", ok, snap)
	}

	cancelled := make(chan struct{}, 1)
	jobs.RegisterWithID("job-running-cancel", "runner:test")
	jobs.AttachCancel("job-running-cancel", func() { cancelled <- struct{}{} })
	if err := manager.Abort(ctx, "job-running-cancel"); err != nil {
		t.Fatalf("Abort running: %v", err)
	}
	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Fatal("expected cancel callback")
	}

	running := mustInsertRunnerRun(t, database, db.RunnerRun{
		ID:        "acr-running-no-cancel",
		JobID:     "job-running-no-cancel",
		RunnerID:  string(RunnerOpenCode),
		Status:    db.RunnerRunStatusRunning,
		StartedAt: db.NowMS(),
	})
	if err := manager.Abort(ctx, running.ID); err == nil || !strings.Contains(err.Error(), "not abortable") {
		t.Fatalf("expected not abortable error, got %v", err)
	}
	if err := manager.Abort(ctx, "missing-job"); err == nil || !strings.Contains(err.Error(), "job not found") {
		t.Fatalf("expected job not found error, got %v", err)
	}
}

func TestManagerExecuteRunBuildFailureFinalizesRun(t *testing.T) {
	database := openRunnerTestDB(t)
	jobs := jobs.NewRegistry(0, 0)
	run := mustInsertRunnerRun(t, database, db.RunnerRun{
		ID:        "acr-build-failure",
		JobID:     "job-build-failure",
		RunnerID:  string(RunnerOpenCode),
		Status:    db.RunnerRunStatusRunning,
		StartedAt: db.NowMS(),
	})
	jobs.RegisterWithID(run.JobID, "runner:opencode")
	manager := &Manager{
		DB:          database,
		Jobs:        jobs,
		Cfg:         config.RunnersConfig{PreviewMaxBytes: 1024, EventChunkMaxBytes: 128},
		TaskTimeout: time.Second,
		ctx:         context.Background(),
	}

	manager.executeRun(run)
	stored := mustGetRunnerRun(t, database, run.ID)
	if stored.Status != db.RunnerRunStatusFailed || !strings.Contains(stored.ErrorMessage, "no runner registry configured") {
		t.Fatalf("unexpected failed run: %#v", stored)
	}
}

func TestManagerExecuteRunHonorsDeadlineAndCancellation(t *testing.T) {
	for _, tc := range []struct {
		name       string
		timeout    int
		cancelFunc func(context.CancelFunc)
		wantStatus string
		wantErr    string
	}{
		{name: "deadline exceeded", timeout: 1, wantStatus: db.RunnerRunStatusTimedOut, wantErr: "timed out"},
		{name: "cancelled", timeout: 10, cancelFunc: func(cancel context.CancelFunc) { time.AfterFunc(100*time.Millisecond, cancel) }, wantStatus: db.RunnerRunStatusAborted, wantErr: "aborted"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			database := openRunnerTestDB(t)
			jobs := jobs.NewRegistry(0, 0)
			binary := writeFakeBinary(t, t.TempDir(), "sleepy-runner", `sleep 2`)
			adapter := &stubRunnerAdapter{
				id:   RunnerID("sleepy"),
				spec: RunnerSpec{ID: RunnerID("sleepy"), DisplayName: "Sleepy", Binary: binary},
				cmd:  CommandSpec{Binary: binary, Cwd: filepath.Dir(binary), OutputMode: OutputPlain},
			}
			registry := NewRunnerRegistry([]RunnerSpec{adapter.spec}, []RunnerAdapter{adapter})
			run := mustInsertRunnerRun(t, database, db.RunnerRun{
				ID:             fmt.Sprintf("acr-%s", strings.ReplaceAll(tc.name, " ", "-")),
				JobID:          fmt.Sprintf("job-%s", strings.ReplaceAll(tc.name, " ", "-")),
				RunnerID:       string(adapter.id),
				Status:         db.RunnerRunStatusRunning,
				StartedAt:      db.NowMS(),
				TimeoutSeconds: tc.timeout,
				Cwd:            filepath.Dir(binary),
			})
			jobs.RegisterWithID(run.JobID, "runner:sleepy")
			runCtx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if tc.cancelFunc != nil {
				tc.cancelFunc(cancel)
			}
			manager := &Manager{
				DB:          database,
				Jobs:        jobs,
				Registry:    registry,
				Process:     NewProcessManager(128, 1024),
				Cfg:         config.RunnersConfig{PreviewMaxBytes: 1024, EventChunkMaxBytes: 128},
				TaskTimeout: time.Second,
				ctx:         runCtx,
			}

			manager.executeRun(run)
			stored := mustGetRunnerRun(t, database, run.ID)
			if stored.Status != tc.wantStatus || stored.ErrorMessage != tc.wantErr {
				t.Fatalf("unexpected stored run: %#v", stored)
			}
		})
	}
}
