package agentcli

import (
	"context"
	"crypto/rand"
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

	"or3-intern/internal/agent"
	"or3-intern/internal/config"
	"or3-intern/internal/db"
)

const (
	agentCLIClaimRetryDelay = 25 * time.Millisecond
	agentCLIFinalizeTimeout = 5 * time.Second
	agentCLIDetectCacheTTL  = 30 * time.Second
)

// Manager queues and runs external agent CLI jobs.
type Manager struct {
	DB       *db.DB
	Jobs     *agent.JobRegistry
	Cfg      config.AgentCLIConfig
	Registry *RunnerRegistry
	Process  *ProcessManager

	MaxConcurrent int
	MaxQueued     int
	TaskTimeout   time.Duration

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
		return fmt.Errorf("agent CLI manager is nil")
	}
	if m.DB == nil {
		return fmt.Errorf("agent CLI db not configured")
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
	m.Registry.RefreshAllAsync(DetectOptions{DisabledRunners: m.Cfg.DisabledRunners})
	running, err := m.DB.ListRunningAgentCLIRuns(ctx)
	if err != nil {
		return err
	}
	queued, err := m.DB.ListQueuedAgentCLIRuns(ctx)
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

// Stop cancels workers and waits for them to exit.
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
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Enqueue validates, persists, and signals a new CLI run.
func (m *Manager) Enqueue(ctx context.Context, req AgentRunRequest) (db.AgentCLIRun, error) {
	if m == nil || m.DB == nil {
		return db.AgentCLIRun{}, fmt.Errorf("agent CLI manager is not available")
	}
	cfg := m.configSnapshot()
	if !cfg.Enabled {
		return db.AgentCLIRun{}, fmt.Errorf("agent CLI delegation is disabled")
	}
	parentSessionKey := strings.TrimSpace(req.ParentSessionKey)
	if parentSessionKey == "" {
		return db.AgentCLIRun{}, fmt.Errorf("missing parent session")
	}
	task := strings.TrimSpace(req.Task)
	if task == "" {
		return db.AgentCLIRun{}, fmt.Errorf("empty task")
	}
	runnerID := strings.TrimSpace(req.RunnerID)
	if runnerID == "" {
		return db.AgentCLIRun{}, fmt.Errorf("missing runner_id")
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
		return db.AgentCLIRun{}, fmt.Errorf("policy validation: %w", err)
	}

	// Check runner readiness
	if m.Registry != nil {
		if _, ok := m.Registry.Spec(RunnerID(runnerID)); !ok {
			return db.AgentCLIRun{}, fmt.Errorf("unknown runner %q", runnerID)
		}
		if isRunnerDisabled(RunnerID(runnerID), cfg.DisabledRunners) {
			return db.AgentCLIRun{}, fmt.Errorf("runner %q is disabled by config", runnerID)
		}
		if RunnerID(runnerID) != RunnerOR3 {
			detectOpts := DetectOptions{DisabledRunners: cfg.DisabledRunners}
			if info, ok := m.Registry.DetectCached(RunnerID(runnerID), agentCLIDetectCacheTTL); ok {
				switch info.Status {
				case RunnerStatusDisabledByConfig:
					return db.AgentCLIRun{}, fmt.Errorf("runner %q is disabled by config", runnerID)
				case RunnerStatusMissing:
					return db.AgentCLIRun{}, fmt.Errorf("runner %q is not installed", runnerID)
				case RunnerStatusAuthMissing:
					return db.AgentCLIRun{}, fmt.Errorf("runner %q is not authenticated", runnerID)
				case RunnerStatusError:
					return db.AgentCLIRun{}, fmt.Errorf("runner %q is not functional", runnerID)
				}
			} else {
				m.Registry.RefreshDetectAsync(RunnerID(runnerID), detectOpts)
			}
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

	// Default cwd
	cwd := req.Cwd

	jobID := newAgentCLIJobID()
	runID := "acr_" + newAgentCLIJobID()[:16]

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

	run := db.AgentCLIRun{
		ID:               runID,
		JobID:            jobID,
		ParentSessionKey: parentSessionKey,
		RunnerID:         runnerID,
		Task:             task,
		Cwd:              cwd,
		Model:            req.Model,
		Mode:             mode,
		Isolation:        isolation,
		Status:           db.AgentCLIStatusQueued,
		RequestedAt:      db.NowMS(),
		TimeoutSeconds:   timeoutSeconds,
		MetaJSON:         metaJSON,
	}

	if err := m.DB.EnqueueAgentCLIRunLimited(ctx, run, m.MaxQueued); err != nil {
		return db.AgentCLIRun{}, err
	}

	kind := "agent_cli:" + runnerID
	if m.Jobs != nil {
		m.Jobs.RegisterWithID(jobID, kind)
		m.Jobs.Publish(jobID, "queued", map[string]any{
			"status":    db.AgentCLIStatusQueued,
			"runner_id": runnerID,
			"run_id":    runID,
			"mode":      mode,
			"isolation": isolation,
		})
	}

	m.signal()
	return run, nil
}

// Abort cancels the running or queued agent CLI job with id.
func (m *Manager) Abort(ctx context.Context, id string) error {
	if m == nil || m.DB == nil {
		return fmt.Errorf("agent CLI manager is not available")
	}
	// First try to cancel a running job via JobRegistry
	if m.Jobs != nil && m.Jobs.Cancel(id) {
		return nil
	}
	// Then try to abort a queued job in the DB
	run, ok, err := m.DB.AbortQueuedAgentCLIRun(ctx, id, "aborted before execution")
	if err != nil {
		return err
	}
	if !ok {
		stored, exists, lookupErr := m.DB.GetAgentCLIRun(ctx, id)
		if lookupErr != nil {
			return lookupErr
		}
		if !exists {
			return fmt.Errorf("job not found")
		}
		if stored.Status == db.AgentCLIStatusQueued {
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
			if !errors.Is(err, context.Canceled) {
				log.Printf("agent CLI worker error: %v", err)
			}
		}
		if ran {
			continue
		}
		select {
		case <-m.ctx.Done():
			return
		case <-m.notifyCh:
		case <-time.After(agentCLIClaimRetryDelay):
		}
	}
}

func (m *Manager) runOnce() (bool, error) {
	run, err := m.DB.ClaimNextAgentCLIRun(m.ctx)
	if err != nil || run == nil {
		return false, err
	}
	m.executeRun(*run)
	return true, nil
}

func (m *Manager) executeRun(run db.AgentCLIRun) {
	timeout := time.Duration(run.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = m.TaskTimeout
	}
	runCtx, cancel := context.WithTimeout(m.ctx, timeout)
	defer cancel()

	if m.Jobs != nil {
		m.Jobs.AttachCancel(run.JobID, cancel)
		m.Jobs.Publish(run.JobID, "started", map[string]any{
			"status":     db.AgentCLIStatusRunning,
			"runner_id":  run.RunnerID,
			"run_id":     run.ID,
			"mode":       run.Mode,
			"isolation":  run.Isolation,
		})
	}

	// Build command through registry adapter
	req := AgentRunRequest{
		RunnerID:  run.RunnerID,
		Task:      run.Task,
		Cwd:       run.Cwd,
		Model:     run.Model,
		Mode:      run.Mode,
		Isolation: run.Isolation,
	}
	// Restore max_turns from persisted meta_json
	if run.MetaJSON != "" && run.MetaJSON != "{}" {
		var meta map[string]any
		if err := json.Unmarshal([]byte(run.MetaJSON), &meta); err == nil {
			if mt, ok := meta["_max_turns"]; ok {
				switch v := mt.(type) {
				case float64:
					req.MaxTurns = int(v)
				}
			}
		}
	}

	var cmdSpec CommandSpec
	var buildErr error
	if m.Registry != nil {
		cmdSpec, buildErr = m.Registry.BuildCommand(req)
	} else {
		buildErr = fmt.Errorf("no runner registry configured")
	}
	if buildErr != nil {
		m.finalizeRun(runCtx, run, db.AgentCLIStatusFailed, buildErr.Error(), ProcessOutput{ExitCode: -1, DurationMS: 0})
		return
	}

	// Build child environment — use os.Environ() as the base so PATH, HOME,
	// and TMPDIR are preserved through the allowlist filter.
	if len(cmdSpec.Env) == 0 {
		cmdSpec.Env = BuildAgentCLIEnv(os.Environ(), m.configSnapshot().ChildEnvAllowlist, nil)
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
	m.persistEvent(run, AgentRunEvent{
		Type:    "started",
		TS:      startedTS,
		Seq:     0,
		JobID:   run.JobID,
		RunnerID: run.RunnerID,
		Payload: startedPayload,
	})

	if m.Jobs != nil {
		m.Jobs.Publish(run.JobID, "started", map[string]any{
			"status":       db.AgentCLIStatusRunning,
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

	var eventSeq int64
	out := pm.Run(runCtx, cmdSpec, func(e AgentRunEvent) {
		e.JobID = run.JobID
		e.RunnerID = run.RunnerID
		m.persistEvent(run, e)
		if m.Jobs != nil {
			m.Jobs.Publish(run.JobID, e.Type, eventToMap(e))
		}
	})

	// Determine final status
	var finalStatus string
	switch {
	case errors.Is(runCtx.Err(), context.DeadlineExceeded):
		finalStatus = db.AgentCLIStatusTimedOut
	case errors.Is(runCtx.Err(), context.Canceled):
		finalStatus = db.AgentCLIStatusAborted
	case out.ExitCode == 0:
		finalStatus = db.AgentCLIStatusSucceeded
	default:
		finalStatus = db.AgentCLIStatusFailed
	}

	// Emit completion event
	completionPayload, _ := json.Marshal(map[string]any{
		"exit_code":     out.ExitCode,
		"duration_ms":   out.DurationMS,
		"stdout_preview": truncateString(out.StdoutPreview, 200),
		"stderr_preview": truncateString(out.StderrPreview, 200),
	})
	completionEvent := AgentRunEvent{
		Type:       "completion",
		TS:         time.Now().UTC().Format(time.RFC3339Nano),
		Seq:        atomicIncrement(&eventSeq),
		JobID:      run.JobID,
		RunnerID:   run.RunnerID,
		Payload:    completionPayload,
		Status:     finalStatus,
		DurationMS: out.DurationMS,
	}
	m.persistEvent(run, completionEvent)
	if m.Jobs != nil {
		m.Jobs.Publish(run.JobID, "completion", map[string]any{
			"exit_code":      out.ExitCode,
			"duration_ms":    out.DurationMS,
			"stdout_preview": out.StdoutPreview,
			"stderr_preview": out.StderrPreview,
			"status":         finalStatus,
		})
	}

	var errMsg string
	if finalStatus == db.AgentCLIStatusFailed {
		errMsg = out.StderrPreview
	}
	if finalStatus == db.AgentCLIStatusTimedOut {
		errMsg = "timed out"
	}
	if finalStatus == db.AgentCLIStatusAborted {
		errMsg = "aborted"
	}
	m.finalizeRun(runCtx, run, finalStatus, errMsg, out)
}

func (m *Manager) finalizeRun(ctx context.Context, run db.AgentCLIRun, status, errMsg string, out ProcessOutput) {
	finalizeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), agentCLIFinalizeTimeout)
	defer cancel()
	cfg := m.configSnapshot()

	fin := db.AgentCLIFinalizeInput{
		Status:           status,
		ExitCode:         out.ExitCode,
		StdoutPreview:    truncateString(out.StdoutPreview, cfg.PreviewMaxBytes),
		StderrPreview:    truncateString(out.StderrPreview, cfg.PreviewMaxBytes),
		FinalTextPreview: truncateString(out.FinalTextPreview, cfg.PreviewMaxBytes),
		ErrorMessage:     errMsg,
		CompletedAt:      db.NowMS(),
	}
	if err := m.DB.FinalizeAgentCLIRun(finalizeCtx, run.ID, fin); err != nil {
		log.Printf("finalize agent CLI run failed: run=%s err=%v", run.ID, err)
	}
	if m.Jobs != nil {
		switch status {
		case db.AgentCLIStatusSucceeded:
			m.Jobs.Complete(run.JobID, status, map[string]any{
				"runner_id":       run.RunnerID,
				"run_id":          run.ID,
				"stdout_preview":  fin.StdoutPreview,
				"stderr_preview":  fin.StderrPreview,
				"exit_code":       fin.ExitCode,
				"duration_ms":     out.DurationMS,
			})
		case db.AgentCLIStatusFailed, db.AgentCLIStatusTimedOut:
			m.Jobs.Fail(run.JobID, errMsg, map[string]any{
				"runner_id":      run.RunnerID,
				"run_id":         run.ID,
				"stderr_preview": fin.StderrPreview,
				"exit_code":      fin.ExitCode,
			})
		case db.AgentCLIStatusAborted:
			m.Jobs.Complete(run.JobID, "aborted", map[string]any{
				"runner_id": run.RunnerID,
				"run_id":    run.ID,
				"message":   errMsg,
			})
		}
	}
}

func (m *Manager) persistEvent(run db.AgentCLIRun, e AgentRunEvent) {
	payloadJSON := ""
	if len(e.Payload) > 0 {
		payloadJSON = string(e.Payload)
	}
	_ = m.DB.AppendAgentCLIEvent(context.Background(), db.AgentCLIEvent{
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

func (m *Manager) reconcileInterruptedRun(run db.AgentCLIRun, reason string) {
	ctx := context.Background()
	fin := db.AgentCLIFinalizeInput{
		Status:      db.AgentCLIStatusAborted,
		ErrorMessage: reason,
		CompletedAt: db.NowMS(),
	}
	_ = m.DB.FinalizeAgentCLIRun(ctx, run.ID, fin)
	if m.Jobs != nil {
		m.Jobs.Publish(run.JobID, "completion", map[string]any{
			"status":  db.AgentCLIStatusAborted,
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

func eventToMap(e AgentRunEvent) map[string]any {
	return map[string]any{
		"type":      e.Type,
		"seq":       e.Seq,
		"stream":    e.Stream,
		"chunk":     e.Chunk,
		"runner_id": e.RunnerID,
		"job_id":    e.JobID,
		"status":    e.Status,
		"message":   e.Message,
		"duration_ms": e.DurationMS,
	}
}

func newAgentCLIJobID() string {
	var raw [12]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return fmt.Sprintf("job-agentcli-%d", time.Now().UnixNano())
	}
	return "job-agentcli-" + hex.EncodeToString(raw[:])
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

func atomicIncrement(i *int64) int64 {
	return atomic.AddInt64(i, 1)
}

func (m *Manager) configSnapshot() config.AgentCLIConfig {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.Cfg
}

func isRunnerDisabled(id RunnerID, disabled []string) bool {
	for _, candidate := range disabled {
		if strings.EqualFold(strings.TrimSpace(candidate), string(id)) {
			return true
		}
	}
	return false
}
