package cron

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func mustStartService(t *testing.T, svc *Service) {
	t.Helper()
	if err := svc.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
}

func mustAddJob(t *testing.T, svc *Service, job CronJob) {
	t.Helper()
	if err := svc.Add(job); err != nil {
		t.Fatalf("Add: %v", err)
	}
}

func mustRunNow(t *testing.T, svc *Service, id string, force bool) bool {
	t.Helper()
	job, err := svc.RunNow(context.Background(), id, force)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return false
		}
		t.Fatalf("RunNow: %v", err)
	}
	if !force && !job.Enabled {
		return false
	}
	return true
}

func makeService(t *testing.T) (*Service, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "cron.json")
	svc := New(path, func(ctx context.Context, job CronJob) (RunResult, error) {
		return RunResult{}, nil
	})
	return svc, path
}

type stubScheduledTimer struct{}

func (stubScheduledTimer) Stop() bool { return true }

func TestNew(t *testing.T) {
	svc, _ := makeService(t)
	if svc == nil {
		t.Fatal("expected non-nil service")
	}
}

func TestStart_Stop(t *testing.T) {
	svc, _ := makeService(t)

	if err := svc.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	// Starting again should be a no-op
	if err := svc.Start(); err != nil {
		t.Fatalf("Start (second call): %v", err)
	}
	svc.Stop()
	// Stopping again should be a no-op
	svc.Stop()
}

func TestAdd_And_List(t *testing.T) {
	svc, _ := makeService(t)
	mustStartService(t, svc)
	defer svc.Stop()

	job := CronJob{
		Name:     "test job",
		Enabled:  true,
		Schedule: CronSchedule{Kind: KindEvery, EveryMS: 60000},
		Payload:  CronPayload{Kind: "agent_turn", Message: "hello"},
	}
	if err := svc.Add(job); err != nil {
		t.Fatalf("Add: %v", err)
	}

	jobs, err := svc.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(jobs) != 1 {
		t.Errorf("expected 1 job, got %d", len(jobs))
	}
	if jobs[0].Name != "test job" {
		t.Errorf("expected name 'test job', got %q", jobs[0].Name)
	}
}

func TestAdd_AutoGeneratesID(t *testing.T) {
	svc, _ := makeService(t)
	mustStartService(t, svc)
	defer svc.Stop()

	job := CronJob{Name: "no-id", Enabled: true, Schedule: CronSchedule{Kind: KindEvery, EveryMS: 60000}}
	mustAddJob(t, svc, job)

	jobs, _ := svc.List()
	if jobs[0].ID == "" {
		t.Error("expected auto-generated ID")
	}
}

func TestAdd_UsesNameAsIDIfMissing(t *testing.T) {
	svc, _ := makeService(t)
	mustStartService(t, svc)
	defer svc.Stop()

	job := CronJob{Enabled: true, Schedule: CronSchedule{Kind: KindEvery, EveryMS: 60000}}
	mustAddJob(t, svc, job)

	jobs, _ := svc.List()
	// ID should match Name when no Name is given either (both auto-generated)
	if jobs[0].Name == "" {
		t.Error("expected name to default to id")
	}
}

func TestAdd_SetsTimestamps(t *testing.T) {
	svc, _ := makeService(t)
	mustStartService(t, svc)
	defer svc.Stop()

	before := time.Now().UnixMilli()
	mustAddJob(t, svc, CronJob{Enabled: true, Schedule: CronSchedule{Kind: KindEvery, EveryMS: 60000}})
	after := time.Now().UnixMilli()

	jobs, _ := svc.List()
	if jobs[0].CreatedAtMS < before || jobs[0].CreatedAtMS > after {
		t.Errorf("CreatedAtMS out of range: got %d, expected [%d, %d]", jobs[0].CreatedAtMS, before, after)
	}
}

func TestRemove_Found(t *testing.T) {
	svc, _ := makeService(t)
	mustStartService(t, svc)
	defer svc.Stop()

	mustAddJob(t, svc, CronJob{ID: "job1", Enabled: true, Schedule: CronSchedule{Kind: KindEvery, EveryMS: 60000}})

	found, err := svc.Remove("job1")
	if err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if !found {
		t.Error("expected found=true")
	}

	jobs, _ := svc.List()
	if len(jobs) != 0 {
		t.Errorf("expected 0 jobs after removal, got %d", len(jobs))
	}
}

