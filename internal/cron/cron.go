// Package cron stores and runs scheduled jobs backed by a JSON file.
package cron

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/robfig/cron/v3"
	_ "modernc.org/sqlite"
)

// ScheduleKind identifies how a job's next run time is computed.
type ScheduleKind string

const (
	// KindAt runs a job once at an absolute Unix-millisecond timestamp.
	KindAt ScheduleKind = "at"
	// KindEvery runs a job on a fixed interval.
	KindEvery ScheduleKind = "every"
	// KindCron runs a job using a cron expression.
	KindCron ScheduleKind = "cron"
)

const (
	// PayloadAgentTurn wakes the normal OR3 agent runtime.
	PayloadAgentTurn = "agent_turn"
	// PayloadSystemEvent is retained for compatibility with existing system-originated cron jobs.
	PayloadSystemEvent = "system_event"
	// PayloadAgentCLIRun enqueues an external agent CLI run.
	PayloadAgentCLIRun = "agent_cli_run"
)

const (
	DefaultAgentCLICronMode      = "review"
	DefaultAgentCLICronIsolation = "host_readonly"
)

// CronSchedule describes when a cron job should run.
type CronSchedule struct {
	Kind    ScheduleKind `json:"kind"`
	AtMS    int64        `json:"at_ms,omitempty"`
	EveryMS int64        `json:"every_ms,omitempty"`
	Expr    string       `json:"expr,omitempty"`
	TZ      string       `json:"tz,omitempty"`
}

// CronPayload is the user-visible work queued when a job fires.
type CronPayload struct {
	Kind       string               `json:"kind"` // "agent_turn"|"system_event"|"agent_cli_run"
	Message    string               `json:"message"`
	Deliver    bool                 `json:"deliver"`
	Channel    string               `json:"channel,omitempty"`
	To         string               `json:"to,omitempty"`
	SessionKey string               `json:"session_key,omitempty"` // optional per-job session key override
	AgentRun   *CronAgentRunPayload `json:"agent_run,omitempty"`
}

// CronAgentRunPayload describes an external agent CLI run started by cron.
type CronAgentRunPayload struct {
	RunnerID       string         `json:"runner_id"`
	Task           string         `json:"task"`
	TimeoutSeconds int            `json:"timeout_seconds,omitempty"`
	Cwd            string         `json:"cwd,omitempty"`
	Model          string         `json:"model,omitempty"`
	Mode           string         `json:"mode,omitempty"`
	Isolation      string         `json:"isolation,omitempty"`
	MaxTurns       int            `json:"max_turns,omitempty"`
	Meta           map[string]any `json:"meta,omitempty"`
}

// CronJobState records the latest execution result for a job.
type CronJobState struct {
	NextRunAtMS       *int64 `json:"next_run_at_ms,omitempty"`
	LastRunAtMS       *int64 `json:"last_run_at_ms,omitempty"`
	LastStatus        string `json:"last_status,omitempty"` // ok|error|skipped
	LastError         string `json:"last_error,omitempty"`
	LastEnqueuedJobID string `json:"last_enqueued_job_id,omitempty"`
	LastEnqueuedRunID string `json:"last_enqueued_run_id,omitempty"`
}

// CronJob is a persisted scheduled job definition.
type CronJob struct {
	ID             string       `json:"id"`
	Name           string       `json:"name"`
	Enabled        bool         `json:"enabled"`
	Schedule       CronSchedule `json:"schedule"`
	Payload        CronPayload  `json:"payload"`
	State          CronJobState `json:"state"`
	CreatedAtMS    int64        `json:"created_at_ms"`
	UpdatedAtMS    int64        `json:"updated_at_ms"`
	DeleteAfterRun bool         `json:"delete_after_run"`
}

// Store is the on-disk JSON document that holds scheduled jobs.
type Store struct {
	Version int       `json:"version"`
	Jobs    []CronJob `json:"jobs"`
}

// ErrNotFound reports that a requested cron job does not exist.
var ErrNotFound = errors.New("cron job not found")

// RunResult records side effects produced by a cron run.
type RunResult struct {
	EnqueuedJobID string
	EnqueuedRunID string
}

