// Package bus provides a single-process fan-out event bus.
package bus

import (
	"log"
	"sync"
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
)

// Bus is a buffered single-process fan-out event bus.
type Bus struct {
	mu          sync.RWMutex
	once        sync.Once
	buffer      int
	closed      bool
	legacy      chan Event
	legacyUsed  bool
	subscribers map[chan Event]struct{}
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
	return &Bus{buffer: buffer, legacy: legacy, subscribers: map[chan Event]struct{}{legacy: struct{}{}}}
}

// Subscribe returns a per-subscriber event stream and an idempotent unsubscribe function.
func (b *Bus) Subscribe() (<-chan Event, func()) {
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
	b.subscribers[ch] = struct{}{}
	b.mu.Unlock()

	var once sync.Once
	unsubscribe := func() {
		once.Do(func() {
			b.mu.Lock()
			if _, ok := b.subscribers[ch]; ok {
				delete(b.subscribers, ch)
				close(ch)
			}
			b.mu.Unlock()
		})
	}
	return ch, unsubscribe
}

// Publish fans ev out without blocking and reports whether at least one active
// subscriber accepted it. Slow optional subscribers may miss events without
// making the publish fail for critical producers.
func (b *Bus) Publish(ev Event) bool {
	if b == nil {
		return false
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	if b.closed {
		log.Printf("bus: event dropped, bus closed (type=%s)", ev.Type)
		return false
	}
	delivered := false
	for ch := range b.subscribers {
		select {
		case ch <- ev:
			delivered = true
		default:
			if ch == b.legacy && !b.legacyUsed {
				continue
			}
			log.Printf("bus: event dropped, subscriber buffer full (type=%s)", ev.Type)
		}
	}
	return delivered
}

// Close closes every subscriber stream once.
func (b *Bus) Close() {
	if b == nil {
		return
	}
	b.once.Do(func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		b.closed = true
		for ch := range b.subscribers {
			close(ch)
			delete(b.subscribers, ch)
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
	b.subscribers[b.legacy] = struct{}{}
	return b.legacy
}
