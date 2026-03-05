package cron

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"errors"
	"log"
	"os"
	"sync"
	"time"

	"github.com/robfig/cron/v3"
)

type ScheduleKind string
const (
	KindAt ScheduleKind = "at"
	KindEvery ScheduleKind = "every"
	KindCron ScheduleKind = "cron"
)

type CronSchedule struct {
	Kind ScheduleKind `json:"kind"`
	AtMS int64 `json:"at_ms,omitempty"`
	EveryMS int64 `json:"every_ms,omitempty"`
	Expr string `json:"expr,omitempty"`
	TZ string `json:"tz,omitempty"`
}

type CronPayload struct {
	Kind string `json:"kind"` // "agent_turn"|"system_event"
	Message string `json:"message"`
	Deliver bool `json:"deliver"`
	Channel string `json:"channel,omitempty"`
	To string `json:"to,omitempty"`
}

type CronJobState struct {
	NextRunAtMS *int64 `json:"next_run_at_ms,omitempty"`
	LastRunAtMS *int64 `json:"last_run_at_ms,omitempty"`
	LastStatus string `json:"last_status,omitempty"` // ok|error|skipped
	LastError string `json:"last_error,omitempty"`
}

type CronJob struct {
	ID string `json:"id"`
	Name string `json:"name"`
	Enabled bool `json:"enabled"`
	Schedule CronSchedule `json:"schedule"`
	Payload CronPayload `json:"payload"`
	State CronJobState `json:"state"`
	CreatedAtMS int64 `json:"created_at_ms"`
	UpdatedAtMS int64 `json:"updated_at_ms"`
	DeleteAfterRun bool `json:"delete_after_run"`
}

type Store struct {
	Version int `json:"version"`
	Jobs []CronJob `json:"jobs"`
}

type Runner func(ctx context.Context, job CronJob) error

type Service struct {
	mu sync.Mutex
	path string
	runner Runner
	c *cron.Cron
	entries map[string]cron.EntryID
}

func New(path string, runner Runner) *Service {
	return &Service{
		path: path,
		runner: runner,
		entries: map[string]cron.EntryID{},
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
	if err := os.MkdirAll(filepathDir(s.path), 0o755); err != nil { return err }
	b, _ := json.MarshalIndent(st, "", "  ")
	return os.WriteFile(s.path, b, 0o644)
}

func filepathDir(p string) string {
	i := len(p)-1
	for i >= 0 && p[i] != '/' && p[i] != '\\' { i-- }
	if i <= 0 { return "." }
	return p[:i]
}

func (s *Service) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.c != nil { return nil }

	s.c = cron.New(cron.WithSeconds(), cron.WithParser(cron.NewParser(cron.SecondOptional|cron.Minute|cron.Hour|cron.Dom|cron.Month|cron.Dow)))
	st, err := s.load()
	if err != nil { return err }
	for _, j := range st.Jobs {
		s.armJobLocked(j)
	}
	s.c.Start()
	return nil
}

func (s *Service) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.c != nil {
		ctx := s.c.Stop()
		<-ctx.Done()
		s.c = nil
		s.entries = map[string]cron.EntryID{}
	}
}

func (s *Service) Status() (map[string]any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, err := s.load()
	if err != nil { return nil, err }
	next := int64(0)
	for _, j := range st.Jobs {
		if j.State.NextRunAtMS != nil {
			if next == 0 || *j.State.NextRunAtMS < next { next = *j.State.NextRunAtMS }
		}
	}
	var nextPtr *int64
	if next != 0 { nextPtr = &next }
	return map[string]any{"jobs": len(st.Jobs), "next_wake_at_ms": nextPtr}, nil
}

func (s *Service) List() ([]CronJob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, err := s.load()
	if err != nil { return nil, err }
	return st.Jobs, nil
}

func (s *Service) Add(job CronJob) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, err := s.load()
	if err != nil { return err }
	now := time.Now().UnixMilli()
	job.CreatedAtMS = now
	job.UpdatedAtMS = now
	if job.ID == "" { job.ID = randID() }
	if job.Name == "" { job.Name = job.ID }
	st.Jobs = append(st.Jobs, job)
	if err := s.save(st); err != nil { return err }
	s.armJobLocked(job)
	return nil
}

func (s *Service) Remove(id string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, err := s.load()
	if err != nil { return false, err }
	found := false
	n := make([]CronJob, 0, len(st.Jobs))
	for _, j := range st.Jobs {
		if j.ID == id {
			found = true
			if eid, ok := s.entries[id]; ok && s.c != nil {
				s.c.Remove(eid)
				delete(s.entries, id)
			}
			continue
		}
		n = append(n, j)
	}
	st.Jobs = n
	if err := s.save(st); err != nil { return false, err }
	return found, nil
}

func (s *Service) RunNow(ctx context.Context, id string, force bool) (bool, error) {
	s.mu.Lock()
	st, err := s.load()
	s.mu.Unlock()
	if err != nil { return false, err }
	for _, j := range st.Jobs {
		if j.ID == id {
			if !force && !j.Enabled { return false, nil }
			err := s.runner(ctx, j)
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
					if st2.Jobs[i].DeleteAfterRun {
						shouldDelete = true
						break
					}
					break
				}
			}
			if shouldDelete {
				next := make([]CronJob, 0, len(st2.Jobs))
				for _, jj := range st2.Jobs {
					if jj.ID == id { continue }
					next = append(next, jj)
				}
				st2.Jobs = next
				if eid, ok := s.entries[id]; ok && s.c != nil {
					s.c.Remove(eid)
					delete(s.entries, id)
				}
			}
			if saveErr := s.save(st2); saveErr != nil {
				log.Printf("cron save failed: %v", saveErr)
			}
			return true, err
		}
	}
	return false, nil
}

func (s *Service) armJobLocked(job CronJob) {
	if s.c == nil { return }
	if !job.Enabled { return }
	switch job.Schedule.Kind {
	case KindAt:
		at := time.UnixMilli(job.Schedule.AtMS)
		if time.Now().After(at) { return }
		delay := time.Until(at)
		// schedule using timer goroutine
		go func(id string, d time.Duration) {
			time.Sleep(d)
			if err := s.runner(context.Background(), job); err != nil {
				log.Printf("cron runner error: id=%s err=%v", id, err)
			}
		}(job.ID, delay)
	case KindEvery:
		sec := int64(job.Schedule.EveryMS / 1000)
		if sec <= 0 { sec = 60 }
		spec := "@every " + time.Duration(sec)*time.Second.String()
		eid, err := s.c.AddFunc(spec, func() {
			if e := s.runner(context.Background(), job); e != nil {
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
			if e := s.runner(context.Background(), job); e != nil {
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
	for i := range b { b[i] = chars[int(randUint()%uint64(len(chars)))] }
	return string(b)
}