func TestRemove_NotFound(t *testing.T) {
	svc, _ := makeService(t)
	mustStartService(t, svc)
	defer svc.Stop()

	found, err := svc.Remove("nonexistent")
	if err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if found {
		t.Error("expected found=false for nonexistent job")
	}
}

func TestRunNow_Success(t *testing.T) {
	ran := false
	dir := t.TempDir()
	path := filepath.Join(dir, "cron.json")
	svc := New(path, func(ctx context.Context, job CronJob) (RunResult, error) {
		ran = true
		return RunResult{}, nil
	})
	mustStartService(t, svc)
	defer svc.Stop()

	mustAddJob(t, svc, CronJob{
		ID:       "runme",
		Enabled:  true,
		Schedule: CronSchedule{Kind: KindEvery, EveryMS: 60000},
	})

	found := mustRunNow(t, svc, "runme", false)
	if !found {
		t.Error("expected found=true")
	}
	if !ran {
		t.Error("expected runner to be called")
	}
}

func TestRunNow_NotFound(t *testing.T) {
	svc, _ := makeService(t)
	mustStartService(t, svc)
	defer svc.Stop()

	found := mustRunNow(t, svc, "missing", false)
	if found {
		t.Error("expected found=false for missing job")
	}
}

func TestRunNow_Disabled_NoForce(t *testing.T) {
	ran := false
	dir := t.TempDir()
	path := filepath.Join(dir, "cron.json")
	svc := New(path, func(ctx context.Context, job CronJob) (RunResult, error) {
		ran = true
		return RunResult{}, nil
	})
	mustStartService(t, svc)
	defer svc.Stop()

	mustAddJob(t, svc, CronJob{
		ID:       "disabled",
		Enabled:  false,
		Schedule: CronSchedule{Kind: KindEvery, EveryMS: 60000},
	})

	found := mustRunNow(t, svc, "disabled", false)
	if found {
		t.Error("expected found=false for disabled job without force")
	}
	if ran {
		t.Error("expected runner NOT to be called for disabled job without force")
	}
}

func TestRunNow_Disabled_WithForce(t *testing.T) {
	ran := false
	dir := t.TempDir()
	path := filepath.Join(dir, "cron.json")
	svc := New(path, func(ctx context.Context, job CronJob) (RunResult, error) {
		ran = true
		return RunResult{}, nil
	})
	mustStartService(t, svc)
	defer svc.Stop()

	mustAddJob(t, svc, CronJob{
		ID:       "force-run",
		Enabled:  false,
		Schedule: CronSchedule{Kind: KindEvery, EveryMS: 60000},
	})

	found := mustRunNow(t, svc, "force-run", true)
	if !found {
		t.Error("expected found=true with force")
	}
	if !ran {
		t.Error("expected runner to be called with force")
	}
}

func TestRunNow_DeleteAfterRun(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cron.json")
	svc := New(path, func(ctx context.Context, job CronJob) (RunResult, error) {
		return RunResult{}, nil
	})
	mustStartService(t, svc)
	defer svc.Stop()

	mustAddJob(t, svc, CronJob{
		ID:             "delete-after",
		Enabled:        true,
		DeleteAfterRun: true,
		Schedule:       CronSchedule{Kind: KindEvery, EveryMS: 60000},
	})

	_ = mustRunNow(t, svc, "delete-after", false)

	jobs, _ := svc.List()
	for _, j := range jobs {
		if j.ID == "delete-after" {
			t.Error("expected job to be deleted after run")
		}
	}
}

func TestStatus_NoJobs(t *testing.T) {
	svc, _ := makeService(t)
	mustStartService(t, svc)
	defer svc.Stop()

	s, err := svc.Status()
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if s["jobs"].(int) != 0 {
		t.Errorf("expected 0 jobs, got %v", s["jobs"])
	}
}

func TestStatus_WithJobs(t *testing.T) {
	svc, _ := makeService(t)
	mustStartService(t, svc)
	defer svc.Stop()

	mustAddJob(t, svc, CronJob{
		Enabled:  true,
		Schedule: CronSchedule{Kind: KindEvery, EveryMS: 60000},
	})

	s, err := svc.Status()
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if s["jobs"].(int) != 1 {
		t.Errorf("expected 1 job, got %v", s["jobs"])
	}
}