// Runner executes a single cron job when it becomes due.
type Runner func(ctx context.Context, job CronJob) (RunResult, error)

type scheduledTimer interface {
	Stop() bool
}

// Service loads, schedules, and persists cron jobs. It is safe for concurrent use.
type Service struct {
	mu      sync.RWMutex
	path    string
	runner  Runner
	c       *cron.Cron
	entries map[string]cron.EntryID
	timers  map[string]scheduledTimer
	ctx     context.Context
	cancel  context.CancelFunc
	now     func() time.Time
	after   func(time.Duration, func()) scheduledTimer
}

// New constructs a Service backed by path and runner.
func New(path string, runner Runner) *Service {
	if runner == nil {
		panic("cron runner not configured")
	}
	return &Service{
		path:    path,
		runner:  runner,
		entries: map[string]cron.EntryID{},
		timers:  map[string]scheduledTimer{},
		now:     time.Now,
		after: func(delay time.Duration, fn func()) scheduledTimer {
			return time.AfterFunc(delay, fn)
		},
	}
}

func (s *Service) nowTime() time.Time {
	if s != nil && s.now != nil {
		return s.now()
	}
	return time.Now()
}

func (s *Service) afterFunc(delay time.Duration, fn func()) scheduledTimer {
	if s != nil && s.after != nil {
		return s.after(delay, fn)
	}
	return time.AfterFunc(delay, fn)
}

func (s *Service) load() (Store, error) {
	var st Store
	st.Version = 1
	if strings.HasSuffix(s.path, ".db") || strings.HasSuffix(s.path, ".sqlite") || strings.HasSuffix(s.path, ".sqlite3") {
		return s.loadSQLite()
	}
	b, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return st, nil
		}
		return st, err
	}
	if err := json.Unmarshal(b, &st); err != nil {
		return st, err
	}
	return st, nil
}

func (s *Service) save(st Store) error {
	if strings.HasSuffix(s.path, ".db") || strings.HasSuffix(s.path, ".sqlite") || strings.HasSuffix(s.path, ".sqlite3") {
		return s.saveSQLite(st)
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	tmpPath := s.path + ".tmp"
	if err := os.WriteFile(tmpPath, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmpPath, s.path)
}

func (s *Service) openSQLite() (*sql.DB, error) {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", s.path)
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS cron_jobs (
		id TEXT PRIMARY KEY,
		job_json TEXT NOT NULL,
		updated_at_ms INTEGER NOT NULL
	)`); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func (s *Service) loadSQLite() (Store, error) {
	st := Store{Version: 1}
	db, err := s.openSQLite()
	if err != nil {
		return st, err
	}
	defer db.Close()
	rows, err := db.Query(`SELECT job_json FROM cron_jobs ORDER BY updated_at_ms, id`)
	if err != nil {
		return st, err
	}
	defer rows.Close()
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return st, err
		}
		var job CronJob
		if err := json.Unmarshal([]byte(raw), &job); err != nil {
			return st, err
		}
		st.Jobs = append(st.Jobs, job)
	}
	return st, rows.Err()
}

func (s *Service) saveSQLite(st Store) error {
	db, err := s.openSQLite()
	if err != nil {
		return err
	}
	defer db.Close()
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	seen := map[string]struct{}{}
	for _, job := range st.Jobs {
		raw, err := json.Marshal(job)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT INTO cron_jobs(id, job_json, updated_at_ms) VALUES (?, ?, ?)
			ON CONFLICT(id) DO UPDATE SET job_json = excluded.job_json, updated_at_ms = excluded.updated_at_ms`,
			job.ID, string(raw), job.UpdatedAtMS); err != nil {
			return err
		}
		seen[job.ID] = struct{}{}
	}
	rows, err := tx.Query(`SELECT id FROM cron_jobs`)
	if err != nil {
		return err
	}
	var stale []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		if _, ok := seen[id]; !ok {
			stale = append(stale, id)
		}
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, id := range stale {
		if _, err := tx.Exec(`DELETE FROM cron_jobs WHERE id = ?`, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// Start loads persisted jobs and arms the scheduler.
func (s *Service) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.c != nil {
		return nil
	}

	s.ctx, s.cancel = context.WithCancel(context.Background())

	s.c = cron.New(cron.WithSeconds(), cron.WithParser(cron.NewParser(cron.SecondOptional|cron.Minute|cron.Hour|cron.Dom|cron.Month|cron.Dow|cron.Descriptor)))
	st, err := s.load()
	if err != nil {
		return err
	}
	for i := range st.Jobs {
		st.Jobs[i] = s.prepareJobLocked(st.Jobs[i])
		s.armJobLocked(st.Jobs[i])
	}
	if err := s.save(st); err != nil {
		return err
	}
	s.c.Start()
	return nil
}

// Stop halts the scheduler and waits for the cron runner to stop.
func (s *Service) Stop() {
	s.mu.Lock()
	c := s.c
	timers := s.timers
	s.c = nil
	s.entries = map[string]cron.EntryID{}
	s.timers = map[string]scheduledTimer{}
	cancel := s.cancel
	s.cancel = nil
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}

	if c != nil {
		ctx := c.Stop()
		<-ctx.Done()
	}
	for _, timer := range timers {
		timer.Stop()
	}
}

