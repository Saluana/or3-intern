package agentcli

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"or3-intern/internal/agent"
	"or3-intern/internal/config"
	"or3-intern/internal/db"
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
func (a *stubRunnerAdapter) BuildCommand(AgentRunRequest) (CommandSpec, error) {
	if a.err != nil {
		return CommandSpec{}, a.err
	}
	cmd := a.cmd
	if cmd.RunnerID == "" {
		cmd.RunnerID = a.id
	}
	return cmd, nil
}

func newTestManager(t *testing.T) (*Manager, *db.DB, *agent.JobRegistry) {
	t.Helper()
	database := openAgentCLITestDB(t)
	jobs := agent.NewJobRegistry(0, 0)
	manager := &Manager{
		DB:       database,
		Jobs:     jobs,
		Registry: NewDefaultRegistry(),
		Cfg: config.AgentCLIConfig{
			Enabled:               true,
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

func mustInsertAgentRun(t *testing.T, database *db.DB, run db.AgentCLIRun) db.AgentCLIRun {
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
		run.RunnerID = string(RunnerOR3)
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
	if err := database.EnqueueAgentCLIRun(context.Background(), run); err != nil {
		t.Fatalf("EnqueueAgentCLIRun: %v", err)
	}
	return run
}

func mustGetAgentRun(t *testing.T, database *db.DB, id string) db.AgentCLIRun {
	t.Helper()
	run, ok, err := database.GetAgentCLIRun(context.Background(), id)
	if err != nil || !ok {
		t.Fatalf("GetAgentCLIRun(%q): ok=%v err=%v", id, ok, err)
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

	database := openAgentCLITestDB(t)
	jobs := agent.NewJobRegistry(0, 0)
	run := mustInsertAgentRun(t, database, db.AgentCLIRun{
		ID:             "acr-reconcile",
		JobID:          "job-reconcile",
		RunnerID:       string(RunnerOR3),
		Status:         db.AgentCLIStatusRunning,
		StartedAt:      db.NowMS(),
		TimeoutSeconds: 30,
	})
	jobs.RegisterWithID(run.JobID, "agent_cli:or3")

	manager := &Manager{
		DB:   database,
		Jobs: jobs,
		Cfg: config.AgentCLIConfig{
			Enabled:               true,
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
	stored := mustGetAgentRun(t, database, run.ID)
	if stored.Status != db.AgentCLIStatusAborted || stored.ErrorMessage != "aborted by service restart" {
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

func TestManagerStopTimeout(t *testing.T) {
	manager := &Manager{DB: openAgentCLITestDB(t), started: true, cancel: func() {}}
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
	manager, _, _ := newTestManager(t)
	manager.RestrictDir = t.TempDir()
	manager.Cfg.MaxTimeoutSeconds = 30
	manager.Registry.detectCache[RunnerCodex] = runnerDetectCacheEntry{
		info:      RunnerInfo{Status: RunnerStatusMissing},
		fetchedAt: time.Now(),
	}
	manager.Registry.detectCache[RunnerClaude] = runnerDetectCacheEntry{
		info:      RunnerInfo{Status: RunnerStatusAuthMissing},
		fetchedAt: time.Now(),
	}
	manager.Registry.detectCache[RunnerGemini] = runnerDetectCacheEntry{
		info:      RunnerInfo{Status: RunnerStatusError},
		fetchedAt: time.Now(),
	}

	cases := []struct {
		name        string
		mutate      func(*Manager)
		req         AgentRunRequest
		wantErrText string
	}{
		{
			name:        "disabled by config",
			mutate:      func(m *Manager) { m.Cfg.Enabled = false },
			req:         AgentRunRequest{ParentSessionKey: "sess", Task: "task", RunnerID: string(RunnerOR3)},
			wantErrText: "disabled",
		},
		{
			name:        "missing parent session",
			req:         AgentRunRequest{Task: "task", RunnerID: string(RunnerOR3)},
			wantErrText: "missing parent session",
		},
		{
			name:        "empty task",
			req:         AgentRunRequest{ParentSessionKey: "sess", RunnerID: string(RunnerOR3)},
			wantErrText: "empty task",
		},
		{
			name:        "missing runner id",
			req:         AgentRunRequest{ParentSessionKey: "sess", Task: "task"},
			wantErrText: "missing runner_id",
		},
		{
			name:        "unknown runner",
			req:         AgentRunRequest{ParentSessionKey: "sess", Task: "task", RunnerID: "missing-runner"},
			wantErrText: "unknown runner",
		},
		{
			name:        "runner disabled",
			mutate:      func(m *Manager) { m.Cfg.DisabledRunners = []string{string(RunnerOR3)} },
			req:         AgentRunRequest{ParentSessionKey: "sess", Task: "task", RunnerID: string(RunnerOR3)},
			wantErrText: "disabled by config",
		},
		{
			name:        "runner missing",
			req:         AgentRunRequest{ParentSessionKey: "sess", Task: "task", RunnerID: string(RunnerCodex)},
			wantErrText: "not installed",
		},
		{
			name:        "runner auth missing",
			req:         AgentRunRequest{ParentSessionKey: "sess", Task: "task", RunnerID: string(RunnerClaude)},
			wantErrText: "not authenticated",
		},
		{
			name:        "runner not functional",
			req:         AgentRunRequest{ParentSessionKey: "sess", Task: "task", RunnerID: string(RunnerGemini)},
			wantErrText: "not functional",
		},
		{
			name:        "invalid cwd",
			req:         AgentRunRequest{ParentSessionKey: "sess", Task: "task", RunnerID: string(RunnerOR3), Cwd: "/outside"},
			wantErrText: "invalid cwd",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			copyManager := *manager
			copyManager.Cfg = manager.Cfg
			if tc.mutate != nil {
				tc.mutate(&copyManager)
			}
			_, err := copyManager.Enqueue(context.Background(), tc.req)
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
	first, err := manager.Enqueue(ctx, AgentRunRequest{ParentSessionKey: "sess", Task: "first", RunnerID: string(RunnerOR3)})
	if err != nil {
		t.Fatalf("Enqueue first: %v", err)
	}
	if _, err := manager.Enqueue(ctx, AgentRunRequest{ParentSessionKey: "sess", Task: "second", RunnerID: string(RunnerOR3)}); !errors.Is(err, db.ErrAgentCLIQueueFull) {
		t.Fatalf("expected queue full, got %v", err)
	}
	if err := manager.Abort(ctx, first.JobID); err != nil {
		t.Fatalf("Abort queued: %v", err)
	}
	stored := mustGetAgentRun(t, database, first.ID)
	if stored.Status != db.AgentCLIStatusAborted {
		t.Fatalf("expected queued run aborted, got %#v", stored)
	}
	snap, ok := jobs.Snapshot(first.JobID)
	if !ok || snap.Status != "aborted" {
		t.Fatalf("unexpected queued snapshot: ok=%v snapshot=%#v", ok, snap)
	}

	cancelled := make(chan struct{}, 1)
	jobs.RegisterWithID("job-running-cancel", "agent_cli:test")
	jobs.AttachCancel("job-running-cancel", func() { cancelled <- struct{}{} })
	if err := manager.Abort(ctx, "job-running-cancel"); err != nil {
		t.Fatalf("Abort running: %v", err)
	}
	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Fatal("expected cancel callback")
	}

	running := mustInsertAgentRun(t, database, db.AgentCLIRun{
		ID:        "acr-running-no-cancel",
		JobID:     "job-running-no-cancel",
		RunnerID:  string(RunnerOR3),
		Status:    db.AgentCLIStatusRunning,
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
	database := openAgentCLITestDB(t)
	jobs := agent.NewJobRegistry(0, 0)
	run := mustInsertAgentRun(t, database, db.AgentCLIRun{
		ID:        "acr-build-failure",
		JobID:     "job-build-failure",
		RunnerID:  string(RunnerOR3),
		Status:    db.AgentCLIStatusRunning,
		StartedAt: db.NowMS(),
	})
	jobs.RegisterWithID(run.JobID, "agent_cli:or3")
	manager := &Manager{
		DB:          database,
		Jobs:        jobs,
		Cfg:         config.AgentCLIConfig{PreviewMaxBytes: 1024, EventChunkMaxBytes: 128},
		TaskTimeout: time.Second,
		ctx:         context.Background(),
	}

	manager.executeRun(run)
	stored := mustGetAgentRun(t, database, run.ID)
	if stored.Status != db.AgentCLIStatusFailed || !strings.Contains(stored.ErrorMessage, "no runner registry configured") {
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
		{name: "deadline exceeded", timeout: 1, wantStatus: db.AgentCLIStatusTimedOut, wantErr: "timed out"},
		{name: "cancelled", timeout: 10, cancelFunc: func(cancel context.CancelFunc) { time.AfterFunc(100*time.Millisecond, cancel) }, wantStatus: db.AgentCLIStatusAborted, wantErr: "aborted"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			database := openAgentCLITestDB(t)
			jobs := agent.NewJobRegistry(0, 0)
			binary := writeFakeBinary(t, t.TempDir(), "sleepy-runner", `sleep 2`)
			adapter := &stubRunnerAdapter{
				id:   RunnerID("sleepy"),
				spec: RunnerSpec{ID: RunnerID("sleepy"), DisplayName: "Sleepy", Binary: binary},
				cmd:  CommandSpec{Binary: binary, Cwd: filepath.Dir(binary), OutputMode: OutputPlain},
			}
			registry := NewRunnerRegistry([]RunnerSpec{adapter.spec}, []RunnerAdapter{adapter})
			run := mustInsertAgentRun(t, database, db.AgentCLIRun{
				ID:             fmt.Sprintf("acr-%s", strings.ReplaceAll(tc.name, " ", "-")),
				JobID:          fmt.Sprintf("job-%s", strings.ReplaceAll(tc.name, " ", "-")),
				RunnerID:       string(adapter.id),
				Status:         db.AgentCLIStatusRunning,
				StartedAt:      db.NowMS(),
				TimeoutSeconds: tc.timeout,
				Cwd:            filepath.Dir(binary),
			})
			jobs.RegisterWithID(run.JobID, "agent_cli:sleepy")
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
				Cfg:         config.AgentCLIConfig{PreviewMaxBytes: 1024, EventChunkMaxBytes: 128},
				TaskTimeout: time.Second,
				ctx:         runCtx,
			}

			manager.executeRun(run)
			stored := mustGetAgentRun(t, database, run.ID)
			if stored.Status != tc.wantStatus || stored.ErrorMessage != tc.wantErr {
				t.Fatalf("unexpected stored run: %#v", stored)
			}
		})
	}
}
