// Package cron stores and runs scheduled jobs backed by a JSON file.
package cron

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/robfig/cron/v3"
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

// RunResult records side effects produced by a cron run.
type RunResult struct {
	EnqueuedJobID string
	EnqueuedRunID string
}

// Runner executes a single cron job when it becomes due.
type Runner func(ctx context.Context, job CronJob) (RunResult, error)

// Service loads, schedules, and persists cron jobs. It is safe for concurrent use.
type Service struct {
	mu      sync.Mutex
	path    string
	runner  Runner
	c       *cron.Cron
	entries map[string]cron.EntryID
	timers  map[string]*time.Timer
}

// New constructs a Service backed by path and runner.
func New(path string, runner Runner) *Service {
	return &Service{
		path:    path,
		runner:  runner,
		entries: map[string]cron.EntryID{},
		timers:  map[string]*time.Timer{},
	}
}

func (s *Service) load() (Store, error) {
	var st Store
	st.Version = 1
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
	if err := os.MkdirAll(filepathDir(s.path), 0o755); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(st, "", "  ")
	return os.WriteFile(s.path, b, 0o644)
}

func filepathDir(p string) string {
	i := len(p) - 1
	for i >= 0 && p[i] != '/' && p[i] != '\\' {
		i--
	}
	if i <= 0 {
		return "."
	}
	return p[:i]
}

// Start loads persisted jobs and arms the scheduler.
func (s *Service) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.c != nil {
		return nil
	}

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
		log.Printf("cron save failed: %v", err)
	}
	s.c.Start()
	return nil
}

// Stop halts the scheduler and waits for the cron runner to stop.
func (s *Service) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.c != nil {
		ctx := s.c.Stop()
		<-ctx.Done()
		s.c = nil
		s.entries = map[string]cron.EntryID{}
	}
	for id, timer := range s.timers {
		timer.Stop()
		delete(s.timers, id)
	}
}

// Status reports the number of jobs and the earliest known next wake time.
func (s *Service) Status() (map[string]any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
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
	s.mu.Lock()
	defer s.mu.Unlock()
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
	job = s.prepareJobLocked(job)
	st.Jobs = append(st.Jobs, job)
	if err := s.save(st); err != nil {
		return err
	}
	s.armJobLocked(job)
	return nil
}

// Update replaces an existing job while preserving its ID and creation time.
func (s *Service) Update(id string, job CronJob) (bool, CronJob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id = strings.TrimSpace(id)
	if id == "" {
		return false, CronJob{}, fmt.Errorf("job id is required")
	}
	if err := ValidateSchedule(job.Schedule); err != nil {
		return false, CronJob{}, err
	}
	job.Payload = NormalizePayload(job.Payload)
	if err := ValidatePayload(job.Payload); err != nil {
		return false, CronJob{}, err
	}
	st, err := s.load()
	if err != nil {
		return false, CronJob{}, err
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
			return true, current, err
		}
		s.armJobLocked(job)
		return true, job, nil
	}
	return false, CronJob{}, nil
}

// SetEnabled toggles a job and updates scheduler state.
func (s *Service) SetEnabled(id string, enabled bool) (bool, CronJob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id = strings.TrimSpace(id)
	if id == "" {
		return false, CronJob{}, fmt.Errorf("job id is required")
	}
	st, err := s.load()
	if err != nil {
		return false, CronJob{}, err
	}
	for i := range st.Jobs {
		if st.Jobs[i].ID != id {
			continue
		}
		if enabled {
			if err := ValidateSchedule(st.Jobs[i].Schedule); err != nil {
				return true, st.Jobs[i], err
			}
			st.Jobs[i].Payload = NormalizePayload(st.Jobs[i].Payload)
			if err := ValidatePayload(st.Jobs[i].Payload); err != nil {
				return true, st.Jobs[i], err
			}
		}
		s.unarmJobLocked(id)
		st.Jobs[i].Enabled = enabled
		st.Jobs[i].UpdatedAtMS = time.Now().UnixMilli()
		st.Jobs[i] = s.prepareJobLocked(st.Jobs[i])
		if err := s.save(st); err != nil {
			return true, st.Jobs[i], err
		}
		s.armJobLocked(st.Jobs[i])
		return true, st.Jobs[i], nil
	}
	return false, CronJob{}, nil
}

// Remove deletes the job with id and reports whether one was found.
func (s *Service) Remove(id string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, err := s.load()
	if err != nil {
		return false, err
	}
	found := false
	n := make([]CronJob, 0, len(st.Jobs))
	for _, j := range st.Jobs {
		if j.ID == id {
			found = true
			s.unarmJobLocked(id)
			continue
		}
		n = append(n, j)
	}
	st.Jobs = n
	if err := s.save(st); err != nil {
		return false, err
	}
	return found, nil
}