// Status reports the number of jobs and the earliest known next wake time.
func (s *Service) Status() (map[string]any, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	st, err := s.load()
	if err != nil {
		return nil, err
	}
	next := int64(0)
	for _, j := range st.Jobs {
		if j.State.NextRunAtMS != nil {
			if next == 0 || *j.State.NextRunAtMS < next {
				next = *j.State.NextRunAtMS
			}
		}
	}
	var nextPtr *int64
	if next != 0 {
		nextPtr = &next
	}
	return map[string]any{"jobs": len(st.Jobs), "next_wake_at_ms": nextPtr}, nil
}

// List returns the persisted jobs in storage order.
func (s *Service) List() ([]CronJob, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	st, err := s.load()
	if err != nil {
		return nil, err
	}
	return st.Jobs, nil
}

// Add assigns missing identifiers, persists job, and arms it when possible.
func (s *Service) Add(job CronJob) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, err := s.load()
	if err != nil {
		return err
	}
	const maxJobs = 10000
	if len(st.Jobs) >= maxJobs {
		return fmt.Errorf("max job count reached (%d)", maxJobs)
	}
	job.Payload = NormalizePayload(job.Payload)
	if err := ValidateSchedule(job.Schedule); err != nil {
		return err
	}
	if err := ValidatePayload(job.Payload); err != nil {
		return err
	}
	now := time.Now().UnixMilli()
	job.CreatedAtMS = now
	job.UpdatedAtMS = now
	if job.ID == "" {
		job.ID = randID()
	}
	if job.Name == "" {
		job.Name = job.ID
	}
	for _, j := range st.Jobs {
		if j.ID == job.ID {
			return fmt.Errorf("job with id %s already exists", job.ID)
		}
	}
	job = s.prepareJobLocked(job)
	st.Jobs = append(st.Jobs, job)
	if err := s.save(st); err != nil {
		return err
	}
	s.armJobLocked(job)
	return nil
}

// Update replaces an existing job while preserving its ID and creation time.
func (s *Service) Update(id string, job CronJob) (CronJob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id = strings.TrimSpace(id)
	if id == "" {
		return CronJob{}, fmt.Errorf("job id is required")
	}
	if err := ValidateSchedule(job.Schedule); err != nil {
		return CronJob{}, err
	}
	job.Payload = NormalizePayload(job.Payload)
	if err := ValidatePayload(job.Payload); err != nil {
		return CronJob{}, err
	}
	st, err := s.load()
	if err != nil {
		return CronJob{}, err
	}
	for i := range st.Jobs {
		if st.Jobs[i].ID != id {
			continue
		}
		current := st.Jobs[i]
		job.ID = id
		job.CreatedAtMS = current.CreatedAtMS
		job.UpdatedAtMS = time.Now().UnixMilli()
		if job.Name == "" {
			job.Name = id
		}
		job = s.prepareJobLocked(job)
		s.unarmJobLocked(id)
		st.Jobs[i] = job
		if err := s.save(st); err != nil {
			return current, err
		}
		s.armJobLocked(job)
		return job, nil
	}
	return CronJob{}, ErrNotFound
}

