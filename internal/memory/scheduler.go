package memory

import (
	"context"
	"sync"
	"time"
)

type Scheduler struct {
	timeout time.Duration
	run     func(context.Context, string)
	baseCtx context.Context

	mu       sync.Mutex
	sessions map[string]*schedulerState
}

type schedulerState struct {
	running bool
	dirty   bool
}

func NewScheduler(timeout time.Duration, run func(context.Context, string)) *Scheduler {
	return NewSchedulerWithContext(context.Background(), timeout, run)
}

func NewSchedulerWithContext(baseCtx context.Context, timeout time.Duration, run func(context.Context, string)) *Scheduler {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	if baseCtx == nil {
		baseCtx = context.Background()
	}
	return &Scheduler{
		timeout:  timeout,
		run:      run,
		baseCtx:  baseCtx,
		sessions: map[string]*schedulerState{},
	}
}

func (s *Scheduler) Trigger(sessionKey string) {
	if s == nil || s.run == nil || sessionKey == "" {
		return
	}
	s.mu.Lock()
	state, ok := s.sessions[sessionKey]
	if !ok {
		state = &schedulerState{}
		s.sessions[sessionKey] = state
	}
	if state.running {
		state.dirty = true
		s.mu.Unlock()
		return
	}
	state.running = true
	state.dirty = false
	s.mu.Unlock()

	go s.runLoop(sessionKey)
}

func (s *Scheduler) runLoop(sessionKey string) {
	for {
		base := s.baseCtx
		if base == nil {
			base = context.Background()
		}
		ctx, cancel := context.WithTimeout(base, s.timeout)
		s.run(ctx, sessionKey)
		cancel()

		s.mu.Lock()
		state := s.sessions[sessionKey]
		if state == nil {
			s.mu.Unlock()
			return
		}
		if state.dirty {
			state.dirty = false
			s.mu.Unlock()
			continue
		}
		state.running = false
		s.mu.Unlock()
		return
	}
}
