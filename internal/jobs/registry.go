package jobs

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log"
	"sync"
	"time"
)

const (
	defaultJobRetention   = 2 * time.Minute
	defaultMaxTrackedJobs = 256
	defaultMaxJobEvents   = 256
	jobSubscriberBuffer   = 128
)

type Event struct {
	Sequence int64          `json:"sequence"`
	Type     string         `json:"type"`
	Data     map[string]any `json:"data"`
}

type Snapshot struct {
	ID        string    `json:"id"`
	Kind      string    `json:"kind"`
	Status    string    `json:"status"`
	Events    []Event   `json:"events"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Registry struct {
	mu         sync.Mutex
	jobs       map[string]*entry
	retention  time.Duration
	maxTracked int
	maxEvents  int
}

type entry struct {
	id          string
	kind        string
	status      string
	events      []Event
	subscribers map[int]chan Event
	nextSubID   int
	nextSeq     int64
	cancel      context.CancelFunc
	done        chan struct{}
	terminal    bool
	createdAt   time.Time
	updatedAt   time.Time
}

func NewRegistry(retention time.Duration, maxTracked int) *Registry {
	if retention <= 0 {
		retention = defaultJobRetention
	}
	if maxTracked <= 0 {
		maxTracked = defaultMaxTrackedJobs
	}
	return &Registry{
		jobs:       map[string]*entry{},
		retention:  retention,
		maxTracked: maxTracked,
		maxEvents:  defaultMaxJobEvents,
	}
}

func (r *Registry) Register(kind string) Snapshot {
	return r.RegisterWithID(newServiceJobID(), kind)
}

func (r *Registry) RegisterWithID(id string, kind string) Snapshot {
	now := time.Now()
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cleanupLocked(now)
	entry := &entry{
		id:          id,
		kind:        kind,
		status:      "queued",
		subscribers: map[int]chan Event{},
		done:        make(chan struct{}),
		createdAt:   now,
		updatedAt:   now,
	}
	r.jobs[id] = entry
	return snapshotForEntry(entry)
}

func (r *Registry) AttachCancel(id string, cancel context.CancelFunc) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	entry := r.jobs[id]
	if entry == nil {
		return false
	}
	entry.cancel = cancel
	entry.updatedAt = time.Now()
	return true
}

func (r *Registry) Cancel(id string) bool {
	r.mu.Lock()
	entry := r.jobs[id]
	if entry == nil {
		r.mu.Unlock()
		return false
	}
	cancel := entry.cancel
	r.mu.Unlock()
	if cancel == nil {
		return false
	}
	cancel()
	return true
}

func (r *Registry) Publish(id string, eventType string, data map[string]any) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	entry := r.jobs[id]
	if entry == nil {
		return false
	}
	if entry.terminal {
		return false
	}
	if data == nil {
		data = map[string]any{}
	}
	if _, ok := data["job_id"]; !ok {
		data["job_id"] = id
	}
	now := time.Now()
	entry.updatedAt = now
	if status, ok := data["status"].(string); ok && status != "" {
		entry.status = status
	}
	entry.nextSeq++
	event := Event{Sequence: entry.nextSeq, Type: eventType, Data: cloneEventData(data)}
	entry.events = append(entry.events, event)
	if r.maxEvents > 0 && len(entry.events) > r.maxEvents {
		entry.events = append([]Event(nil), entry.events[len(entry.events)-r.maxEvents:]...)
	}
	for _, ch := range entry.subscribers {
		select {
		case ch <- event:
		default:
			log.Printf("job_registry: event dropped for job %s (type=%s, subscriber full)", id, eventType)
		}
	}
	return true
}

func (r *Registry) Complete(id string, status string, data map[string]any) bool {
	if data == nil {
		data = map[string]any{}
	}
	if status == "" {
		status = "completed"
	}
	data["status"] = status
	if !r.Publish(id, "completion", data) {
		return false
	}
	r.markTerminal(id, status)
	return true
}

func (r *Registry) Fail(id string, message string, data map[string]any) bool {
	if data == nil {
		data = map[string]any{}
	}
	if message != "" {
		data["message"] = message
	}
	data["status"] = "failed"
	if !r.Publish(id, "error", data) {
		return false
	}
	r.markTerminal(id, "failed")
	return true
}

func (r *Registry) markTerminal(id string, status string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	entry := r.jobs[id]
	if entry == nil || entry.terminal {
		return
	}
	entry.status = status
	entry.terminal = true
	entry.updatedAt = time.Now()
	close(entry.done)
	for subID, ch := range entry.subscribers {
		close(ch)
		delete(entry.subscribers, subID)
	}
}

func (r *Registry) Subscribe(id string) (Snapshot, <-chan Event, func(), bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	entry := r.jobs[id]
	if entry == nil {
		return Snapshot{}, nil, nil, false
	}
	snapshot := snapshotForEntry(entry)
	if entry.terminal {
		ch := make(chan Event)
		close(ch)
		return snapshot, ch, func() {}, true
	}
	entry.nextSubID++
	subID := entry.nextSubID
	ch := make(chan Event, jobSubscriberBuffer)
	entry.subscribers[subID] = ch
	unsubscribe := func() {
		r.mu.Lock()
		defer r.mu.Unlock()
		current := r.jobs[id]
		if current == nil {
			return
		}
		sub, ok := current.subscribers[subID]
		if !ok {
			return
		}
		close(sub)
		delete(current.subscribers, subID)
	}
	return snapshot, ch, unsubscribe, true
}

func (r *Registry) Snapshot(id string) (Snapshot, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	entry := r.jobs[id]
	if entry == nil {
		return Snapshot{}, false
	}
	return snapshotForEntry(entry), true
}

func (r *Registry) Wait(ctx context.Context, id string) (Snapshot, bool) {
	r.mu.Lock()
	entry := r.jobs[id]
	if entry == nil {
		r.mu.Unlock()
		return Snapshot{}, false
	}
	done := entry.done
	terminal := entry.terminal
	r.mu.Unlock()
	if !terminal {
		select {
		case <-done:
		case <-ctx.Done():
			return Snapshot{}, false
		}
	}
	return r.Snapshot(id)
}

func (r *Registry) SetMaxEventsForTest(maxEvents int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.maxEvents = maxEvents
}

func (r *Registry) TrackedCountForTest() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.jobs)
}

func (r *Registry) CleanupForTest(now time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cleanupLocked(now)
}

func (r *Registry) cleanupLocked(now time.Time) {
	for id, entry := range r.jobs {
		if entry == nil {
			delete(r.jobs, id)
			continue
		}
		if entry.terminal && now.Sub(entry.updatedAt) > r.retention {
			delete(r.jobs, id)
		}
	}
	for len(r.jobs) > r.maxTracked {
		if !r.evictOldestLocked(false) {
			if !r.evictOldestLocked(true) {
				break
			}
		}
	}
}

func (r *Registry) evictOldestLocked(includeActive bool) bool {
	oldestID := ""
	var oldest time.Time
	for id, entry := range r.jobs {
		if entry == nil {
			continue
		}
		if !includeActive && !entry.terminal {
			continue
		}
		if oldestID == "" || entry.updatedAt.Before(oldest) {
			oldestID = id
			oldest = entry.updatedAt
		}
	}
	if oldestID == "" {
		return false
	}
	delete(r.jobs, oldestID)
	return true
}

func snapshotForEntry(entry *entry) Snapshot {
	events := make([]Event, len(entry.events))
	for i, event := range entry.events {
		events[i] = Event{
			Sequence: event.Sequence,
			Type:     event.Type,
			Data:     cloneEventData(event.Data),
		}
	}
	return Snapshot{
		ID:        entry.id,
		Kind:      entry.kind,
		Status:    entry.status,
		Events:    events,
		CreatedAt: entry.createdAt,
		UpdatedAt: entry.updatedAt,
	}
}

func cloneEventData(in map[string]any) map[string]any {
	if len(in) == 0 {
		return map[string]any{}
	}
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(in); err != nil {
		return map[string]any{}
	}
	var out map[string]any
	dec := json.NewDecoder(&buf)
	dec.UseNumber()
	if err := dec.Decode(&out); err != nil {
		return map[string]any{}
	}
	for k, v := range out {
		if num, ok := v.(json.Number); ok {
			if i, err := num.Int64(); err == nil {
				out[k] = i
			}
		}
	}
	return out
}

func newServiceJobID() string {
	var raw [12]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "svc-job"
	}
	return "svc-" + hex.EncodeToString(raw[:])
}