// SetEnabled toggles a job and updates scheduler state.
func (s *Service) SetEnabled(id string, enabled bool) (CronJob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id = strings.TrimSpace(id)
	if id == "" {
		return CronJob{}, fmt.Errorf("job id is required")
	}
	st, err := s.load()
	if err != nil {
		return CronJob{}, err
	}
	for i := range st.Jobs {
		if st.Jobs[i].ID != id {
			continue
		}
		if enabled {
			if err := ValidateSchedule(st.Jobs[i].Schedule); err != nil {
				return st.Jobs[i], err
			}
			st.Jobs[i].Payload = NormalizePayload(st.Jobs[i].Payload)
			if err := ValidatePayload(st.Jobs[i].Payload); err != nil {
				return st.Jobs[i], err
			}
		}
		s.unarmJobLocked(id)
		st.Jobs[i].Enabled = enabled
		st.Jobs[i].UpdatedAtMS = time.Now().UnixMilli()
		st.Jobs[i] = s.prepareJobLocked(st.Jobs[i])
		if err := s.save(st); err != nil {
			return st.Jobs[i], err
		}
		s.armJobLocked(st.Jobs[i])
		return st.Jobs[i], nil
	}
	return CronJob{}, ErrNotFound
}

// Remove deletes the job with id and reports whether one was found.
func (s *Service) Remove(id string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.removeLocked(id)
}

func (s *Service) removeLocked(id string) (bool, error) {
	st, err := s.load()
	if err != nil {
		return false, err
	}
	found := false
	for _, j := range st.Jobs {
		if j.ID == id {
			found = true
			break
		}
	}
	if !found {
		return false, nil
	}
	n := make([]CronJob, 0, len(st.Jobs))
	for _, j := range st.Jobs {
		if j.ID == id {
			s.unarmJobLocked(id)
			continue
		}
		n = append(n, j)
	}
	st.Jobs = n
	if err := s.save(st); err != nil {
		return false, err
	}
	return true, nil
}

// RunNow runs the job with id immediately.
//
// When force is false, disabled jobs are skipped and reported as not run.
func (s *Service) RunNow(ctx context.Context, id string, force bool) (CronJob, error) {
	return s.runJobByID(ctx, id, force)
}

func (s *Service) runJobByID(ctx context.Context, id string, force bool) (CronJob, error) {
	s.mu.Lock()
	st, err := s.load()
	if err != nil {
		s.mu.Unlock()
		return CronJob{}, err
	}
	var jobToRun CronJob
	found := false
	for i := range st.Jobs {
		if st.Jobs[i].ID == id {
			jobToRun = st.Jobs[i]
			found = true
			break
		}
	}
	if !found {
		s.mu.Unlock()
		return CronJob{}, ErrNotFound
	}
	if !force && !jobToRun.Enabled {
		s.mu.Unlock()
		return jobToRun, nil
	}
	s.mu.Unlock()

	result, err := s.runner(ctx, jobToRun)

	s.mu.Lock()
	defer s.mu.Unlock()
	latest, loadErr := s.load()
	if loadErr != nil {
		if err != nil {
			return jobToRun, errors.Join(err, loadErr)
		}
		return jobToRun, loadErr
	}
	jobIndex := -1
	for i := range latest.Jobs {
		if latest.Jobs[i].ID == id {
			jobIndex = i
			break
		}
	}
	if jobIndex == -1 {
		return jobToRun, err
	}
	jobToUpdate := latest.Jobs[jobIndex]
	now := s.nowTime().UnixMilli()
	jobToUpdate.State.LastRunAtMS = &now
	if err != nil {
		jobToUpdate.State.LastStatus = "error"
		jobToUpdate.State.LastError = err.Error()
	} else {
		jobToUpdate.State.LastStatus = "ok"
		jobToUpdate.State.LastError = ""
	}
	jobToUpdate.State.LastEnqueuedJobID = result.EnqueuedJobID
	jobToUpdate.State.LastEnqueuedRunID = result.EnqueuedRunID
	if jobToUpdate.DeleteAfterRun {
		s.unarmJobLocked(id)
		n := make([]CronJob, 0, len(latest.Jobs))
		for _, jj := range latest.Jobs {
			if jj.ID == id {
				continue
			}
			n = append(n, jj)
		}
		latest.Jobs = n
	} else {
		latest.Jobs[jobIndex] = s.prepareJobLocked(jobToUpdate)
	}
	if saveErr := s.save(latest); saveErr != nil {
		log.Printf("cron save failed: %v", saveErr)
	}
	return jobToRun, err
}