// RunNow runs the job with id immediately.
//
// When force is false, disabled jobs are skipped and reported as not run.
func (s *Service) RunNow(ctx context.Context, id string, force bool) (bool, error) {
	return s.runJobByID(ctx, id, force)
}

func (s *Service) runJobByID(ctx context.Context, id string, force bool) (bool, error) {
	s.mu.Lock()
	st, err := s.load()
	s.mu.Unlock()
	if err != nil {
		return false, err
	}
	for _, j := range st.Jobs {
		if j.ID == id {
			if !force && !j.Enabled {
				return false, nil
			}
			if s.runner == nil {
				return true, fmt.Errorf("cron runner not configured")
			}
			result, err := s.runner(ctx, j)
			s.mu.Lock()
			defer s.mu.Unlock()
			st2, loadErr := s.load()
			if loadErr != nil {
				return true, err
			}
			shouldDelete := false
			for i := range st2.Jobs {
				if st2.Jobs[i].ID == id {
					now := time.Now().UnixMilli()
					st2.Jobs[i].State.LastRunAtMS = &now
					if err != nil {
						st2.Jobs[i].State.LastStatus = "error"
						st2.Jobs[i].State.LastError = err.Error()
					} else {
						st2.Jobs[i].State.LastStatus = "ok"
						st2.Jobs[i].State.LastError = ""
					}
					st2.Jobs[i].State.LastEnqueuedJobID = result.EnqueuedJobID
					st2.Jobs[i].State.LastEnqueuedRunID = result.EnqueuedRunID
					if st2.Jobs[i].DeleteAfterRun {
						shouldDelete = true
						break
					}
					st2.Jobs[i] = s.prepareJobLocked(st2.Jobs[i])
					break
				}
			}
			if shouldDelete {
				next := make([]CronJob, 0, len(st2.Jobs))
				for _, jj := range st2.Jobs {
					if jj.ID == id {
						continue
					}
					next = append(next, jj)
				}
				st2.Jobs = next
				s.unarmJobLocked(id)
			}
			if saveErr := s.save(st2); saveErr != nil {
				log.Printf("cron save failed: %v", saveErr)
			}
			return true, err
		}
	}
	return false, nil
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
	next := nextRunMS(job.Schedule, time.Now())
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
		if time.Now().After(at) {
			return
		}
		s.timers[job.ID] = time.AfterFunc(time.Until(at), func() {
			if _, err := s.runJobByID(context.Background(), job.ID, false); err != nil {
				log.Printf("cron runner error: id=%s err=%v", job.ID, err)
			}
		})
	case KindEvery:
		sec := int64(job.Schedule.EveryMS / 1000)
		if sec <= 0 {
			sec = 60
		}
		spec := "@every " + (time.Duration(sec) * time.Second).String()
		eid, err := s.c.AddFunc(spec, func() {
			if _, e := s.runJobByID(context.Background(), job.ID, false); e != nil {
				log.Printf("cron runner error: id=%s err=%v", job.ID, e)
			}
		})
		if err == nil {
			s.entries[job.ID] = eid
		} else {
			log.Printf("cron schedule add failed: id=%s spec=%s err=%v", job.ID, spec, err)
		}
	case KindCron:
		spec := job.Schedule.Expr
		eid, err := s.c.AddFunc(spec, func() {
			if _, e := s.runJobByID(context.Background(), job.ID, false); e != nil {
				log.Printf("cron runner error: id=%s err=%v", job.ID, e)
			}
		})
		if err == nil {
			s.entries[job.ID] = eid
		} else {
			log.Printf("cron schedule add failed: id=%s spec=%s err=%v", job.ID, spec, err)
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
		if run.Meta != nil {
			meta := make(map[string]any, len(run.Meta))
			for k, v := range run.Meta {
				meta[k] = v
			}
			run.Meta = meta
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
		if schedule.EveryMS < 0 {
			return fmt.Errorf("every_ms must not be negative")
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

func cronParser() cron.Parser {
	return cron.NewParser(cron.SecondOptional | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)
}

func randUint() uint64 {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return uint64(time.Now().UnixNano())
	}
	return binary.LittleEndian.Uint64(b[:])
}

func randID() string {
	const chars = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, 10)
	for i := range b {
		b[i] = chars[int(randUint()%uint64(len(chars)))]
	}
	return string(b)
}
