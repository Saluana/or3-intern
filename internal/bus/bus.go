package bus

import (
	"context"
)

type EventType string

const (
	EventUserMessage EventType = "user_message"
	EventCron EventType = "cron"
	EventSystem EventType = "system"
)

type Event struct {
	Type EventType
	SessionKey string
	Channel string
	From string
	Message string
	Meta map[string]any
}

type Handler func(ctx context.Context, ev Event) error

type Bus struct {
	ch chan Event
}

func New(buffer int) *Bus {
	if buffer <= 0 { buffer = 128 }
	return &Bus{ch: make(chan Event, buffer)}
}

func (b *Bus) Publish(ev Event) { b.ch <- ev }
func (b *Bus) Channel() <-chan Event { return b.ch }