func (s *Service) unarmJobLocked(id string) {
	if eid, ok := s.entries[id]; ok && s.c != nil {
		s.c.Remove(eid)
		delete(s.entries, id)
	}
	if timer, ok := s.timers[id]; ok {
		timer.Stop()
		delete(s.timers, id)
	}
}

func (s *Service) prepareJobLocked(job CronJob) CronJob {
	job.Payload = NormalizePayload(job.Payload)
	next := nextRunMS(job.Schedule, s.nowTime())
	job.State.NextRunAtMS = next
	if !job.Enabled {
		job.State.NextRunAtMS = nil
	}
	return job
}

func (s *Service) armJobLocked(job CronJob) {
	if s.c == nil {
		return
	}
	s.unarmJobLocked(job.ID)
	if !job.Enabled {
		return
	}
	if err := ValidateSchedule(job.Schedule); err != nil {
		log.Printf("cron schedule invalid: id=%s err=%v", job.ID, err)
		return
	}
	if err := ValidatePayload(job.Payload); err != nil {
		log.Printf("cron payload invalid: id=%s err=%v", job.ID, err)
		return
	}
	switch job.Schedule.Kind {
	case KindAt:
		at := time.UnixMilli(job.Schedule.AtMS)
		dur := at.Sub(s.nowTime())
		if dur <= 0 {
			return
		}
		id := job.ID
		s.timers[id] = s.afterFunc(dur, func() {
			if _, err := s.runJobByID(s.ctx, id, false); err != nil && !errors.Is(err, ErrNotFound) {
				log.Printf("cron runner error: id=%s err=%v", id, err)
			}
		})
	case KindEvery:
		sec := int64(job.Schedule.EveryMS / 1000)
		if sec <= 0 {
			log.Printf("cron schedule invalid: id=%s every_ms=%d must be >= 1000", job.ID, job.Schedule.EveryMS)
			return
		}
		spec := "@every " + (time.Duration(sec) * time.Second).String()
		id := job.ID
		eid, err := s.c.AddFunc(spec, func() {
			if _, e := s.runJobByID(s.ctx, id, false); e != nil && !errors.Is(e, ErrNotFound) {
				log.Printf("cron runner error: id=%s err=%v", id, e)
			}
		})
		if err == nil {
			s.entries[id] = eid
		} else {
			log.Printf("cron schedule add failed: id=%s spec=%s err=%v", id, spec, err)
		}
	case KindCron:
		spec := job.Schedule.Expr
		id := job.ID
		if tz := strings.TrimSpace(job.Schedule.TZ); tz != "" {
			if _, err := time.LoadLocation(tz); err == nil {
				spec = "CRON_TZ=" + tz + " " + spec
			}
		}
		eid, err := s.c.AddFunc(spec, func() {
			if _, e := s.runJobByID(s.ctx, id, false); e != nil && !errors.Is(e, ErrNotFound) {
				log.Printf("cron runner error: id=%s err=%v", id, e)
			}
		})
		if err == nil {
			s.entries[id] = eid
		} else {
			log.Printf("cron schedule add failed: id=%s spec=%s err=%v", id, spec, err)
		}
	}
}

