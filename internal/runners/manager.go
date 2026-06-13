package runners

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"or3-intern/internal/config"
	"or3-intern/internal/db"
	"or3-intern/internal/jobs"
)

const (
	runnerClaimRetryDelay = 25 * time.Millisecond
	runnerFinalizeTimeout = 5 * time.Second
	runnerDetectCacheTTL  = 30 * time.Second
)

// Manager queues and runs external runner jobs.
type Manager struct {
	DB       *db.DB
	Jobs     *jobs.Registry
	Cfg      config.RunnersConfig
	Registry *RunnerRegistry
	Runtimes *RunnerRuntimeRegistry
	Process  *ProcessManager

	// OpenCodeExternalDirectories are OR3-owned directories that OpenCode may
	// access outside the current cwd without falling back to a global permissions
	// bypass.
	OpenCodeExternalDirectories []string

	MaxConcurrent int
	MaxQueued     int
	TaskTimeout   time.Duration

	// RestrictDir is the allowed root for working directories.
	// Empty means no restriction.
	RestrictDir string

	mu       sync.Mutex
	started  bool
	ctx      context.Context
	cancel   context.CancelFunc
	notifyCh chan struct{}
	wg       sync.WaitGroup
}

// Start launches the background workers and resumes queued jobs.
func (m *Manager) Start(ctx context.Context) error {
	if m == nil {
		return fmt.Errorf("runner manager is nil")
	}
	if m.DB == nil {
		return fmt.Errorf("runner db not configured")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.started {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if m.MaxConcurrent <= 0 {
		m.MaxConcurrent = 1
	}
	if m.MaxQueued <= 0 {
		m.MaxQueued = 16
	}
	if m.TaskTimeout <= 0 {
		m.TaskTimeout = 900 * time.Second
	}
	if m.Process == nil {
		m.Process = NewProcessManager(m.Cfg.EventChunkMaxBytes, m.Cfg.PreviewMaxBytes)
	}
	if m.Registry == nil {
		m.Registry = NewDefaultRegistry()
	}
	if m.Runtimes == nil {
		m.Runtimes = NewDefaultRuntimeRegistry()
	}
	m.Registry.RefreshAllAsync(m.detectOptions(m.Cfg))
	running, err := m.DB.ListRunningRunnerRuns(ctx)
	if err != nil {
		return err
	}
	queued, err := m.DB.ListQueuedRunnerRuns(ctx)
	if err != nil {
		return err
	}
	m.ctx, m.cancel = context.WithCancel(ctx)
	m.notifyCh = make(chan struct{}, m.MaxConcurrent)
	m.started = true
	for i := 0; i < m.MaxConcurrent; i++ {
		m.wg.Add(1)
		go m.workerLoop()
	}
	for _, run := range running {
		m.reconcileInterruptedRun(run, "aborted by service restart")
	}
	if len(queued) > 0 {
		m.signalN(minInt(len(queued), m.MaxConcurrent))
	}
	return nil
}

// Stop cancels workers and waits for them to exit. It also stops any
// registered native runtimes (managed opencode servers, etc.) so process
// groups are cleaned up on shutdown. External servers are never killed.
func (m *Manager) Stop(ctx context.Context) error {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	if !m.started {
		m.mu.Unlock()
		return nil
	}
	cancel := m.cancel
	m.started = false
	runtimes := m.Runtimes
	m.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	done := make(chan struct{})
	go func() {
		m.wg.Wait()
		close(done)
	}()
	if ctx == nil {
		ctx = context.Background()
	}
	var firstErr error
	select {
	case <-done:
	case <-ctx.Done():
		firstErr = ctx.Err()
	}
	// Best-effort native runtime shutdown. Use a bounded timeout so a
	// stuck managed process doesn't block the service indefinitely.
	if runtimes != nil {
		stopCtx, cancelStop := context.WithTimeout(context.Background(), runnerFinalizeTimeout)
		defer cancelStop()
		runtimes.ForEach(func(runtime NativeRunnerRuntime) {
			if err := runtime.Stop(stopCtx); err != nil && firstErr == nil {
				firstErr = err
			}
		})
	}
	return firstErr
}

// Enqueue validates, persists, and signals a new runner run.
func (m *Manager) Enqueue(ctx context.Context, req RunnerRunRequest) (db.RunnerRun, error) {
	if m == nil || m.DB == nil {
		return db.RunnerRun{}, fmt.Errorf("runner manager is not available")
	}
	cfg := m.configSnapshot()
	parentSessionKey := strings.TrimSpace(req.ParentSessionKey)
	if parentSessionKey == "" {
		return db.RunnerRun{}, fmt.Errorf("missing parent session")
	}
	task := strings.TrimSpace(req.Task)
	if task == "" {
		return db.RunnerRun{}, fmt.Errorf("empty task")
	}
	runnerID := strings.TrimSpace(req.RunnerID)
	if runnerID == "" {
		return db.RunnerRun{}, fmt.Errorf("missing runner_id")
	}

	// Default and normalize mode/isolation
	mode := req.Mode
	if mode == "" {
		mode = cfg.DefaultMode
	}
	isolation := req.Isolation
	if isolation == "" {
		isolation = cfg.DefaultIsolation
	}

	// Validate policy
	if err := ValidateRunPolicy(RunnerMode(mode), RunIsolation(isolation), cfg.AllowSandboxAuto); err != nil {
		return db.RunnerRun{}, fmt.Errorf("policy validation: %w", err)
	}
	// Check runner readiness
	if m.Registry != nil {
		if _, ok := m.Registry.Spec(RunnerID(runnerID)); !ok {
			return db.RunnerRun{}, fmt.Errorf("unknown runner %q", runnerID)
		}
		if isRunnerDisabled(RunnerID(runnerID), cfg.Disabled) {
			return db.RunnerRun{}, fmt.Errorf("runner %q is disabled by config", runnerID)
		}
		detectOpts := m.detectOptions(cfg)
		if info, ok := m.Registry.DetectCached(RunnerID(runnerID), runnerDetectCacheTTL); ok {
			switch info.Status {
			case RunnerStatusDisabledByConfig:
				return db.RunnerRun{}, fmt.Errorf("runner %q is disabled by config", runnerID)
			case RunnerStatusMissing:
				return db.RunnerRun{}, fmt.Errorf("runner %q is not installed", runnerID)
			case RunnerStatusAuthMissing:
				return db.RunnerRun{}, fmt.Errorf("runner %q is not authenticated", runnerID)
			case RunnerStatusError:
				return db.RunnerRun{}, fmt.Errorf("runner %q is not functional", runnerID)
			}
		} else {
			m.Registry.RefreshDetectAsync(RunnerID(runnerID), detectOpts)
		}
	}

	// Default timeout
	timeoutSeconds := req.TimeoutSeconds
	if timeoutSeconds <= 0 {
		timeoutSeconds = cfg.DefaultTimeoutSeconds
	}
	if timeoutSeconds > cfg.MaxTimeoutSeconds {
		timeoutSeconds = cfg.MaxTimeoutSeconds
	}

	// Resolve and validate cwd against allowed root
	cwd, err := resolveRunnerCwd(req.Cwd, m.RestrictDir)
	if err != nil {
		return db.RunnerRun{}, fmt.Errorf("invalid cwd: %w", err)
	}

	jobID := newRunnerJobID()
	runID := "rr_" + newRunnerJobID()[:16]

	metaJSON := "{}"
	combined := make(map[string]any, len(req.Meta)+2)
	for k, v := range req.Meta {
		combined[k] = v
	}
	if req.MaxTurns > 0 {
		combined["_max_turns"] = req.MaxTurns
	}
	if len(combined) > 0 {
		b, _ := json.Marshal(combined)
		if len(b) > 0 {
			metaJSON = string(b)
		}
	}

	model := strings.TrimSpace(req.Model)
	if RunnerID(runnerID) == RunnerOpenCode && model != "" {
		model = NormalizeOpenCodeModelID(ctx, cfg, nativeEnv(cfg), model)
	}

	run := db.RunnerRun{
		ID:               runID,
		JobID:            jobID,
		ParentSessionKey: parentSessionKey,
		RunnerID:         runnerID,
		Task:             task,
		Cwd:              cwd,
		Model:            model,
		Mode:             mode,
		Isolation:        isolation,
		Status:           db.RunnerRunStatusQueued,
		RequestedAt:      db.NowMS(),
		TimeoutSeconds:   timeoutSeconds,
		MetaJSON:         metaJSON,
	}

	if err := m.DB.EnqueueRunnerRunLimited(ctx, run, m.MaxQueued); err != nil {
		return db.RunnerRun{}, err
	}

	kind := "runner:" + runnerID
	if m.Jobs != nil {
		m.Jobs.RegisterWithID(jobID, kind)
		m.Jobs.Publish(jobID, "queued", map[string]any{
			"status":    db.RunnerRunStatusQueued,
			"runner_id": runnerID,
			"run_id":    runID,
			"mode":      mode,
			"isolation": isolation,
		})
	}

	m.signal()
	return run, nil
}

// Abort cancels the running or queued runner job with id.
func (m *Manager) Abort(ctx context.Context, id string) error {
	if m == nil || m.DB == nil {
		return fmt.Errorf("runner manager is not available")
	}
	if m.Runtimes != nil {
		m.Runtimes.ForEach(func(runtime NativeRunnerRuntime) {
			_ = runtime.Abort(ctx, id)
		})
	}
	// First try to cancel a running job via JobRegistry
	if m.Jobs != nil && m.Jobs.Cancel(id) {
		return nil
	}
	// Then try to abort a queued job in the DB
	run, ok, err := m.DB.AbortQueuedRunnerRun(ctx, id, "aborted before execution")
	if err != nil {
		return err
	}
	if !ok {
		stored, exists, lookupErr := m.DB.GetRunnerRun(ctx, id)
		if lookupErr != nil {
			return lookupErr
		}
		if !exists {
			return fmt.Errorf("job not found")
		}
		if stored.Status == db.RunnerRunStatusQueued {
			return fmt.Errorf("job is not abortable")
		}
		return fmt.Errorf("job is not abortable")
	}
	if m.Jobs != nil {
		m.Jobs.Complete(id, "aborted", map[string]any{
			"message":   "aborted before execution",
			"runner_id": run.RunnerID,
			"run_id":    run.ID,
		})
	}
	return nil
}

func (m *Manager) workerLoop() {
	defer m.wg.Done()
	for {
		ran, err := m.runOnce()
		if err != nil {
			if runnerDatabaseClosed(err) {
				return
			}
			if !errors.Is(err, context.Canceled) {
				log.Printf("runner worker error: %v", err)
			}
		}
		if ran {
			continue
		}
		select {
		case <-m.ctx.Done():
			return
		case <-m.notifyCh:
		case <-time.After(runnerClaimRetryDelay):
		}
	}
}

func runnerDatabaseClosed(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(strings.ToLower(err.Error()), "database is closed")
}

func (m *Manager) runOnce() (bool, error) {
	run, err := m.DB.ClaimNextRunnerRun(m.ctx)
	if err != nil || run == nil {
		return false, err
	}
	m.executeRun(*run)
	return true, nil
}

func (m *Manager) executeRun(run db.RunnerRun) {
	defer m.recoverRunPanic(run)
	timeout := time.Duration(run.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = m.TaskTimeout
	}
	runCtx, cancel := context.WithTimeout(m.ctx, timeout)
	defer cancel()

	if m.Jobs != nil {
		m.Jobs.AttachCancel(run.JobID, cancel)
		m.Jobs.Publish(run.JobID, "started", map[string]any{
			"status":    db.RunnerRunStatusRunning,
			"runner_id": run.RunnerID,
			"run_id":    run.ID,
			"mode":      run.Mode,
			"isolation": run.Isolation,
		})
	}

	if out, handled := m.tryExecuteNativeRun(runCtx, run); handled {
		if out.PendingNativeApproval {
			m.pauseRunForNativeApproval(run, out)
			return
		}
		finalStatus := db.RunnerRunStatusSucceeded
		if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
			finalStatus = db.RunnerRunStatusTimedOut
		} else if errors.Is(runCtx.Err(), context.Canceled) {
			finalStatus = db.RunnerRunStatusAborted
		} else if out.ExitCode != 0 {
			finalStatus = db.RunnerRunStatusFailed
		}
		m.emitCompletion(run, finalStatus, out)
		errMsg := ""
		if finalStatus == db.RunnerRunStatusFailed {
			errMsg = out.StderrPreview
		}
		if finalStatus == db.RunnerRunStatusTimedOut {
			errMsg = "timed out"
		}
		if finalStatus == db.RunnerRunStatusAborted {
			errMsg = "aborted"
		}
		m.finalizeRun(runCtx, run, finalStatus, errMsg, out)
		return
	}

	var cmdSpec CommandSpec
	cmdSpec, buildErr := m.buildCommandSpecForRun(runCtx, run)
	if buildErr != nil {
		m.finalizeRun(runCtx, run, db.RunnerRunStatusFailed, buildErr.Error(), ProcessOutput{ExitCode: -1, DurationMS: 0})
		return
	}
	runnerID := cmdSpec.RunnerID
	if runnerID == "" {
		runnerID = RunnerID(run.RunnerID)
	}
	additionalEnv, envErr := m.runnerAdditionalEnv(runnerID, parseAgentRunMeta(run.MetaJSON))
	if envErr != nil {
		m.finalizeRun(runCtx, run, db.RunnerRunStatusFailed, envErr.Error(), ProcessOutput{ExitCode: -1, DurationMS: 0})
		return
	}

	// Build child environment — use os.Environ() as the base so PATH, HOME,
	// and TMPDIR are preserved through the allowlist filter.
	if len(cmdSpec.Env) == 0 {
		cmdSpec.Env = BuildRunnerEnv(os.Environ(), m.configSnapshot().ChildEnvAllowlist, additionalEnv)
	} else if len(additionalEnv) > 0 {
		cmdSpec.Env = mergeEnvOverlay(cmdSpec.Env, additionalEnv)
	}

	// Emit started event with argv preview
	startedTS := time.Now().UTC().Format(time.RFC3339Nano)
	startedPayload, _ := json.Marshal(map[string]any{
		"job_id":       run.JobID,
		"runner_id":    run.RunnerID,
		"run_id":       run.ID,
		"argv_preview": cmdSpec.ArgvPreview,
		"cwd":          cmdSpec.Cwd,
	})
	m.persistEvent(run, RunnerRunEvent{
		Type:     "started",
		TS:       startedTS,
		Seq:      0,
		JobID:    run.JobID,
		RunnerID: run.RunnerID,
		Payload:  startedPayload,
	})

	if m.Jobs != nil {
		m.Jobs.Publish(run.JobID, "started", map[string]any{
			"status":       db.RunnerRunStatusRunning,
			"runner_id":    run.RunnerID,
			"run_id":       run.ID,
			"argv_preview": cmdSpec.ArgvPreview,
			"cwd":          cmdSpec.Cwd,
		})
	}

	// Run the process
	pm := m.Process
	if pm == nil {
		cfg := m.configSnapshot()
		pm = NewProcessManager(cfg.EventChunkMaxBytes, cfg.PreviewMaxBytes)
	}

	var maxSeq int64
	out := pm.Run(runCtx, cmdSpec, func(e RunnerRunEvent) {
		e.JobID = run.JobID
		e.RunnerID = run.RunnerID
		updateMaxSeq(&maxSeq, e.Seq)
		m.persistEvent(run, e)
		if m.Jobs != nil {
			m.Jobs.Publish(run.JobID, e.Type, eventToMap(e))
		}
	})
	if runnerID == RunnerCodex && out.ExitCode != 0 {
		authText := firstNonEmpty(out.StderrPreview, out.StdoutPreview, out.FinalTextPreview)
		if isCodexAuthRefreshFailure(nil, authText) {
			out.StderrPreview = codexAuthRefreshFailureMessage(nil, authText)
			out.FinalTextPreview = ""
		}
	}

	// Determine final status
	var finalStatus string
	switch {
	case errors.Is(runCtx.Err(), context.DeadlineExceeded):
		finalStatus = db.RunnerRunStatusTimedOut
	case errors.Is(runCtx.Err(), context.Canceled):
		finalStatus = db.RunnerRunStatusAborted
	case out.ExitCode == 0:
		finalStatus = db.RunnerRunStatusSucceeded
	default:
		finalStatus = db.RunnerRunStatusFailed
	}

	m.emitCompletionWithSeq(run, finalStatus, out, maxSeq+1)

	var errMsg string
	if finalStatus == db.RunnerRunStatusFailed {
		errMsg = out.StderrPreview
	}
	if finalStatus == db.RunnerRunStatusTimedOut {
		errMsg = "timed out"
	}
	if finalStatus == db.RunnerRunStatusAborted {
		errMsg = "aborted"
	}
	m.finalizeRun(runCtx, run, finalStatus, errMsg, out)
}

func (m *Manager) tryExecuteNativeRun(ctx context.Context, run db.RunnerRun) (ProcessOutput, bool) {
	cfg := m.configSnapshot()
	mode := runnerRuntimeMode(cfg, RunnerID(run.RunnerID))
	if mode == RuntimeModeCLI {
		return ProcessOutput{}, false
	}
	chatReq, ok := buildRuntimeChatRequest(run)
	if !ok {
		return ProcessOutput{}, false
	}
	if m.Runtimes == nil {
		m.Runtimes = NewDefaultRuntimeRegistry()
	}
	runtime, ok := m.Runtimes.Get(RunnerID(run.RunnerID))
	if !ok {
		return ProcessOutput{}, false
	}
	env := nativeEnv(cfg)
	runnerID := RunnerID(run.RunnerID)
	if additionalEnv, err := m.runnerAdditionalEnv(runnerID, parseAgentRunMeta(run.MetaJSON)); err != nil {
		return ProcessOutput{ExitCode: -1, StderrPreview: err.Error()}, true
	} else if len(additionalEnv) > 0 {
		env = mergeEnvOverlay(env, additionalEnv)
	}
	startedPayload, _ := json.Marshal(map[string]any{
		"job_id":    run.JobID,
		"runner_id": run.RunnerID,
		"run_id":    run.ID,
		"runtime":   "native",
		"cwd":       run.Cwd,
	})
	m.persistEvent(run, RunnerRunEvent{Type: "started", TS: time.Now().UTC().Format(time.RFC3339Nano), Seq: 0, JobID: run.JobID, RunnerID: run.RunnerID, Payload: startedPayload})
	if m.Jobs != nil {
		m.Jobs.Publish(run.JobID, "started", map[string]any{"status": db.RunnerRunStatusRunning, "runner_id": run.RunnerID, "run_id": run.ID, "runtime": "native", "cwd": run.Cwd})
	}
	var maxSeq int64
	out, err := runtime.Execute(ctx, NativeRuntimeExecuteRequest{
		Run:    run,
		Chat:   chatReq,
		Config: cfg,
		Env:    env,
		OnEvent: func(e RunnerRunEvent) {
			e.JobID = run.JobID
			e.RunnerID = run.RunnerID
			updateMaxSeq(&maxSeq, e.Seq)
			m.persistEvent(run, e)
			if m.Jobs != nil {
				m.Jobs.Publish(run.JobID, e.Type, eventToMap(e))
			}
		},
	})
	out.EventSeq = maxSeq
	if err == nil {
		return out, true
	}
	if errors.Is(err, errNativeApprovalRequired) {
		out.ExitCode = -1
		out.PendingNativeApproval = true
		if out.StderrPreview == "" {
			out.StderrPreview = err.Error()
		}
		return out, true
	}
	if RunnerID(run.RunnerID) == RunnerCodex && isCodexAuthRefreshFailure(err, out.StderrPreview) {
		out.ExitCode = -1
		out.StderrPreview = codexAuthRefreshFailureMessage(err, out.StderrPreview)
		return out, true
	}
	if RunnerID(run.RunnerID) == RunnerCodex {
		out.ExitCode = -1
		if out.StderrPreview == "" {
			out.StderrPreview = err.Error()
		}
		return out, true
	}
	if mode == RuntimeModeAuto {
		payload, _ := json.Marshal(map[string]any{"runtime": "native", "fallback": "cli", "reason": err.Error()})
		m.persistEvent(run, RunnerRunEvent{Type: "warning", TS: time.Now().UTC().Format(time.RFC3339Nano), JobID: run.JobID, RunnerID: run.RunnerID, Payload: payload, Message: err.Error()})
		if m.Jobs != nil {
			m.Jobs.Publish(run.JobID, "warning", map[string]any{"runtime": "native", "fallback": "cli", "reason": err.Error()})
		}
		return ProcessOutput{}, false
	}
	out.ExitCode = -1
	if out.StderrPreview == "" {
		out.StderrPreview = err.Error()
	}
	return out, true
}

func (m *Manager) emitCompletion(run db.RunnerRun, finalStatus string, out ProcessOutput) {
	m.emitCompletionWithSeq(run, finalStatus, out, out.EventSeq+1)
}

func (m *Manager) emitCompletionWithSeq(run db.RunnerRun, finalStatus string, out ProcessOutput, seq int64) {
	completionPayload, _ := json.Marshal(map[string]any{
		"exit_code":          out.ExitCode,
		"duration_ms":        out.DurationMS,
		"final_text":         truncateString(out.FinalTextPreview, 200),
		"final_text_preview": truncateString(out.FinalTextPreview, 200),
		"stdout_preview":     truncateString(out.StdoutPreview, 200),
		"stderr_preview":     truncateString(out.StderrPreview, 200),
	})
	completionEvent := RunnerRunEvent{
		Type:       "completion",
		TS:         time.Now().UTC().Format(time.RFC3339Nano),
		Seq:        seq,
		JobID:      run.JobID,
		RunnerID:   run.RunnerID,
		Payload:    completionPayload,
		Status:     finalStatus,
		DurationMS: out.DurationMS,
	}
	m.persistEvent(run, completionEvent)
	if m.Jobs != nil {
		m.Jobs.Publish(run.JobID, "completion", map[string]any{
			"exit_code":          out.ExitCode,
			"duration_ms":        out.DurationMS,
			"final_text":         out.FinalTextPreview,
			"final_text_preview": out.FinalTextPreview,
			"stdout_preview":     out.StdoutPreview,
			"stderr_preview":     out.StderrPreview,
			"status":             finalStatus,
		})
	}
}

func (m *Manager) recoverRunPanic(run db.RunnerRun) {
	if recovered := recover(); recovered != nil {
		log.Printf("runner worker recovered panic: run=%s err=%v", run.ID, recovered)
		m.finalizeRun(context.Background(), run, db.RunnerRunStatusFailed, "runner worker recovered after an internal failure", ProcessOutput{ExitCode: -1})
	}
}

func (m *Manager) pauseRunForNativeApproval(run db.RunnerRun, out ProcessOutput) {
	status := db.RunnerRunStatusApprovalRequired
	m.emitCompletion(run, status, out)
	if m.Jobs != nil {
		m.Jobs.PauseForApproval(run.JobID, map[string]any{
			"runner_id": run.RunnerID,
			"run_id":    run.ID,
			"status":    status,
		})
	}
}

// ResumeNativeRunAfterApproval continues a native run that paused for operator
// approval. The underlying runner_runs row must still be running.
func (m *Manager) ResumeNativeRunAfterApproval(ctx context.Context, run db.RunnerRun) error {
	if m == nil {
		return errors.New("runner manager not configured")
	}
	chatReq, ok := buildRuntimeChatRequest(run)
	if !ok {
		return errors.New("run is not a native chat execution")
	}
	if m.Runtimes == nil {
		return errors.New("no runner registry configured")
	}
	runtime, ok := m.Runtimes.Get(RunnerID(run.RunnerID))
	if !ok {
		return errors.New("runner runtime not found")
	}
	continuer, ok := runtime.(NativeTurnContinuer)
	if !ok {
		return errors.New("runner does not support native approval continuation")
	}
	cfg := m.configSnapshot()
	env := nativeEnv(cfg)
	runnerID := RunnerID(run.RunnerID)
	if additionalEnv, err := m.runnerAdditionalEnv(runnerID, parseAgentRunMeta(run.MetaJSON)); err != nil {
		return err
	} else if len(additionalEnv) > 0 {
		env = mergeEnvOverlay(env, additionalEnv)
	}
	var maxSeq int64
	out, err := continuer.ContinuePendingTurn(ctx, NativeRuntimeExecuteRequest{
		Run:    run,
		Chat:   chatReq,
		Config: cfg,
		Env:    env,
		OnEvent: func(e RunnerRunEvent) {
			e.JobID = run.JobID
			e.RunnerID = run.RunnerID
			updateMaxSeq(&maxSeq, e.Seq)
			m.persistEvent(run, e)
			if m.Jobs != nil {
				m.Jobs.Publish(run.JobID, e.Type, eventToMap(e))
			}
		},
	})
	out.EventSeq = maxSeq
	if errors.Is(err, errNativeApprovalRequired) {
		m.pauseRunForNativeApproval(run, out)
		return nil
	}
	finalStatus := db.RunnerRunStatusSucceeded
	errMsg := ""
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		finalStatus = db.RunnerRunStatusTimedOut
		errMsg = "timed out"
	} else if errors.Is(ctx.Err(), context.Canceled) {
		finalStatus = db.RunnerRunStatusAborted
		errMsg = "aborted"
	} else if err != nil || out.ExitCode != 0 {
		finalStatus = db.RunnerRunStatusFailed
		if err != nil && errMsg == "" {
			errMsg = err.Error()
		}
		if errMsg == "" {
			errMsg = out.StderrPreview
		}
	}
	m.emitCompletion(run, finalStatus, out)
	m.finalizeRun(ctx, run, finalStatus, errMsg, out)
	return err
}