func TestStatus_NextWakeAtMS(t *testing.T) {
	svc, _ := makeService(t)
	mustStartService(t, svc)
	defer svc.Stop()

	// Add a job with a known next_run_at_ms
	next := time.Now().Add(time.Hour).UnixMilli()
	mustAddJob(t, svc, CronJob{
		Enabled:  true,
		Schedule: CronSchedule{Kind: KindEvery, EveryMS: 60000},
		State:    CronJobState{NextRunAtMS: &next},
	})

	s, err := svc.Status()
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if s["next_wake_at_ms"] == nil {
		t.Error("expected next_wake_at_ms to be set")
	}
}

func TestArmJob_KindAt_PastTime(t *testing.T) {
	svc, _ := makeService(t)
	mustStartService(t, svc)
	defer svc.Stop()

	// at time in the past - should not schedule (no panic)
	mustAddJob(t, svc, CronJob{
		ID:      "at-past",
		Enabled: true,
		Schedule: CronSchedule{
			Kind: KindAt,
			AtMS: time.Now().Add(-time.Hour).UnixMilli(),
		},
	})

	jobs, _ := svc.List()
	if len(jobs) != 1 {
		t.Errorf("expected 1 job, got %d", len(jobs))
	}
}

func TestArmJob_KindEvery_ZeroInterval(t *testing.T) {
	svc, _ := makeService(t)
	mustStartService(t, svc)
	defer svc.Stop()

	// Zero EveryMS must be rejected (must be at least 1000)
	err := svc.Add(CronJob{
		ID:      "every-zero",
		Enabled: true,
		Schedule: CronSchedule{
			Kind:    KindEvery,
			EveryMS: 0,
		},
	})
	if err == nil {
		t.Fatal("expected error for zero EveryMS")
	}
}

func TestArmJob_KindCron_ValidExpr(t *testing.T) {
	svc, _ := makeService(t)
	mustStartService(t, svc)
	defer svc.Stop()

	mustAddJob(t, svc, CronJob{
		ID:      "cron-expr",
		Enabled: true,
		Schedule: CronSchedule{
			Kind: KindCron,
			Expr: "0 * * * *",
		},
	})

	jobs, _ := svc.List()
	if len(jobs) != 1 {
		t.Errorf("expected 1 job, got %d", len(jobs))
	}
}

func TestArmJob_KindCron_InvalidExpr(t *testing.T) {
	svc, _ := makeService(t)
	mustStartService(t, svc)
	defer svc.Stop()

	// Invalid cron expr should be rejected before persistence.
	err := svc.Add(CronJob{
		ID:      "bad-expr",
		Enabled: true,
		Schedule: CronSchedule{
			Kind: KindCron,
			Expr: "not a valid cron expression at all",
		},
	})
	if err == nil {
		t.Fatal("expected invalid cron expression error")
	}

	jobs, _ := svc.List()
	if len(jobs) != 0 {
		t.Errorf("expected invalid job to be rejected, got %d jobs", len(jobs))
	}
}

func TestArmJob_DisabledJob(t *testing.T) {
	svc, _ := makeService(t)
	mustStartService(t, svc)
	defer svc.Stop()

	mustAddJob(t, svc, CronJob{
		ID:       "disabled",
		Enabled:  false,
		Schedule: CronSchedule{Kind: KindEvery, EveryMS: 60000},
	})

	jobs, _ := svc.List()
	if len(jobs) != 1 {
		t.Errorf("expected 1 job, got %d", len(jobs))
	}
}

func TestRandID_Length(t *testing.T) {
	id := randID()
	if len(id) != 10 {
		t.Errorf("expected 10-char ID, got %d: %q", len(id), id)
	}
}

func TestRandID_Uniqueness(t *testing.T) {
	ids := map[string]bool{}
	for i := 0; i < 100; i++ {
		id := randID()
		if ids[id] {
			t.Errorf("duplicate id generated: %q", id)
		}
		ids[id] = true
	}
}

func TestLoad_FileNotExist(t *testing.T) {
	svc, _ := makeService(t)
	// load from non-existent path should return empty store
	st, err := svc.load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(st.Jobs) != 0 {
		t.Errorf("expected 0 jobs from non-existent file, got %d", len(st.Jobs))
	}
}

