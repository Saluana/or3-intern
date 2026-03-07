package cron

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func makeService(t *testing.T) (*Service, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "cron.json")
	svc := New(path, func(ctx context.Context, job CronJob) error {
		return nil
	})
	return svc, path
}

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
	svc.Start()
	defer svc.Stop()

	job := CronJob{
		Name:    "test job",
		Enabled: true,
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
	svc.Start()
	defer svc.Stop()

	job := CronJob{Name: "no-id", Enabled: true, Schedule: CronSchedule{Kind: KindEvery, EveryMS: 60000}}
	svc.Add(job)

	jobs, _ := svc.List()
	if jobs[0].ID == "" {
		t.Error("expected auto-generated ID")
	}
}

func TestAdd_UsesNameAsIDIfMissing(t *testing.T) {
	svc, _ := makeService(t)
	svc.Start()
	defer svc.Stop()

	job := CronJob{Enabled: true, Schedule: CronSchedule{Kind: KindEvery, EveryMS: 60000}}
	svc.Add(job)

	jobs, _ := svc.List()
	// ID should match Name when no Name is given either (both auto-generated)
	if jobs[0].Name == "" {
		t.Error("expected name to default to id")
	}
}

func TestAdd_SetsTimestamps(t *testing.T) {
	svc, _ := makeService(t)
	svc.Start()
	defer svc.Stop()

	before := time.Now().UnixMilli()
	svc.Add(CronJob{Enabled: true, Schedule: CronSchedule{Kind: KindEvery, EveryMS: 60000}})
	after := time.Now().UnixMilli()

	jobs, _ := svc.List()
	if jobs[0].CreatedAtMS < before || jobs[0].CreatedAtMS > after {
		t.Errorf("CreatedAtMS out of range: got %d, expected [%d, %d]", jobs[0].CreatedAtMS, before, after)
	}
}

func TestRemove_Found(t *testing.T) {
	svc, _ := makeService(t)
	svc.Start()
	defer svc.Stop()

	svc.Add(CronJob{ID: "job1", Enabled: true, Schedule: CronSchedule{Kind: KindEvery, EveryMS: 60000}})

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
	svc.Start()
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
	svc := New(path, func(ctx context.Context, job CronJob) error {
		ran = true
		return nil
	})
	svc.Start()
	defer svc.Stop()

	svc.Add(CronJob{
		ID:       "runme",
		Enabled:  true,
		Schedule: CronSchedule{Kind: KindEvery, EveryMS: 60000},
	})

	found, err := svc.RunNow(context.Background(), "runme", false)
	if err != nil {
		t.Fatalf("RunNow: %v", err)
	}
	if !found {
		t.Error("expected found=true")
	}
	if !ran {
		t.Error("expected runner to be called")
	}
}

func TestRunNow_NotFound(t *testing.T) {
	svc, _ := makeService(t)
	svc.Start()
	defer svc.Stop()

	found, err := svc.RunNow(context.Background(), "missing", false)
	if err != nil {
		t.Fatalf("RunNow: %v", err)
	}
	if found {
		t.Error("expected found=false for missing job")
	}
}

func TestRunNow_Disabled_NoForce(t *testing.T) {
	ran := false
	dir := t.TempDir()
	path := filepath.Join(dir, "cron.json")
	svc := New(path, func(ctx context.Context, job CronJob) error {
		ran = true
		return nil
	})
	svc.Start()
	defer svc.Stop()

	svc.Add(CronJob{
		ID:       "disabled",
		Enabled:  false,
		Schedule: CronSchedule{Kind: KindEvery, EveryMS: 60000},
	})

	found, _ := svc.RunNow(context.Background(), "disabled", false)
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
	svc := New(path, func(ctx context.Context, job CronJob) error {
		ran = true
		return nil
	})
	svc.Start()
	defer svc.Stop()

	svc.Add(CronJob{
		ID:       "force-run",
		Enabled:  false,
		Schedule: CronSchedule{Kind: KindEvery, EveryMS: 60000},
	})

	found, err := svc.RunNow(context.Background(), "force-run", true)
	if err != nil {
		t.Fatalf("RunNow: %v", err)
	}
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
	svc := New(path, func(ctx context.Context, job CronJob) error {
		return nil
	})
	svc.Start()
	defer svc.Stop()

	svc.Add(CronJob{
		ID:             "delete-after",
		Enabled:        true,
		DeleteAfterRun: true,
		Schedule:       CronSchedule{Kind: KindEvery, EveryMS: 60000},
	})

	svc.RunNow(context.Background(), "delete-after", false)

	jobs, _ := svc.List()
	for _, j := range jobs {
		if j.ID == "delete-after" {
			t.Error("expected job to be deleted after run")
		}
	}
}

