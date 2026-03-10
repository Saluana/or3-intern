package agent

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

const (
	defaultJobRetention   = 2 * time.Minute
	defaultMaxTrackedJobs = 256
	jobSubscriberBuffer   = 128
)

type JobEvent struct {
	Sequence int64          `json:"sequence"`
	Type     string         `json:"type"`
	Data     map[string]any `json:"data"`
}

type JobSnapshot struct {
	ID        string     `json:"id"`
	Kind      string     `json:"kind"`
	Status    string     `json:"status"`
	Events    []JobEvent `json:"events"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}

type JobRegistry struct {
	mu         sync.Mutex
	jobs       map[string]*jobEntry
	retention  time.Duration
	maxTracked int
}

type jobEntry struct {
	id          string
	kind        string
	status      string
	events      []JobEvent
	subscribers map[int]chan JobEvent
	nextSubID   int
	nextSeq     int64
	cancel      context.CancelFunc
	done        chan struct{}
	terminal    bool
	createdAt   time.Time
	updatedAt   time.Time
}

type JobObserver struct {
	registry *JobRegistry
	jobID    string
}

func NewJobRegistry(retention time.Duration, maxTracked int) *JobRegistry {
	if retention <= 0 {
		retention = defaultJobRetention
	}
	if maxTracked <= 0 {
		maxTracked = defaultMaxTrackedJobs
	}
	return &JobRegistry{
		jobs:       map[string]*jobEntry{},
		retention:  retention,
		maxTracked: maxTracked,
	}
}

func (r *JobRegistry) Register(kind string) JobSnapshot {
	return r.RegisterWithID(newServiceJobID(), kind)
}

func (r *JobRegistry) RegisterWithID(id string, kind string) JobSnapshot {
	now := time.Now()
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cleanupLocked(now)
	entry := &jobEntry{
		id:          id,
		kind:        kind,
		status:      "queued",
		subscribers: map[int]chan JobEvent{},
		done:        make(chan struct{}),
		createdAt:   now,
		updatedAt:   now,
	}
	r.jobs[id] = entry
	return snapshotForEntry(entry)
}

func (r *JobRegistry) AttachCancel(id string, cancel context.CancelFunc) bool {
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

func (r *JobRegistry) Cancel(id string) bool {
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

func (r *JobRegistry) Publish(id string, eventType string, data map[string]any) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	entry := r.jobs[id]
	if entry == nil {
		return false
	}
	now := time.Now()
	entry.updatedAt = now
	if status, ok := data["status"].(string); ok && status != "" {
		entry.status = status
	}
	entry.nextSeq++
	event := JobEvent{Sequence: entry.nextSeq, Type: eventType, Data: cloneEventData(data)}
	entry.events = append(entry.events, event)
	for _, ch := range entry.subscribers {
		select {
		case ch <- event:
		default:
		}
	}
	return true
}

func (r *JobRegistry) Complete(id string, status string, data map[string]any) bool {
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

func (r *JobRegistry) Fail(id string, message string, data map[string]any) bool {
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

func (r *JobRegistry) markTerminal(id string, status string) {
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

func (r *JobRegistry) Subscribe(id string) (JobSnapshot, <-chan JobEvent, func(), bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	entry := r.jobs[id]
	if entry == nil {
		return JobSnapshot{}, nil, nil, false
	}
	snapshot := snapshotForEntry(entry)
	if entry.terminal {
		ch := make(chan JobEvent)
		close(ch)
		return snapshot, ch, func() {}, true
	}
	entry.nextSubID++
	subID := entry.nextSubID
	ch := make(chan JobEvent, jobSubscriberBuffer)
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

func (r *JobRegistry) Snapshot(id string) (JobSnapshot, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	entry := r.jobs[id]
	if entry == nil {
		return JobSnapshot{}, false
	}
	return snapshotForEntry(entry), true
}

func (r *JobRegistry) Wait(ctx context.Context, id string) (JobSnapshot, bool) {
	r.mu.Lock()
	entry := r.jobs[id]
	if entry == nil {
		r.mu.Unlock()
		return JobSnapshot{}, false
	}
	done := entry.done
	terminal := entry.terminal
	r.mu.Unlock()
	if !terminal {
		select {
		case <-done:
		case <-ctx.Done():
			return JobSnapshot{}, false
		}
	}
	return r.Snapshot(id)
}

func (r *JobRegistry) Observer(jobID string) ConversationObserver {
	return JobObserver{registry: r, jobID: jobID}
}

func (o JobObserver) OnTextDelta(_ context.Context, text string) {
	if o.registry == nil || text == "" {
		return
	}
	o.registry.Publish(o.jobID, "text_delta", map[string]any{"content": text})
}

func (o JobObserver) OnToolCall(_ context.Context, name string, arguments string) {
	if o.registry == nil {
		return
	}
	o.registry.Publish(o.jobID, "tool_call", map[string]any{"name": name, "arguments": arguments})
}

func (o JobObserver) OnToolResult(_ context.Context, name string, result string, err error) {
	if o.registry == nil {
		return
	}
	data := map[string]any{"name": name, "result": result}
	if err != nil {
		data["error"] = err.Error()
	}
	o.registry.Publish(o.jobID, "tool_result", data)
}

func (o JobObserver) OnCompletion(_ context.Context, finalText string, streamed bool) {
	if o.registry == nil {
		return
	}
	o.registry.Publish(o.jobID, "assistant", map[string]any{"content": finalText, "streamed": streamed})
}

func (o JobObserver) OnError(_ context.Context, err error) {
	if o.registry == nil || err == nil {
		return
	}
	o.registry.Publish(o.jobID, "runtime_error", map[string]any{"message": err.Error()})
}

func (r *JobRegistry) cleanupLocked(now time.Time) {
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
		oldestID := ""
		var oldest time.Time
		for id, entry := range r.jobs {
			if oldestID == "" || entry.updatedAt.Before(oldest) {
				oldestID = id
				oldest = entry.updatedAt
			}
		}
		if oldestID == "" {
			break
		}
		delete(r.jobs, oldestID)
	}
}

func snapshotForEntry(entry *jobEntry) JobSnapshot {
	events := make([]JobEvent, len(entry.events))
	for i, event := range entry.events {
		events[i] = JobEvent{
			Sequence: event.Sequence,
			Type:     event.Type,
			Data:     cloneEventData(event.Data),
		}
	}
	return JobSnapshot{
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
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
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
