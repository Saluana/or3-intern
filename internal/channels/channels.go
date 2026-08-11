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

const (
	defaultDeduplicatorTTL        = 5 * time.Minute
	defaultDeduplicatorMaxEntries = 4096
)

// IngressDeduplicator tracks recently seen message identifiers and blocks
// duplicate delivery within a configurable window. It is safe for concurrent
// use.
type IngressDeduplicator struct {
	mu         sync.Mutex
	seen       map[string]time.Time
	ttl        time.Duration
	maxEntries int
}

// NewIngressDeduplicator creates a deduplicator with the given TTL (how long a
// seen key is remembered). A zero or negative TTL defaults to 5 minutes.
func NewIngressDeduplicator(ttl time.Duration) *IngressDeduplicator {
	if ttl <= 0 {
		ttl = defaultDeduplicatorTTL
	}
	return &IngressDeduplicator{
		seen:       make(map[string]time.Time),
		ttl:        ttl,
		maxEntries: defaultDeduplicatorMaxEntries,
	}
}

// IsDuplicate returns true when key was already seen within the TTL window.
// Evicts stale entries on each call.
func (d *IngressDeduplicator) IsDuplicate(key string) bool {
	if d == nil || strings.TrimSpace(key) == "" {
		return false
	}
	now := time.Now()
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.seen == nil {
		d.seen = make(map[string]time.Time)
	}
	d.evictExpired(now)
	if _, exists := d.seen[key]; exists {
		return true
	}
	maxEntries := d.maxEntries
	if maxEntries <= 0 {
		maxEntries = defaultDeduplicatorMaxEntries
	}
	for len(d.seen) >= maxEntries {
		d.evictOldest()
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

// evictOldest must be called with d.mu held and only while d.seen is nonempty.
func (d *IngressDeduplicator) evictOldest() {
	var oldestKey string
	var oldest time.Time
	found := false
	for key, seenAt := range d.seen {
		if !found || seenAt.Before(oldest) {
			oldestKey = key
			oldest = seenAt
			found = true
		}
	}
	if found {
		delete(d.seen, oldestKey)
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

// MetaDeliverer sends a completed response with channel-specific metadata.
type MetaDeliverer interface {
	DeliverWithMeta(ctx context.Context, channel, to, text string, meta map[string]any) error
}

// Channel is the transport contract implemented by each messaging integration.
type Channel interface {
	Name() string
	Start(ctx context.Context, eventBus *bus.Bus) error
	Stop(ctx context.Context) error
	Deliver(ctx context.Context, to, text string, meta map[string]any) error
}

// ConnectionStatusProvider is optionally implemented by channels that keep a
// live inbound connection. Values are intended for diagnostics: connected,
// reconnecting, failed, or stopped.
type ConnectionStatusProvider interface {
	ConnectionStatus() string
}

// TypingIndicator is optionally implemented by channels that can show a
// transient "assistant is working" state while a turn is being processed.
type TypingIndicator interface {
	StartTyping(ctx context.Context, to string, meta map[string]any) func()
}

// Manager owns a named set of channels and their start state.
type Manager struct {
	mu       sync.RWMutex
	channels map[string]Channel
	started  map[string]*channelStart
	starting map[string]*channelStart
}

type channelStart struct {
	cancel context.CancelFunc
	done   chan struct{}
}

// NewManager constructs an empty channel manager.
func NewManager() *Manager {
	return &Manager{
		channels: map[string]Channel{},
		started:  map[string]*channelStart{},
		starting: map[string]*channelStart{},
	}
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

// ConnectionStatuses returns the current diagnostic state for registered
// channels. Channels without a persistent inbound connection report unknown.
func (m *Manager) ConnectionStatuses() map[string]string {
	if m == nil {
		return map[string]string{}
	}
	m.mu.RLock()
	channels := make(map[string]Channel, len(m.channels))
	started := make(map[string]bool, len(m.started))
	starting := make(map[string]bool, len(m.starting))
	for name, ch := range m.channels {
		channels[name] = ch
	}
	for name := range m.started {
		started[name] = true
	}
	for name := range m.starting {
		starting[name] = true
	}
	m.mu.RUnlock()
	statuses := make(map[string]string, len(channels))
	for name, ch := range channels {
		if provider, ok := ch.(ConnectionStatusProvider); ok {
			if status := provider.ConnectionStatus(); status != "" && status != "stopped" {
				statuses[name] = status
				continue
			}
		}
		if !started[name] && !starting[name] {
			statuses[name] = "stopped"
			continue
		}
		if provider, ok := ch.(ConnectionStatusProvider); ok {
			statuses[name] = provider.ConnectionStatus()
			continue
		}
		statuses[name] = "unknown"
	}
	return statuses
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
	name = strings.TrimSpace(strings.ToLower(name))
	ch, err := m.get(name)
	if err != nil {
		return err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	m.mu.Lock()
	if m.started[name] != nil || m.starting[name] != nil {
		m.mu.Unlock()
		return nil
	}
	startCtx, cancel := context.WithCancel(ctx)
	start := &channelStart{cancel: cancel, done: make(chan struct{})}
	m.starting[name] = start
	m.mu.Unlock()

	err = ch.Start(startCtx, eventBus)
	m.mu.Lock()
	if m.starting[name] == start {
		delete(m.starting, name)
		if err == nil && startCtx.Err() == nil {
			m.started[name] = start
		}
	}
	close(start.done)
	m.mu.Unlock()
	if err != nil {
		start.cancel()
		return err
	}
	if err := startCtx.Err(); err != nil {
		start.cancel()
		return err
	}
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
	name = strings.TrimSpace(strings.ToLower(name))
	ch, err := m.get(name)
	if err != nil {
		return err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	m.mu.Lock()
	running := m.started[name]
	start := m.starting[name]
	if start != nil {
		start.cancel()
	}
	if running != nil {
		running.cancel()
	}
	m.mu.Unlock()
	if running == nil && start == nil {
		return nil
	}
	if err := ch.Stop(ctx); err != nil {
		return err
	}
	if start != nil {
		select {
		case <-start.done:
		case <-ctx.Done():
			return ctx.Err()
		}
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

// StartTyping starts a channel-specific typing indicator when supported.
// The returned function is always safe to call and stops the indicator loop.
func (m *Manager) StartTyping(ctx context.Context, channel, to string, meta map[string]any) func() {
	ch, err := m.get(channel)
	if err != nil {
		return func() {}
	}
	typing, ok := ch.(TypingIndicator)
	if !ok {
		return func() {}
	}
	return typing.StartTyping(ctx, to, meta)
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
