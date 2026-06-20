package runners

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
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