func TestStatus_NoJobs(t *testing.T) {
	svc, _ := makeService(t)
	svc.Start()
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
	svc.Start()
	defer svc.Stop()

	svc.Add(CronJob{
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
	svc.Start()
	defer svc.Stop()

	// Add a job with a known next_run_at_ms
	next := time.Now().Add(time.Hour).UnixMilli()
	svc.Add(CronJob{
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
	svc.Start()
	defer svc.Stop()

	// at time in the past - should not schedule (no panic)
	svc.Add(CronJob{
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
	svc.Start()
	defer svc.Stop()

	// Zero EveryMS should default to 60s
	svc.Add(CronJob{
		ID:      "every-zero",
		Enabled: true,
		Schedule: CronSchedule{
			Kind:    KindEvery,
			EveryMS: 0,
		},
	})

	jobs, _ := svc.List()
	if len(jobs) != 1 {
		t.Errorf("expected 1 job, got %d", len(jobs))
	}
}

func TestArmJob_KindCron_ValidExpr(t *testing.T) {
	svc, _ := makeService(t)
	svc.Start()
	defer svc.Stop()

	svc.Add(CronJob{
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
	svc.Start()
	defer svc.Stop()

	// Invalid cron expr - should log but not panic
	svc.Add(CronJob{
		ID:      "bad-expr",
		Enabled: true,
		Schedule: CronSchedule{
			Kind: KindCron,
			Expr: "not a valid cron expression at all",
		},
	})

	jobs, _ := svc.List()
	if len(jobs) != 1 {
		t.Errorf("expected 1 job (still added, just not scheduled), got %d", len(jobs))
	}
}

func TestArmJob_DisabledJob(t *testing.T) {
	svc, _ := makeService(t)
	svc.Start()
	defer svc.Stop()

	svc.Add(CronJob{
		ID:      "disabled",
		Enabled: false,
		Schedule: CronSchedule{Kind: KindEvery, EveryMS: 60000},
	})

	jobs, _ := svc.List()
	if len(jobs) != 1 {
		t.Errorf("expected 1 job, got %d", len(jobs))
	}
}

func TestFilepathDir(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"/home/user/config.json", "/home/user"},
		{"config.json", "."},
		{"/config.json", "."},
		{"a/b/c/d.json", "a/b/c"},
	}
	for _, tc := range tests {
		got := filepathDir(tc.input)
		if got != tc.want {
			t.Errorf("filepathDir(%q) = %q, want %q", tc.input, got, tc.want)
		}
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
	os.WriteFile(path, []byte("{invalid"), 0o644)

	svc := New(path, func(ctx context.Context, job CronJob) error { return nil })
	_, err := svc.load()
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestSave_And_Load(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cron.json")
	svc := New(path, func(ctx context.Context, job CronJob) error { return nil })

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
	svc := New(path, func(ctx context.Context, job CronJob) error { return nil })
	svc.Start()
	defer svc.Stop()

	before := time.Now().UnixMilli()
	svc.Add(CronJob{ID: "track-run", Enabled: true, Schedule: CronSchedule{Kind: KindEvery, EveryMS: 60000}})
	svc.RunNow(context.Background(), "track-run", false)
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
dir := t.TempDir()
path := filepath.Join(dir, "cron.json")
svc := New(path, func(ctx context.Context, job CronJob) error {
runCh <- struct{}{}
return nil
})
svc.Start()
defer svc.Stop()

// Schedule to run very soon
atMS := time.Now().Add(100 * time.Millisecond).UnixMilli()
svc.Add(CronJob{
ID:      "at-future",
Enabled: true,
Schedule: CronSchedule{
Kind: KindAt,
AtMS: atMS,
},
})

// Wait for it to run
select {
case <-runCh:
// success
case <-time.After(2 * time.Second):
t.Error("timeout waiting for KindAt job to run")
}
}

func TestRemove_WithSchedulerEntry(t *testing.T) {
dir := t.TempDir()
path := filepath.Join(dir, "cron.json")
svc := New(path, func(ctx context.Context, job CronJob) error { return nil })
svc.Start()
defer svc.Stop()

// Add a cron job (uses the scheduler)
svc.Add(CronJob{
ID:      "sched-job",
Enabled: true,
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
svc1 := New(path, func(ctx context.Context, job CronJob) error { return nil })
svc1.Start()
svc1.Add(CronJob{
ID:      "existing",
Enabled: true,
Schedule: CronSchedule{Kind: KindEvery, EveryMS: 60000},
})
svc1.Stop()

// Create a new service with same path - Start should load existing jobs
svc2 := New(path, func(ctx context.Context, job CronJob) error { return nil })
if err := svc2.Start(); err != nil {
t.Fatalf("Start with existing jobs: %v", err)
}
defer svc2.Stop()

jobs, _ := svc2.List()
if len(jobs) != 1 {
t.Errorf("expected 1 job loaded from file, got %d", len(jobs))
}
}

func TestRunNow_SaveError(t *testing.T) {
dir := t.TempDir()
path := filepath.Join(dir, "cron.json")
svc := New(path, func(ctx context.Context, job CronJob) error { return nil })
svc.Start()
defer svc.Stop()

svc.Add(CronJob{
ID:      "save-err",
Enabled: true,
Schedule: CronSchedule{Kind: KindEvery, EveryMS: 60000},
})

// Run successfully
found, err := svc.RunNow(context.Background(), "save-err", false)
if err != nil {
t.Fatalf("RunNow: %v", err)
}
if !found {
t.Error("expected found=true")
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

func TestCronRunnerPerJobSession(t *testing.T) {
svc, _ := makeService(t)
if err := svc.Start(); err != nil {
t.Fatalf("Start: %v", err)
}
defer svc.Stop()

// Track which session key the runner sees
var capturedSessionKey string
var runnerCalled bool
svc2 := &Service{
path: svc.path,
runner: func(ctx context.Context, job CronJob) error {
capturedSessionKey = job.Payload.SessionKey
runnerCalled = true
return nil
},
}

// Simulate a job with per-job SessionKey
job := CronJob{
ID:      "per-job-session",
Name:    "Per Job Session Test",
Enabled: true,
Schedule: CronSchedule{Kind: KindEvery, EveryMS: 60000},
Payload: CronPayload{
Kind:       "agent_turn",
Message:    "per-job message",
SessionKey: "per-job-session-key",
},
}

// Directly call the runner with the job
if err := svc2.runner(context.Background(), job); err != nil {
t.Fatalf("runner: %v", err)
}

if !runnerCalled {
t.Fatal("expected runner to be called")
}
if capturedSessionKey != "per-job-session-key" {
t.Errorf("expected SessionKey %q, got %q", "per-job-session-key", capturedSessionKey)
}
}
