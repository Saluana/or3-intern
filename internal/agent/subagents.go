package agent

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"or3-intern/internal/db"
	"or3-intern/internal/providers"
	"or3-intern/internal/tools"
)

const (
	subagentClaimRetryDelay = 25 * time.Millisecond
	subagentFinalizeTimeout = 5 * time.Second
)

// SubagentManager queues and runs background subagent jobs.
type SubagentManager struct {
	DB              *db.DB
	Runtime         *Runtime
	Deliver         Deliverer
	MaxConcurrent   int
	MaxQueued       int
	TaskTimeout     time.Duration
	BackgroundTools func() *tools.Registry
	Jobs            *JobRegistry

	mu       sync.Mutex
	started  bool
	ctx      context.Context
	cancel   context.CancelFunc
	notifyCh chan struct{}
	wg       sync.WaitGroup
}

// ServiceSubagentRequest describes a service-originated subagent request.
type ServiceSubagentRequest struct {
	ParentSessionKey string
	Task             string
	PromptSnapshot   []providers.ChatMessage
	AllowedTools     []string
	RestrictTools    bool
	ProfileName      string
	Channel          string
	ReplyTo          string
	Meta             map[string]any
	Timeout          time.Duration
}

type subagentJobMetadata struct {
	ProfileName    string                  `json:"profile_name,omitempty"`
	AllowedTools   []string                `json:"allowed_tools,omitempty"`
	RestrictTools  bool                    `json:"restrict_tools,omitempty"`
	PromptSnapshot []providers.ChatMessage `json:"prompt_snapshot,omitempty"`
	TimeoutSeconds int                     `json:"timeout_seconds,omitempty"`
	ServiceMeta    map[string]any          `json:"service_meta,omitempty"`
}

// Start launches the background workers and resumes queued jobs.
func (m *SubagentManager) Start(ctx context.Context) error {
	if m == nil {
		return fmt.Errorf("subagent manager is nil")
	}
	if m.DB == nil {
		return fmt.Errorf("subagent db not configured")
	}
	if m.Runtime == nil {
		return fmt.Errorf("subagent runtime not configured")
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
		m.MaxQueued = 32
	}
	if m.TaskTimeout <= 0 {
		m.TaskTimeout = 5 * time.Minute
	}
	running, err := m.DB.ListRunningSubagentJobs(ctx)
	if err != nil {
		return err
	}
	queued, err := m.DB.ListQueuedSubagentJobs(ctx)
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
	for _, job := range running {
		m.reconcileInterruptedJob(job, "subagent interrupted during restart")
	}
	if len(queued) > 0 {
		m.signalN(min(len(queued), m.MaxConcurrent))
	}
	return nil
}

