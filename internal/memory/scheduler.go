package memory

import (
	"context"
	"sync"
	"time"
)

type Scheduler struct {
	timeout time.Duration
	run     func(context.Context, string)

	mu       sync.Mutex
	sessions map[string]*schedulerState
}

type schedulerState struct {
	running bool
	dirty   bool
}

func NewScheduler(timeout time.Duration, run func(context.Context, string)) *Scheduler {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &Scheduler{
		timeout:  timeout,
		run:      run,
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
		ctx, cancel := context.WithTimeout(context.Background(), s.timeout)
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