// NormalizePayload fills compatibility defaults for persisted and incoming jobs.
func NormalizePayload(payload CronPayload) CronPayload {
	payload.Kind = strings.TrimSpace(payload.Kind)
	if payload.Kind == "" {
		payload.Kind = PayloadAgentTurn
	}
	payload.SessionKey = strings.TrimSpace(payload.SessionKey)
	payload.Channel = strings.TrimSpace(payload.Channel)
	payload.To = strings.TrimSpace(payload.To)
	if payload.AgentRun != nil {
		run := *payload.AgentRun
		run.RunnerID = strings.TrimSpace(run.RunnerID)
		run.Task = strings.TrimSpace(run.Task)
		run.Cwd = strings.TrimSpace(run.Cwd)
		run.Model = strings.TrimSpace(run.Model)
		run.Mode = strings.TrimSpace(run.Mode)
		run.Isolation = strings.TrimSpace(run.Isolation)
		if payload.Kind == PayloadAgentCLIRun {
			if run.Mode == "" {
				run.Mode = DefaultAgentCLICronMode
			}
			if run.Isolation == "" {
				run.Isolation = DefaultAgentCLICronIsolation
			}
		}

		payload.AgentRun = &run
	}
	return payload
}

// ValidatePayload checks whether a payload can be dispatched by the cron runner.
func ValidatePayload(payload CronPayload) error {
	payload = NormalizePayload(payload)
	switch payload.Kind {
	case PayloadAgentTurn, PayloadSystemEvent:
		return nil
	case PayloadAgentCLIRun:
		if payload.AgentRun == nil {
			return fmt.Errorf("agent_run is required for agent_cli_run payloads")
		}
		if strings.TrimSpace(payload.AgentRun.RunnerID) == "" {
			return fmt.Errorf("agent_run.runner_id is required")
		}
		if strings.TrimSpace(payload.AgentRun.Task) == "" {
			return fmt.Errorf("agent_run.task is required")
		}
		return nil
	default:
		return fmt.Errorf("unsupported payload kind: %s", payload.Kind)
	}
}

// ValidateSchedule checks whether a schedule can be executed by the scheduler.
func ValidateSchedule(schedule CronSchedule) error {
	switch schedule.Kind {
	case KindAt:
		if schedule.AtMS <= 0 {
			return fmt.Errorf("at_ms is required for at schedules")
		}
	case KindEvery:
		if schedule.EveryMS < 1000 {
			return fmt.Errorf("every_ms must be at least 1000 (1 second)")
		}
	case KindCron:
		if strings.TrimSpace(schedule.Expr) == "" {
			return fmt.Errorf("expr is required for cron schedules")
		}
		if _, err := cronParser().Parse(schedule.Expr); err != nil {
			return fmt.Errorf("invalid cron expression: %w", err)
		}
	default:
		return fmt.Errorf("unsupported schedule kind: %s", schedule.Kind)
	}
	if strings.TrimSpace(schedule.TZ) != "" {
		if _, err := time.LoadLocation(strings.TrimSpace(schedule.TZ)); err != nil {
			return fmt.Errorf("invalid timezone: %w", err)
		}
	}
	return nil
}

func nextRunMS(schedule CronSchedule, now time.Time) *int64 {
	switch schedule.Kind {
	case KindAt:
		if schedule.AtMS <= now.UnixMilli() {
			return nil
		}
		next := schedule.AtMS
		return &next
	case KindEvery:
		everyMS := schedule.EveryMS
		if everyMS <= 0 {
			everyMS = int64(time.Minute / time.Millisecond)
		}
		next := now.Add(time.Duration(everyMS) * time.Millisecond).UnixMilli()
		return &next
	case KindCron:
		parser := cronParser()
		scheduleSpec, err := parser.Parse(schedule.Expr)
		if err != nil {
			return nil
		}
		if strings.TrimSpace(schedule.TZ) != "" {
			if loc, err := time.LoadLocation(strings.TrimSpace(schedule.TZ)); err == nil {
				now = now.In(loc)
			}
		}
		next := scheduleSpec.Next(now).UnixMilli()
		if next <= 0 {
			return nil
		}
		return &next
	default:
		return nil
	}
}

var cronParserInstance = cron.NewParser(cron.SecondOptional | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)

func cronParser() cron.Parser {
	return cronParserInstance
}

func randUint() (uint64, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return 0, err
	}
	return binary.BigEndian.Uint64(b[:]), nil
}

func mustRandUint() uint64 {
	v, err := randUint()
	if err != nil {
		panic("crypto/rand failed: " + err.Error())
	}
	return v
}

func randID() string {
	const chars = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, 10)
	for i := range b {
		b[i] = chars[int(mustRandUint()%uint64(len(chars)))]
	}
	return string(b)
}
