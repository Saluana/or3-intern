// Package bus provides a small in-memory event bus for cross-service signaling.
package bus

import (
	"context"
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

// Handler consumes a published event.
type Handler func(ctx context.Context, ev Event) error

// Bus is a buffered single-channel event queue.
type Bus struct {
	ch chan Event
}

// New constructs a Bus with buffer slots, defaulting to 128 when buffer <= 0.
func New(buffer int) *Bus {
	if buffer <= 0 {
		buffer = 128
	}
	return &Bus{ch: make(chan Event, buffer)}
}

// Publish enqueues ev without blocking and reports whether it was accepted.
func (b *Bus) Publish(ev Event) bool {
	select {
	case b.ch <- ev:
		return true
	default:
		return false
	}
}

// Channel returns the receive-only event stream.
func (b *Bus) Channel() <-chan Event { return b.ch }