func (m *Manager) finalizeRun(ctx context.Context, run db.RunnerRun, status, errMsg string, out ProcessOutput) {
	finalizeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), runnerFinalizeTimeout)
	defer cancel()
	cfg := m.configSnapshot()

	fin := db.RunnerRunFinalizeInput{
		Status:           status,
		ExitCode:         out.ExitCode,
		StdoutPreview:    truncateString(out.StdoutPreview, cfg.PreviewMaxBytes),
		StderrPreview:    truncateString(out.StderrPreview, cfg.PreviewMaxBytes),
		FinalTextPreview: truncateString(out.FinalTextPreview, cfg.PreviewMaxBytes),
		ErrorMessage:     errMsg,
		CompletedAt:      db.NowMS(),
	}
	if err := m.DB.FinalizeRunnerRun(finalizeCtx, run.ID, fin); err != nil {
		log.Printf("finalize runner run failed: run=%s err=%v", run.ID, err)
		return
	}
	if m.Jobs != nil {
		switch status {
		case db.RunnerRunStatusSucceeded:
			m.Jobs.Complete(run.JobID, status, map[string]any{
				"runner_id":          run.RunnerID,
				"run_id":             run.ID,
				"final_text":         fin.FinalTextPreview,
				"final_text_preview": fin.FinalTextPreview,
				"stdout_preview":     fin.StdoutPreview,
				"stderr_preview":     fin.StderrPreview,
				"exit_code":          fin.ExitCode,
				"duration_ms":        out.DurationMS,
			})
		case db.RunnerRunStatusFailed, db.RunnerRunStatusTimedOut:
			m.Jobs.Fail(run.JobID, errMsg, map[string]any{
				"runner_id":      run.RunnerID,
				"run_id":         run.ID,
				"stderr_preview": fin.StderrPreview,
				"exit_code":      fin.ExitCode,
			})
		case db.RunnerRunStatusAborted:
			m.Jobs.Complete(run.JobID, "aborted", map[string]any{
				"runner_id": run.RunnerID,
				"run_id":    run.ID,
				"message":   errMsg,
			})
		}
	}
}