func TestLoad_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cron.json")
	if err := os.WriteFile(path, []byte("{invalid"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	svc := New(path, func(ctx context.Context, job CronJob) (RunResult, error) { return RunResult{}, nil })
	_, err := svc.load()
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestSave_And_Load(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cron.json")
	svc := New(path, func(ctx context.Context, job CronJob) (RunResult, error) { return RunResult{}, nil })

	st := Store{
		Version: 1,
		Jobs: []CronJob{
			{ID: "saved-job", Name: "saved", Enabled: true},
		},
	}
	if err := svc.save(st); err != nil {
		t.Fatalf("save: %v", err)
	}

	loaded, err := svc.load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(loaded.Jobs) != 1 {
		t.Errorf("expected 1 job, got %d", len(loaded.Jobs))
	}
	if loaded.Jobs[0].ID != "saved-job" {
		t.Errorf("expected ID 'saved-job', got %q", loaded.Jobs[0].ID)
	}
}

func TestRunNow_UpdatesLastRun(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cron.json")
	svc := New(path, func(ctx context.Context, job CronJob) (RunResult, error) { return RunResult{}, nil })
	mustStartService(t, svc)
	defer svc.Stop()

	before := time.Now().UnixMilli()
	mustAddJob(t, svc, CronJob{ID: "track-run", Enabled: true, Schedule: CronSchedule{Kind: KindEvery, EveryMS: 60000}})
	_ = mustRunNow(t, svc, "track-run", false)
	after := time.Now().UnixMilli()

	jobs, _ := svc.List()
	if len(jobs) == 0 {
		t.Fatal("expected 1 job")
	}
	if jobs[0].State.LastRunAtMS == nil {
		t.Fatal("expected LastRunAtMS to be set")
	}
	if *jobs[0].State.LastRunAtMS < before || *jobs[0].State.LastRunAtMS > after {
		t.Errorf("LastRunAtMS=%d out of range [%d,%d]", *jobs[0].State.LastRunAtMS, before, after)
	}
	if jobs[0].State.LastStatus != "ok" {
		t.Errorf("expected LastStatus='ok', got %q", jobs[0].State.LastStatus)
	}
}

func TestArmJob_KindAt_FutureTime(t *testing.T) {
	runCh := make(chan struct{}, 1)
	scheduledCh := make(chan time.Duration, 1)
	dir := t.TempDir()
	path := filepath.Join(dir, "cron.json")
	fixedNow := time.Unix(1_700_000_000, 0).UTC()
	svc := New(path, func(ctx context.Context, job CronJob) (RunResult, error) {
		runCh <- struct{}{}
		return RunResult{}, nil
	})
	svc.now = func() time.Time { return fixedNow }
	svc.after = func(delay time.Duration, fn func()) scheduledTimer {
		scheduledCh <- delay
		go fn()
		return stubScheduledTimer{}
	}
	mustStartService(t, svc)
	defer svc.Stop()

	atMS := fixedNow.Add(time.Second).UnixMilli()
	mustAddJob(t, svc, CronJob{
		ID:      "at-future",
		Enabled: true,
		Schedule: CronSchedule{
			Kind: KindAt,
			AtMS: atMS,
		},
	})

	select {
	case delay := <-scheduledCh:
		if delay <= 0 {
			t.Fatalf("expected positive schedule delay, got %s", delay)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timeout waiting for KindAt timer scheduling")
	}

	select {
	case <-runCh:
	case <-time.After(100 * time.Millisecond):
		t.Error("timeout waiting for KindAt job to run")
	}
}

func TestRemove_WithSchedulerEntry(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cron.json")
	svc := New(path, func(ctx context.Context, job CronJob) (RunResult, error) { return RunResult{}, nil })
	mustStartService(t, svc)
	defer svc.Stop()

	// Add a cron job (uses the scheduler)
	mustAddJob(t, svc, CronJob{
		ID:       "sched-job",
		Enabled:  true,
		Schedule: CronSchedule{Kind: KindCron, Expr: "0 * * * *"},
	})

	// Remove should also remove from scheduler entries
	found, err := svc.Remove("sched-job")
	if err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if !found {
		t.Error("expected found=true")
	}

	jobs, _ := svc.List()
	if len(jobs) != 0 {
		t.Errorf("expected 0 jobs after removal, got %d", len(jobs))
	}
}

func TestStart_WithExistingJobs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cron.json")

	// First, create a service and add jobs
	svc1 := New(path, func(ctx context.Context, job CronJob) (RunResult, error) { return RunResult{}, nil })
	mustStartService(t, svc1)
	mustAddJob(t, svc1, CronJob{
		ID:       "existing",
		Enabled:  true,
		Schedule: CronSchedule{Kind: KindEvery, EveryMS: 60000},
	})
	svc1.Stop()

	// Create a new service with same path - Start should load existing jobs
	svc2 := New(path, func(ctx context.Context, job CronJob) (RunResult, error) { return RunResult{}, nil })
	if err := svc2.Start(); err != nil {
		t.Fatalf("Start with existing jobs: %v", err)
	}
	defer svc2.Stop()

	jobs, _ := svc2.List()
	if len(jobs) != 1 {
		t.Errorf("expected 1 job loaded from file, got %d", len(jobs))
	}
}

func TestCronPayloadSessionKey(t *testing.T) {
	payload := CronPayload{
		Kind:       "agent_turn",
		Message:    "hello from cron",
		SessionKey: "custom-session-123",
		Channel:    "telegram",
		To:         "user456",
	}

	// Serialize
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	// Deserialize
	var decoded CronPayload
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if decoded.SessionKey != "custom-session-123" {
		t.Errorf("expected SessionKey %q, got %q", "custom-session-123", decoded.SessionKey)
	}
	if decoded.Kind != "agent_turn" {
		t.Errorf("expected Kind %q, got %q", "agent_turn", decoded.Kind)
	}
	if decoded.Message != "hello from cron" {
		t.Errorf("expected Message %q, got %q", "hello from cron", decoded.Message)
	}
}

func TestCronPayloadSessionKey_OmitEmpty(t *testing.T) {
	// SessionKey should be omitted when empty (json:"session_key,omitempty")
	payload := CronPayload{
		Kind:    "agent_turn",
		Message: "no session key",
	}
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if strings.Contains(string(data), "session_key") {
		t.Errorf("expected session_key to be omitted when empty, got: %s", string(data))
	}
}

func TestCronPayloadAgentCLIRun_JSONRoundTrip(t *testing.T) {
	payload := CronPayload{
		Kind:       PayloadAgentCLIRun,
		SessionKey: "cron:agents",
		AgentRun: &CronAgentRunPayload{
			RunnerID:       "codex",
			Task:           "review the repo",
			TimeoutSeconds: 600,
			Cwd:            "/workspace",
			Model:          "gpt-5",
			Mode:           "review",
			Isolation:      "host_readonly",
			MaxTurns:       4,
			Meta:           map[string]any{"source": "cron"},
		},
	}

	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var decoded CronPayload
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if decoded.Kind != PayloadAgentCLIRun {
		t.Fatalf("expected kind %q, got %q", PayloadAgentCLIRun, decoded.Kind)
	}
	if decoded.AgentRun == nil {
		t.Fatal("expected agent_run to round-trip")
	}
	if decoded.AgentRun.RunnerID != "codex" || decoded.AgentRun.Task != "review the repo" {
		t.Fatalf("unexpected agent run payload: %#v", decoded.AgentRun)
	}
}

func TestValidatePayload_AgentCLIRunDefaultsModeAndIsolation(t *testing.T) {
	payload := NormalizePayload(CronPayload{
		Kind: PayloadAgentCLIRun,
		AgentRun: &CronAgentRunPayload{
			RunnerID: "codex",
			Task:     "review",
		},
	})
	if err := ValidatePayload(payload); err != nil {
		t.Fatalf("ValidatePayload: %v", err)
	}
	if payload.AgentRun.Mode != DefaultAgentCLICronMode {
		t.Fatalf("expected default mode %q, got %q", DefaultAgentCLICronMode, payload.AgentRun.Mode)
	}
	if payload.AgentRun.Isolation != DefaultAgentCLICronIsolation {
		t.Fatalf("expected default isolation %q, got %q", DefaultAgentCLICronIsolation, payload.AgentRun.Isolation)
	}
}

func TestValidatePayload_AgentCLIRunRequiresRunnerAndTask(t *testing.T) {
	cases := []CronPayload{
		{Kind: PayloadAgentCLIRun},
		{Kind: PayloadAgentCLIRun, AgentRun: &CronAgentRunPayload{Task: "review"}},
		{Kind: PayloadAgentCLIRun, AgentRun: &CronAgentRunPayload{RunnerID: "codex"}},
	}
	for _, tc := range cases {
		if err := ValidatePayload(tc); err == nil {
			t.Fatalf("expected validation error for %#v", tc)
		}
	}
}

func TestRunNow_StoresEnqueuedRunIDs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cron.json")
	svc := New(path, func(ctx context.Context, job CronJob) (RunResult, error) {
		return RunResult{EnqueuedJobID: "job-agentcli-123", EnqueuedRunID: "acr_123"}, nil
	})
	mustStartService(t, svc)
	defer svc.Stop()

	mustAddJob(t, svc, CronJob{
		ID:       "agent-run",
		Enabled:  true,
		Schedule: CronSchedule{Kind: KindEvery, EveryMS: 60000},
		Payload: CronPayload{
			Kind: PayloadAgentCLIRun,
			AgentRun: &CronAgentRunPayload{
				RunnerID: "codex",
				Task:     "review",
			},
		},
	})
	_ = mustRunNow(t, svc, "agent-run", false)

	jobs, _ := svc.List()
	if jobs[0].State.LastEnqueuedJobID != "job-agentcli-123" {
		t.Fatalf("expected enqueued job id, got %#v", jobs[0].State)
	}
	if jobs[0].State.LastEnqueuedRunID != "acr_123" {
		t.Fatalf("expected enqueued run id, got %#v", jobs[0].State)
	}
}

func TestService_ConcurrentMutationAndLifecycle(t *testing.T) {
	var ran atomic.Int32
	dir := t.TempDir()
	path := filepath.Join(dir, "cron.json")
	svc := New(path, func(ctx context.Context, job CronJob) (RunResult, error) {
		ran.Add(1)
		return RunResult{}, nil
	})
	mustStartService(t, svc)
	defer svc.Stop()

	const runJobs = 8
	const removeJobs = 8
	const addJobs = 8
	const lifecycleCycles = 4

	for i := 0; i < runJobs; i++ {
		mustAddJob(t, svc, CronJob{
			ID:       fmt.Sprintf("run-%d", i),
			Enabled:  true,
			Schedule: CronSchedule{Kind: KindEvery, EveryMS: 60000},
		})
	}
	for i := 0; i < removeJobs; i++ {
		mustAddJob(t, svc, CronJob{
			ID:       fmt.Sprintf("remove-%d", i),
			Enabled:  true,
			Schedule: CronSchedule{Kind: KindEvery, EveryMS: 60000},
		})
	}

	errCh := make(chan error, runJobs+removeJobs+addJobs)
	var wg sync.WaitGroup
	for i := 0; i < runJobs; i++ {
		id := fmt.Sprintf("run-%d", i)
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			job, err := svc.RunNow(context.Background(), id, false)
			if err != nil {
				errCh <- fmt.Errorf("RunNow %s: %w", id, err)
				return
			}
			if job.ID != id {
				errCh <- fmt.Errorf("RunNow returned job %q, want %q", job.ID, id)
			}
		}(id)
	}
	for i := 0; i < removeJobs; i++ {
		id := fmt.Sprintf("remove-%d", i)
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			removed, err := svc.Remove(id)
			if err != nil {
				errCh <- fmt.Errorf("Remove %s: %w", id, err)
				return
			}
			if !removed {
				errCh <- fmt.Errorf("Remove %s returned false", id)
			}
		}(id)
	}
	for i := 0; i < addJobs; i++ {
		id := fmt.Sprintf("add-%d", i)
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			if err := svc.Add(CronJob{ID: id, Enabled: true, Schedule: CronSchedule{Kind: KindEvery, EveryMS: 60000}}); err != nil {
				errCh <- fmt.Errorf("Add %s: %w", id, err)
			}
		}(id)
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for cycle := 0; cycle < lifecycleCycles; cycle++ {
			svc.Stop()
			if err := svc.Start(); err != nil {
				errCh <- fmt.Errorf("Start cycle %d: %w", cycle, err)
				return
			}
		}
	}()
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			t.Error(err)
		}
	}

	if got := ran.Load(); got != runJobs {
		t.Fatalf("expected %d successful run invocations, got %d", runJobs, got)
	}

	jobs, err := svc.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	seen := make(map[string]struct{}, len(jobs))
	for _, job := range jobs {
		if _, ok := seen[job.ID]; ok {
			t.Fatalf("duplicate job id %q after concurrent operations", job.ID)
		}
		seen[job.ID] = struct{}{}
	}
	for i := 0; i < runJobs; i++ {
		id := fmt.Sprintf("run-%d", i)
		if _, ok := seen[id]; !ok {
			t.Fatalf("expected retained job %q after concurrent operations", id)
		}
	}
	for i := 0; i < addJobs; i++ {
		id := fmt.Sprintf("add-%d", i)
		if _, ok := seen[id]; !ok {
			t.Fatalf("expected added job %q after concurrent operations", id)
		}
	}
	for i := 0; i < removeJobs; i++ {
		if _, ok := seen[fmt.Sprintf("remove-%d", i)]; ok {
			t.Fatalf("removed job remove-%d still present", i)
		}
	}
}