// Stop cancels workers and waits for them to exit.
func (m *SubagentManager) Stop(ctx context.Context) error {
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

// Enqueue stores a tool-originated subagent request and signals workers.
func (m *SubagentManager) Enqueue(ctx context.Context, req tools.SpawnRequest) (tools.SpawnJob, error) {
	if m == nil || m.DB == nil {
		return tools.SpawnJob{}, fmt.Errorf("background subagents disabled")
	}
	task := strings.TrimSpace(req.Task)
	if task == "" {
		return tools.SpawnJob{}, fmt.Errorf("empty task")
	}
	parentSessionKey := strings.TrimSpace(req.ParentSessionKey)
	if parentSessionKey == "" {
		return tools.SpawnJob{}, fmt.Errorf("missing parent session")
	}
	jobID := newSubagentID()
	metadata := map[string]any{}
	if profileName := strings.TrimSpace(req.ProfileName); profileName != "" {
		metadata["profile_name"] = profileName
	}
	metadataJSON := mustMetadataJSON(metadata)
	job := db.SubagentJob{
		ID:               jobID,
		ParentSessionKey: parentSessionKey,
		ChildSessionKey:  childSessionKey(parentSessionKey, jobID),
		Channel:          strings.TrimSpace(req.Channel),
		ReplyTo:          strings.TrimSpace(req.To),
		Task:             task,
		Status:           db.SubagentStatusQueued,
		MetadataJSON:     metadataJSON,
	}
	if err := m.DB.EnqueueSubagentJobLimited(ctx, job, m.MaxQueued); err != nil {
		return tools.SpawnJob{}, err
	}
	if m.Jobs != nil {
		m.Jobs.RegisterWithID(job.ID, "subagent")
		m.Jobs.Publish(job.ID, "queued", map[string]any{"status": db.SubagentStatusQueued, "child_session_key": job.ChildSessionKey})
	}
	m.signal()
	return tools.SpawnJob{ID: job.ID, ChildSessionKey: job.ChildSessionKey}, nil
}

// EnqueueService stores a service-originated subagent request and signals workers.
func (m *SubagentManager) EnqueueService(ctx context.Context, req ServiceSubagentRequest) (tools.SpawnJob, error) {
	if m == nil || m.DB == nil {
		return tools.SpawnJob{}, fmt.Errorf("background subagents disabled")
	}
	parentSessionKey := strings.TrimSpace(req.ParentSessionKey)
	if parentSessionKey == "" {
		return tools.SpawnJob{}, fmt.Errorf("missing parent session")
	}
	task := strings.TrimSpace(req.Task)
	if task == "" {
		return tools.SpawnJob{}, fmt.Errorf("empty task")
	}
	jobID := newSubagentID()
	metadata := subagentJobMetadata{
		ProfileName:    strings.TrimSpace(req.ProfileName),
		AllowedTools:   append([]string{}, req.AllowedTools...),
		RestrictTools:  req.RestrictTools,
		PromptSnapshot: append([]providers.ChatMessage{}, req.PromptSnapshot...),
		ServiceMeta:    cloneMap(req.Meta),
	}
	if req.Timeout > 0 {
		metadata.TimeoutSeconds = int(req.Timeout / time.Second)
	}
	job := db.SubagentJob{
		ID:               jobID,
		ParentSessionKey: parentSessionKey,
		ChildSessionKey:  childSessionKey(parentSessionKey, jobID),
		Channel:          strings.TrimSpace(req.Channel),
		ReplyTo:          strings.TrimSpace(req.ReplyTo),
		Task:             task,
		Status:           db.SubagentStatusQueued,
		MetadataJSON:     mustMetadataJSON(metadata),
	}
	if err := m.DB.EnqueueSubagentJobLimited(ctx, job, m.MaxQueued); err != nil {
		return tools.SpawnJob{}, err
	}
	if m.Jobs != nil {
		m.Jobs.RegisterWithID(job.ID, "subagent")
		m.Jobs.Publish(job.ID, "queued", serviceLifecycleEventPayload(metadata.ServiceMeta, map[string]any{"status": db.SubagentStatusQueued, "child_session_key": job.ChildSessionKey}))
	}
	m.signal()
	return tools.SpawnJob{ID: job.ID, ChildSessionKey: job.ChildSessionKey}, nil
}

func (m *SubagentManager) workerLoop() {
	defer m.wg.Done()
	for {
		ran, err := m.runOnce()
		if err != nil {
			if !errors.Is(err, context.Canceled) {
				log.Printf("subagent worker error: %v", err)
			}
		}
		if ran {
			continue
		}
		select {
		case <-m.ctx.Done():
			return
		case <-m.notifyCh:
		case <-time.After(subagentClaimRetryDelay):
		}
	}
}

func (m *SubagentManager) runOnce() (bool, error) {
	job, err := m.DB.ClaimNextSubagentJob(m.ctx)
	if err != nil || job == nil {
		return false, err
	}
	m.executeJob(*job)
	return true, nil
}

func (m *SubagentManager) executeJob(job db.SubagentJob) {
	timeout := m.jobTimeout(job)
	runCtx, cancel := context.WithTimeout(m.ctx, timeout)
	defer cancel()
	metadata := parseSubagentJobMetadata(job.MetadataJSON)
	if m.Jobs != nil {
		m.Jobs.AttachCancel(job.ID, cancel)
		m.Jobs.Publish(job.ID, "started", serviceLifecycleEventPayload(metadata.ServiceMeta, map[string]any{"status": db.SubagentStatusRunning, "child_session_key": job.ChildSessionKey}))
	}
	result, err := m.runJob(runCtx, job)
	if err != nil {
		reason := strings.TrimSpace(err.Error())
		switch {
		case errors.Is(err, context.Canceled), errors.Is(runCtx.Err(), context.Canceled):
			m.finalizeJob(runCtx, job, db.SubagentStatusInterrupted, "", "", reasonOrDefault(reason, "subagent interrupted"), true)
		case errors.Is(err, context.DeadlineExceeded), errors.Is(runCtx.Err(), context.DeadlineExceeded):
			m.finalizeJob(runCtx, job, db.SubagentStatusFailed, "", "", reasonOrDefault(reason, "subagent timed out"), true)
		default:
			m.finalizeJob(runCtx, job, db.SubagentStatusFailed, "", "", reasonOrDefault(reason, "subagent failed"), true)
		}
		return
	}
	m.finalizeJob(runCtx, job, db.SubagentStatusSucceeded, result.Preview, result.ArtifactID, "", true)
}

func (m *SubagentManager) runJob(ctx context.Context, job db.SubagentJob) (BackgroundRunResult, error) {
	metadata := parseSubagentJobMetadata(job.MetadataJSON)
	promptSnapshot := append([]providers.ChatMessage{}, metadata.PromptSnapshot...)
	var err error
	if len(promptSnapshot) == 0 {
		promptSnapshot, err = m.Runtime.BuildPromptSnapshot(ctx, job.ParentSessionKey, job.Task)
		if err != nil {
			return BackgroundRunResult{}, err
		}
	}
	if m.Jobs != nil {
		ctx = ContextWithConversationObserver(ctx, m.Jobs.Observer(job.ID))
		ctx = ContextWithStreamingChannel(ctx, NullStreamer{})
	}
	return m.Runtime.RunBackground(ctx, BackgroundRunInput{
		SessionKey:       job.ChildSessionKey,
		ParentSessionKey: job.ParentSessionKey,
		Task:             job.Task,
		PromptSnapshot:   promptSnapshot,
		Tools:            toolRegistryWithAllowlist(m.backgroundTools(), metadata.AllowedTools, metadata.RestrictTools),
		Meta: map[string]any{
			"subagent_job_id":    job.ID,
			"parent_session_key": job.ParentSessionKey,
			"profile_name":       metadata.ProfileName,
		},
		Channel: job.Channel,
		ReplyTo: job.ReplyTo,
	})
}

func (m *SubagentManager) backgroundTools() *tools.Registry {
	if m.BackgroundTools != nil {
		return m.BackgroundTools()
	}
	return tools.NewRegistry()
}

func (m *SubagentManager) finalizeJob(baseCtx context.Context, job db.SubagentJob, status string, preview string, artifactID string, errText string, deliver bool) {
	finalizeCtx, cancel := boundedContext(baseCtx, subagentFinalizeTimeout)
	defer cancel()
	success := status == db.SubagentStatusSucceeded
	text := formatParentSubagentSummary(job, success, preview, artifactID, errText)
	metadata := parseSubagentJobMetadata(job.MetadataJSON)
	payload := map[string]any{
		"subagent_job_id": job.ID,
		"child_session":   job.ChildSessionKey,
		"status":          status,
	}
	if artifactID != "" {
		payload["artifact_id"] = artifactID
	}
	if err := m.DB.FinalizeSubagentJob(finalizeCtx, job, status, preview, artifactID, errText, text, payload); err != nil {
		log.Printf("finalize subagent failed: job=%s err=%v", job.ID, err)
		return
	}
	if m.Jobs != nil {
		if status == db.SubagentStatusSucceeded {
			m.Jobs.Complete(job.ID, status, serviceLifecycleEventPayload(metadata.ServiceMeta, map[string]any{"preview": preview, "artifact_id": artifactID, "child_session_key": job.ChildSessionKey}))
		} else if status == db.SubagentStatusInterrupted {
			m.Jobs.Complete(job.ID, "aborted", serviceLifecycleEventPayload(metadata.ServiceMeta, map[string]any{"message": errText, "child_session_key": job.ChildSessionKey}))
		} else {
			m.Jobs.Fail(job.ID, errText, serviceLifecycleEventPayload(metadata.ServiceMeta, map[string]any{"child_session_key": job.ChildSessionKey}))
		}
	}
	if deliver {
		m.deliverCompletion(finalizeCtx, job, success, preview, artifactID, errText)
	}
}

// Abort cancels the running or queued subagent job with id.
func (m *SubagentManager) Abort(ctx context.Context, id string) error {
	if m == nil || m.DB == nil {
		return fmt.Errorf("background subagents disabled")
	}
	if m.Jobs != nil && m.Jobs.Cancel(id) {
		return nil
	}
	job, ok, err := m.DB.AbortQueuedSubagentJob(ctx, id, "subagent aborted before execution")
	if err != nil {
		return err
	}
	if !ok {
		stored, exists, lookupErr := m.DB.GetSubagentJob(ctx, id)
		if lookupErr != nil {
			return lookupErr
		}
		if !exists {
			return fmt.Errorf("job not found")
		}
		if stored.Status == db.SubagentStatusQueued {
			return fmt.Errorf("job is not abortable")
		}
		return fmt.Errorf("job is not abortable")
	}
	if m.Jobs != nil {
		m.Jobs.Complete(id, "aborted", map[string]any{"message": "subagent aborted before execution", "child_session_key": job.ChildSessionKey})
	}
	return nil
}

func (m *SubagentManager) jobTimeout(job db.SubagentJob) time.Duration {
	metadata := parseSubagentJobMetadata(job.MetadataJSON)
	if metadata.TimeoutSeconds > 0 {
		return time.Duration(metadata.TimeoutSeconds) * time.Second
	}
	if m.TaskTimeout <= 0 {
		return 5 * time.Minute
	}
	return m.TaskTimeout
}

func parseSubagentJobMetadata(raw string) subagentJobMetadata {
	if strings.TrimSpace(raw) == "" {
		return subagentJobMetadata{}
	}
	var metadata subagentJobMetadata
	if err := json.Unmarshal([]byte(raw), &metadata); err != nil {
		var legacy map[string]any
		if legacyErr := json.Unmarshal([]byte(raw), &legacy); legacyErr != nil {
			return subagentJobMetadata{}
		}
		metadata.ProfileName = strings.TrimSpace(fmt.Sprint(legacy["profile_name"]))
		return metadata
	}
	metadata.ProfileName = strings.TrimSpace(metadata.ProfileName)
	return metadata
}

func serviceLifecycleEventPayload(serviceMeta map[string]any, payload map[string]any) map[string]any {
	out := map[string]any{}
	for key, value := range payload {
		out[key] = value
	}
	for _, key := range []string{"request_id", "workspace_id", "network_session_id"} {
		if value, ok := serviceMeta[key]; ok {
			out[key] = value
		}
	}
	return out
}

func mustMetadataJSON(payload any) string {
	if payload == nil {
		return "{}"
	}
	b, err := json.Marshal(payload)
	if err != nil || len(b) == 0 {
		return "{}"
	}
	return string(b)
}

func (m *SubagentManager) reconcileInterruptedJob(job db.SubagentJob, reason string) {
	m.finalizeJob(m.ctx, job, db.SubagentStatusInterrupted, "", "", reasonOrDefault(reason, "subagent interrupted during restart"), false)
}

func (m *SubagentManager) deliverCompletion(ctx context.Context, job db.SubagentJob, success bool, preview string, artifactID string, errText string) {
	deliverer := m.Deliver
	if deliverer == nil && m.Runtime != nil {
		deliverer = m.Runtime.Deliver
	}
	if deliverer == nil || strings.TrimSpace(job.Channel) == "" || strings.TrimSpace(job.ReplyTo) == "" {
		return
	}
	text := formatDeliverySubagentSummary(job, success, preview, artifactID, errText)
	if err := deliverer.Deliver(ctx, job.Channel, job.ReplyTo, text); err != nil {
		log.Printf("subagent delivery failed: job=%s err=%v", job.ID, err)
	}
}

func (m *SubagentManager) signal() {
	m.signalN(1)
}

func (m *SubagentManager) signalN(n int) {
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

func boundedContext(base context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if base == nil {
		base = context.Background()
	} else {
		base = context.WithoutCancel(base)
	}
	if timeout <= 0 {
		return context.WithCancel(base)
	}
	return context.WithTimeout(base, timeout)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func childSessionKey(parentSessionKey, jobID string) string {
	return parentSessionKey + ":subagent:" + jobID
}

func newSubagentID() string {
	var raw [12]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return fmt.Sprintf("job-%d", time.Now().UnixNano())
	}
	return "job-" + hex.EncodeToString(raw[:])
}

func reasonOrDefault(reason string, fallback string) string {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return fallback
	}
	return reason
}

func formatParentSubagentSummary(job db.SubagentJob, success bool, preview string, artifactID string, errText string) string {
	if success {
		text := fmt.Sprintf("Background job %s completed: %s", job.ID, preview)
		if artifactID != "" {
			text += fmt.Sprintf("\nartifact_id=%s", artifactID)
		}
		return text
	}
	return fmt.Sprintf("Background job %s failed: %s", job.ID, reasonOrDefault(errText, "unknown error"))
}

func formatDeliverySubagentSummary(job db.SubagentJob, success bool, preview string, artifactID string, errText string) string {
	if success {
		text := fmt.Sprintf("Background job %s finished. %s", job.ID, preview)
		if artifactID != "" {
			text += fmt.Sprintf("\nartifact_id=%s", artifactID)
		}
		return text
	}
	return fmt.Sprintf("Background job %s failed. %s", job.ID, reasonOrDefault(errText, "unknown error"))
}
