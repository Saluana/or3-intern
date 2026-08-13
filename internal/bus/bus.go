// Package bus provides a single-process fan-out event bus.
package bus

import (
	"log"
	"sync"
	"sync/atomic"
	"time"
)

// EventType classifies messages sent through a Bus.
type EventType string

const (
	// EventUserMessage represents a user-authored message.
	EventUserMessage EventType = "user_message"
	// EventCron represents a scheduled cron job turn.
	EventCron EventType = "cron"
	// EventHeartbeat represents a periodic heartbeat turn.
	EventHeartbeat EventType = "heartbeat"
	// EventSystem represents an internal system event.
	EventSystem EventType = "system"
	// EventWebhook represents an inbound webhook trigger.
	EventWebhook EventType = "webhook"
	// EventFileChange represents a filesystem-triggered event.
	EventFileChange EventType = "file_change"
)

// Event is a single message published on the bus.
type Event struct {
	Type       EventType
	SessionKey string
	Channel    string
	From       string
	Message    string
	Meta       map[string]any
}

const (
	defaultBufferSize = 128
	maxBufferSize     = 1_000_000
	criticalSendWait  = 100 * time.Millisecond
)

// Bus is a buffered single-process fan-out event bus.
type Bus struct {
	mu          sync.RWMutex
	once        sync.Once
	buffer      int
	closed      bool
	legacy      chan Event
	legacyUsed  bool
	subscribers map[chan Event]*subscriberState
}

type subscriberState struct {
	mu      sync.Mutex
	ch      chan Event
	closed  bool
	name    string
	dropped atomic.Uint64
}

func (s *subscriberState) deliver(ev Event, wait bool) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return false
	}
	if !wait {
		select {
		case s.ch <- ev:
			return true
		default:
			return false
		}
	}
	timer := time.NewTimer(criticalSendWait)
	defer timer.Stop()
	select {
	case s.ch <- ev:
		return true
	case <-timer.C:
		return false
	}
}

func (s *subscriberState) close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.closed {
		s.closed = true
		close(s.ch)
	}
}

// SubscriberHealth exposes delivery loss without requiring log scraping.
type SubscriberHealth struct {
	Name    string
	Dropped uint64
}

// New constructs a Bus with per-subscriber buffer slots, defaulting to 128 when buffer <= 0.
func New(buffer int) *Bus {
	if buffer <= 0 {
		buffer = defaultBufferSize
	}
	if buffer > maxBufferSize {
		panic("bus buffer exceeds maxBufferSize")
	}
	legacy := make(chan Event, buffer)
	return &Bus{buffer: buffer, legacy: legacy, subscribers: map[chan Event]*subscriberState{legacy: {ch: legacy, name: "legacy-worker-queue"}}}
}

// Subscribe returns a per-subscriber event stream and an idempotent unsubscribe function.
func (b *Bus) Subscribe() (<-chan Event, func()) {
	return b.SubscribeNamed("optional")
}

// SubscribeNamed registers an observable optional subscriber.
func (b *Bus) SubscribeNamed(name string) (<-chan Event, func()) {
	if b == nil {
		ch := make(chan Event)
		close(ch)
		return ch, func() {}
	}
	ch := make(chan Event, b.buffer)
	b.mu.Lock()
	if b.closed {
		close(ch)
		b.mu.Unlock()
		return ch, func() {}
	}
	state := &subscriberState{ch: ch, name: name}
	b.subscribers[ch] = state
	b.mu.Unlock()

	var once sync.Once
	unsubscribe := func() {
		once.Do(func() {
			b.mu.Lock()
			if _, ok := b.subscribers[ch]; ok {
				delete(b.subscribers, ch)
			}
			b.mu.Unlock()
			state.close()
		})
	}
	return ch, unsubscribe
}

// Publish fans ev out and reports whether at least one active subscriber
// accepted it. Delivery is always bounded: callers never block while holding
// the lifecycle lock, so a full queue cannot deadlock Close or unsubscribe.
func (b *Bus) Publish(ev Event) bool {
	if b == nil {
		return false
	}
	type delivery struct {
		state      *subscriberState
		critical   bool
		reportDrop bool
	}
	b.mu.RLock()
	if b.closed {
		b.mu.RUnlock()
		log.Printf("bus: event dropped, bus closed (type=%s)", ev.Type)
		return false
	}
	deliveries := make([]delivery, 0, len(b.subscribers))
	for ch, state := range b.subscribers {
		legacy := ch == b.legacy
		deliveries = append(deliveries, delivery{
			state:      state,
			critical:   legacy && b.legacyUsed,
			reportDrop: !legacy || b.legacyUsed,
		})
	}
	b.mu.RUnlock()
	delivered := false
	for _, target := range deliveries {
		if target.state.deliver(ev, target.critical) {
			delivered = true
			continue
		}
		if !target.reportDrop {
			continue
		}
		dropped := target.state.dropped.Add(1)
		log.Printf("bus: event dropped, subscriber=%s buffer full (type=%s dropped=%d)", target.state.name, ev.Type, dropped)
	}
	return delivered
}

// Health returns a point-in-time delivery-loss snapshot for every subscriber.
func (b *Bus) Health() []SubscriberHealth {
	if b == nil {
		return nil
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	result := make([]SubscriberHealth, 0, len(b.subscribers))
	for _, state := range b.subscribers {
		result = append(result, SubscriberHealth{Name: state.name, Dropped: state.dropped.Load()})
	}
	return result
}

// Close closes every subscriber stream once.
func (b *Bus) Close() {
	if b == nil {
		return
	}
	b.once.Do(func() {
		b.mu.Lock()
		b.closed = true
		states := make([]*subscriberState, 0, len(b.subscribers))
		for ch, state := range b.subscribers {
			states = append(states, state)
			delete(b.subscribers, ch)
		}
		b.mu.Unlock()
		for _, state := range states {
			state.close()
		}
	})
}

// Channel returns a shared receive-only queue stream.
//
// Deprecated: use Subscribe for broadcast fan-out. Channel is retained for
// worker-pool queue semantics where multiple consumers split work.
func (b *Bus) Channel() <-chan Event {
	if b == nil {
		ch := make(chan Event)
		close(ch)
		return ch
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.legacy != nil {
		b.legacyUsed = true
		return b.legacy
	}
	b.legacy = make(chan Event, b.buffer)
	if b.closed {
		close(b.legacy)
		return b.legacy
	}
	b.legacyUsed = true
	b.subscribers[b.legacy] = &subscriberState{ch: b.legacy, name: "legacy-worker-queue"}
	return b.legacy
}