func (m *Manager) persistEvent(run db.RunnerRun, e RunnerRunEvent) {
	payloadJSON := ""
	if len(e.Payload) > 0 {
		payloadJSON = string(e.Payload)
	}
	_ = m.DB.AppendRunnerRunEvent(context.Background(), db.RunnerRunEvent{
		RunID:       run.ID,
		JobID:       run.JobID,
		Seq:         e.Seq,
		TS:          e.TS,
		Type:        e.Type,
		Stream:      e.Stream,
		Chunk:       e.Chunk,
		PayloadJSON: payloadJSON,
	})
}

func (m *Manager) reconcileInterruptedRun(run db.RunnerRun, reason string) {
	ctx := context.Background()
	fin := db.RunnerRunFinalizeInput{
		Status:       db.RunnerRunStatusAborted,
		ErrorMessage: reason,
		CompletedAt:  db.NowMS(),
	}
	if err := m.DB.FinalizeRunnerRun(ctx, run.ID, fin); err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			log.Printf("reconcile interrupted run failed: run=%s err=%v", run.ID, err)
		}
		return
	}
	if m.Jobs != nil {
		m.Jobs.Publish(run.JobID, "completion", map[string]any{
			"status":  db.RunnerRunStatusAborted,
			"message": reason,
		})
		m.Jobs.Complete(run.JobID, "aborted", map[string]any{
			"runner_id": run.RunnerID,
			"run_id":    run.ID,
			"message":   reason,
		})
	}
}

