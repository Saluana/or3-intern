package channels

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"or3-intern/internal/bus"
)

type Channel interface {
	Name() string
	Start(ctx context.Context, eventBus *bus.Bus) error
	Stop(ctx context.Context) error
	Deliver(ctx context.Context, to, text string, meta map[string]any) error
}

type Manager struct {
	mu       sync.RWMutex
	channels map[string]Channel
	started  map[string]bool
}

func NewManager() *Manager {
	return &Manager{channels: map[string]Channel{}, started: map[string]bool{}}
}

func (m *Manager) Register(ch Channel) error {
	if ch == nil {
		return errors.New("nil channel")
	}
	name := strings.TrimSpace(strings.ToLower(ch.Name()))
	if name == "" {
		return errors.New("channel name required")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.channels[name]; exists {
		return fmt.Errorf("channel already registered: %s", name)
	}
	m.channels[name] = ch
	return nil
}

func (m *Manager) Names() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]string, 0, len(m.channels))
	for name := range m.channels {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func (m *Manager) StartAll(ctx context.Context, eventBus *bus.Bus) error {
	for _, name := range m.Names() {
		if err := m.Start(ctx, name, eventBus); err != nil {
			return err
		}
	}
	return nil
}

func (m *Manager) Start(ctx context.Context, name string, eventBus *bus.Bus) error {
	ch, err := m.get(name)
	if err != nil {
		return err
	}
	m.mu.Lock()
	if m.started[name] {
		m.mu.Unlock()
		return nil
	}
	m.mu.Unlock()
	if err := ch.Start(ctx, eventBus); err != nil {
		return err
	}
	m.mu.Lock()
	m.started[name] = true
	m.mu.Unlock()
	return nil
}

func (m *Manager) StopAll(ctx context.Context) error {
	var errs []string
	for _, name := range m.Names() {
		if err := m.Stop(ctx, name); err != nil {
			errs = append(errs, err.Error())
		}
	}
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

func (m *Manager) Stop(ctx context.Context, name string) error {
	ch, err := m.get(name)
	if err != nil {
		return err
	}
	m.mu.Lock()
	started := m.started[name]
	m.mu.Unlock()
	if !started {
		return nil
	}
	if err := ch.Stop(ctx); err != nil {
		return err
	}
	m.mu.Lock()
	delete(m.started, name)
	m.mu.Unlock()
	return nil
}

func (m *Manager) Deliver(ctx context.Context, channel, to, text string) error {
	return m.DeliverWithMeta(ctx, channel, to, text, nil)
}

func (m *Manager) DeliverWithMeta(ctx context.Context, channel, to, text string, meta map[string]any) error {
	if strings.TrimSpace(channel) == "" {
		channel = "cli"
	}
	ch, err := m.get(channel)
	if err != nil {
		return err
	}
	return ch.Deliver(ctx, to, text, meta)
}

func (m *Manager) get(name string) (Channel, error) {
	name = strings.TrimSpace(strings.ToLower(name))
	m.mu.RLock()
	defer m.mu.RUnlock()
	ch := m.channels[name]
	if ch == nil {
		return nil, fmt.Errorf("channel not found: %s", name)
	}
	return ch, nil
}
