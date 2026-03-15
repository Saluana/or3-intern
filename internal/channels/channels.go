// Package channels defines the shared channel interfaces and metadata helpers.
package channels

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"or3-intern/internal/bus"
)

const defaultDeduplicatorTTL = 5 * time.Minute

// IngressDeduplicator tracks recently seen message identifiers and blocks
// duplicate delivery within a configurable window. It is safe for concurrent
// use.
type IngressDeduplicator struct {
	mu   sync.Mutex
	seen map[string]time.Time
	ttl  time.Duration
}

// NewIngressDeduplicator creates a deduplicator with the given TTL (how long a
// seen key is remembered). A zero or negative TTL defaults to 5 minutes.
func NewIngressDeduplicator(ttl time.Duration) *IngressDeduplicator {
	if ttl <= 0 {
		ttl = defaultDeduplicatorTTL
	}
	return &IngressDeduplicator{
		seen: make(map[string]time.Time),
		ttl:  ttl,
	}
}

// IsDuplicate returns true when key was already seen within the TTL window.
// Evicts stale entries on each call.
func (d *IngressDeduplicator) IsDuplicate(key string) bool {
	now := time.Now()
	d.mu.Lock()
	defer d.mu.Unlock()
	d.evictExpired(now)
	if _, exists := d.seen[key]; exists {
		return true
	}
	d.seen[key] = now
	return false
}

// evictExpired must be called with d.mu held.
func (d *IngressDeduplicator) evictExpired(now time.Time) {
	for k, t := range d.seen {
		if now.Sub(t) >= d.ttl {
			delete(d.seen, k)
		}
	}
}

const (
	// MetaMediaPaths stores local media attachments that accompany a delivery.
	MetaMediaPaths = "media_paths"
	// MetaThreadTS stores a thread identifier for threaded channels.
	MetaThreadTS = "thread_ts"
	// MetaReplyToMessageID stores a provider-specific reply target identifier.
	MetaReplyToMessageID = "reply_to_message_id"
	// MetaMessageReference stores a provider-specific reply reference payload.
	MetaMessageReference = "message_reference"
)

// Channel is the transport contract implemented by each messaging integration.
type Channel interface {
	Name() string
	Start(ctx context.Context, eventBus *bus.Bus) error
	Stop(ctx context.Context) error
	Deliver(ctx context.Context, to, text string, meta map[string]any) error
}

// Manager owns a named set of channels and their start state.
type Manager struct {
	mu       sync.RWMutex
	channels map[string]Channel
	started  map[string]bool
}

// NewManager constructs an empty channel manager.
func NewManager() *Manager {
	return &Manager{channels: map[string]Channel{}, started: map[string]bool{}}
}

// Register adds ch under its normalized name.
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

// Names returns the registered channel names in sorted order.
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

// StartAll starts every registered channel in name order.
func (m *Manager) StartAll(ctx context.Context, eventBus *bus.Bus) error {
	for _, name := range m.Names() {
		if err := m.Start(ctx, name, eventBus); err != nil {
			return err
		}
	}
	return nil
}

// Start starts the named channel if it is not already running.
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

// StopAll stops all registered channels and joins any returned errors.
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

// Stop stops the named channel if it is running.
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

// Deliver sends text on channel without extra metadata.
func (m *Manager) Deliver(ctx context.Context, channel, to, text string) error {
	return m.DeliverWithMeta(ctx, channel, to, text, nil)
}

// DeliverWithMeta sends text on channel with provider-specific metadata.
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

// CloneMeta returns a shallow copy of meta.
func CloneMeta(meta map[string]any) map[string]any {
	if len(meta) == 0 {
		return nil
	}
	out := make(map[string]any, len(meta))
	for k, v := range meta {
		out[k] = v
	}
	return out
}

// ReplyMeta extracts only reply-thread metadata from meta.
func ReplyMeta(meta map[string]any) map[string]any {
	if len(meta) == 0 {
		return nil
	}
	out := map[string]any{}
	for _, key := range []string{MetaThreadTS, MetaReplyToMessageID, MetaMessageReference} {
		if value, ok := meta[key]; ok && hasMeaningfulMetaValue(value) {
			out[key] = value
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func hasMeaningfulMetaValue(value any) bool {
	switch v := value.(type) {
	case nil:
		return false
	case string:
		return strings.TrimSpace(v) != ""
	case int:
		return v > 0
	case int8:
		return v > 0
	case int16:
		return v > 0
	case int32:
		return v > 0
	case int64:
		return v > 0
	case uint:
		return v > 0
	case uint8:
		return v > 0
	case uint16:
		return v > 0
	case uint32:
		return v > 0
	case uint64:
		return v > 0
	default:
		text := strings.TrimSpace(fmt.Sprint(value))
		return text != "" && text != "<nil>"
	}
}