func (m *Manager) signal() {
	m.signalN(1)
}

func (m *Manager) signalN(n int) {
	if n <= 0 {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.started || m.notifyCh == nil {
		return
	}
	for i := 0; i < n; i++ {
		select {
		case m.notifyCh <- struct{}{}:
		default:
			return
		}
	}
}

func eventToMap(e RunnerRunEvent) map[string]any {
	out := map[string]any{
		"type":        e.Type,
		"seq":         e.Seq,
		"stream":      e.Stream,
		"chunk":       e.Chunk,
		"runner_id":   e.RunnerID,
		"job_id":      e.JobID,
		"status":      e.Status,
		"message":     e.Message,
		"duration_ms": e.DurationMS,
	}
	if len(e.Payload) > 0 {
		var payload any
		if err := json.Unmarshal(e.Payload, &payload); err == nil {
			out["payload"] = payload
		} else {
			out["payload_json"] = string(e.Payload)
		}
	}
	return out
}

func (m *Manager) buildCommandSpecForRun(ctx context.Context, run db.RunnerRun) (CommandSpec, error) {
	if m.Registry == nil {
		return CommandSpec{}, fmt.Errorf("no runner registry configured")
	}
	cfg := m.configSnapshot()
	meta := parseAgentRunMeta(run.MetaJSON)
	model := strings.TrimSpace(run.Model)
	if RunnerID(run.RunnerID) == RunnerOpenCode && model != "" && strings.TrimSpace(asString(meta["runner_chat_continuation_mode"])) != string(ContinuationNative) {
		model = OpenCodeCLIModelFlag(ctx, cfg, nativeEnv(cfg), model)
	}
	req := RunnerRunRequest{
		RunnerID:  run.RunnerID,
		Task:      run.Task,
		Cwd:       run.Cwd,
		Model:     model,
		Mode:      run.Mode,
		Isolation: run.Isolation,
		Meta:      meta,
	}
	if mt, ok := meta["_max_turns"]; ok {
		switch v := mt.(type) {
		case float64:
			req.MaxTurns = int(v)
		case int:
			req.MaxTurns = v
		}
	}
	if sessionID := strings.TrimSpace(stringMeta(meta, "runner_chat_session_id")); sessionID != "" {
		chatReq := RunnerChatCommandRequest{
			SessionID:        sessionID,
			TurnID:           stringMeta(meta, "runner_chat_turn_id"),
			NativeSessionRef: stringMeta(meta, "runner_chat_native_session_ref"),
			ContinuationMode: ContinuationMode(firstNonEmptyStringMeta(meta, "runner_chat_continuation_mode", string(ContinuationReplay))),
			ReplayPrompt:     firstNonEmptyStringMeta(meta, "runner_chat_replay_prompt", run.Task),
			UserMessage:      firstNonEmptyStringMeta(meta, "runner_chat_user_message", run.Task),
			MemoryRefresh:    stringMeta(meta, "runner_chat_memory_refresh"),
			Model:            model,
			Mode:             run.Mode,
			Isolation:        run.Isolation,
			MaxTurns:         req.MaxTurns,
			Cwd:              run.Cwd,
			TimeoutSeconds:   run.TimeoutSeconds,
			Meta:             meta,
		}
		return m.Registry.BuildChatCommand(RunnerID(run.RunnerID), chatReq)
	}
	return m.Registry.BuildCommand(req)
}

func parseAgentRunMeta(metaJSON string) map[string]any {
	if strings.TrimSpace(metaJSON) == "" || metaJSON == "{}" {
		return map[string]any{}
	}
	var meta map[string]any
	if err := json.Unmarshal([]byte(metaJSON), &meta); err != nil || meta == nil {
		return map[string]any{}
	}
	return meta
}

func stringMeta(meta map[string]any, key string) string {
	if meta == nil {
		return ""
	}
	value, _ := meta[key].(string)
	return strings.TrimSpace(value)
}

func firstNonEmptyStringMeta(meta map[string]any, key string, fallback string) string {
	if value := stringMeta(meta, key); value != "" {
		return value
	}
	return strings.TrimSpace(fallback)
}

func newRunnerJobID() string {
	var raw [12]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return fmt.Sprintf("job-runner-%d", time.Now().UnixNano())
	}
	return "job-runner-" + hex.EncodeToString(raw[:])
}

