package memory

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestScheduler_SingleFlightAndCoalesce(t *testing.T) {
	var started int32
	block := make(chan struct{})
	done := make(chan struct{})
	s := NewScheduler(2*time.Second, func(ctx context.Context, sessionKey string) {
		atomic.AddInt32(&started, 1)
		if atomic.LoadInt32(&started) == 1 {
			<-block
		}
		if atomic.LoadInt32(&started) >= 2 {
			select {
			case done <- struct{}{}:
			default:
			}
		}
	})

	s.Trigger("sess")
	time.Sleep(30 * time.Millisecond)
	s.Trigger("sess")
	close(block)

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("expected coalesced second pass")
	}
	if got := atomic.LoadInt32(&started); got != 2 {
		t.Fatalf("expected exactly 2 runs, got %d", got)
	}
}

func TestScheduler_IndependentSessions(t *testing.T) {
	var mu sync.Mutex
	calls := map[string]int{}
	done := make(chan struct{}, 2)
	s := NewScheduler(2*time.Second, func(ctx context.Context, sessionKey string) {
		mu.Lock()
		calls[sessionKey]++
		mu.Unlock()
		done <- struct{}{}
	})
	s.Trigger("a")
	s.Trigger("b")

	for i := 0; i < 2; i++ {
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("expected both sessions to run")
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if calls["a"] != 1 || calls["b"] != 1 {
		t.Fatalf("unexpected calls: %#v", calls)
	}
}