func truncateString(s string, maxBytes int) string {
	if maxBytes <= 0 || len(s) <= maxBytes {
		return s
	}
	return s[:maxBytes]
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func updateMaxSeq(maxSeq *int64, seq int64) {
	for {
		current := atomic.LoadInt64(maxSeq)
		if seq <= current {
			return
		}
		if atomic.CompareAndSwapInt64(maxSeq, current, seq) {
			return
		}
	}
}

func (m *Manager) configSnapshot() config.RunnersConfig {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.Cfg
}

// ApplyConfig refreshes live policy/config values used by newly enqueued runs.
func (m *Manager) ApplyConfig(cfg config.RunnersConfig, maxConcurrent, maxQueued int, timeout time.Duration, openCodeDirs []string, restrictDir string) {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.Cfg = cfg
	if maxConcurrent > 0 {
		m.MaxConcurrent = maxConcurrent
	}
	if maxQueued > 0 {
		m.MaxQueued = maxQueued
	}
	if timeout > 0 {
		m.TaskTimeout = timeout
	}
	m.OpenCodeExternalDirectories = append([]string{}, openCodeDirs...)
	m.RestrictDir = restrictDir
	m.mu.Unlock()
	if m.Registry != nil {
		m.Registry.RefreshAllAsync(m.detectOptions(cfg))
	}
}

// DetectOptions returns the environment-aware runner detection options used by this manager.
func (m *Manager) DetectOptions() DetectOptions {
	if m == nil {
		return DetectOptions{Env: SecretStrippedEnv()}
	}
	return m.detectOptions(m.configSnapshot())
}

func (m *Manager) detectOptions(cfg config.RunnersConfig) DetectOptions {
	additionalEnv := map[string]string(nil)
	if env, err := codexHomeEnv(cfg); err == nil {
		additionalEnv = env
	}
	return DetectOptions{
		DisabledRunners: cfg.Disabled,
		Env:             BuildRunnerEnv(os.Environ(), cfg.ChildEnvAllowlist, additionalEnv),
	}
}

func (m *Manager) runnerAdditionalEnv(runnerID RunnerID, meta map[string]any) (map[string]string, error) {
	if m == nil {
		return nil, nil
	}
	if runnerID == RunnerCodex {
		return codexHomeEnv(m.configSnapshot())
	}
	if runnerID != RunnerOpenCode {
		return nil, nil
	}
	directories := m.openCodeExternalDirectoriesSnapshot()
	if permission, ok := runnerPermissionFromMeta(meta); ok {
		directories = append(directories, permission.TargetPath)
	}
	return buildOpenCodeConfigEnv(directories), nil
}

func (m *Manager) openCodeExternalDirectoriesSnapshot() []string {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.OpenCodeExternalDirectories) == 0 {
		return nil
	}
	return append([]string{}, m.OpenCodeExternalDirectories...)
}

func isRunnerDisabled(id RunnerID, disabled []string) bool {
	for _, candidate := range disabled {
		if strings.EqualFold(strings.TrimSpace(candidate), string(id)) {
			return true
		}
	}
	return false
}
