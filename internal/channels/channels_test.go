package channels

import (
	"context"
	"reflect"
	"strconv"
	"testing"
	"time"

	"or3-intern/internal/bus"
)

type testChannel struct {
	name         string
	startedCount int
	stoppedCount int
	delivered    []string
}

type blockingStartChannel struct {
	name    string
	started chan struct{}
	stopped chan struct{}
}

func (c *blockingStartChannel) Name() string { return c.name }
func (c *blockingStartChannel) Start(ctx context.Context, eventBus *bus.Bus) error {
	_ = eventBus
	close(c.started)
	<-ctx.Done()
	return ctx.Err()
}
func (c *blockingStartChannel) Stop(ctx context.Context) error {
	_ = ctx
	close(c.stopped)
	return nil
}
func (c *blockingStartChannel) Deliver(ctx context.Context, to, text string, meta map[string]any) error {
	_ = ctx
	_ = to
	_ = text
	_ = meta
	return nil
}

func (c *testChannel) Name() string { return c.name }
func (c *testChannel) Start(ctx context.Context, eventBus *bus.Bus) error {
	_ = ctx
	_ = eventBus
	c.startedCount++
	return nil
}
func (c *testChannel) Stop(ctx context.Context) error {
	_ = ctx
	c.stoppedCount++
	return nil
}
func (c *testChannel) Deliver(ctx context.Context, to, text string, meta map[string]any) error {
	_ = ctx
	_ = meta
	c.delivered = append(c.delivered, to+":"+text)
	return nil
}

func TestManager_RegisterStartDeliverStop(t *testing.T) {
	m := NewManager()
	ch := &testChannel{name: "telegram"}
	if err := m.Register(ch); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := m.StartAll(context.Background(), bus.New(1)); err != nil {
		t.Fatalf("StartAll: %v", err)
	}
	if ch.startedCount != 1 {
		t.Fatalf("expected start count 1, got %d", ch.startedCount)
	}
	if err := m.Deliver(context.Background(), "telegram", "123", "hello"); err != nil {
		t.Fatalf("Deliver: %v", err)
	}
	if len(ch.delivered) != 1 || ch.delivered[0] != "123:hello" {
		t.Fatalf("unexpected delivered messages: %#v", ch.delivered)
	}
	if err := m.StopAll(context.Background()); err != nil {
		t.Fatalf("StopAll: %v", err)
	}
	if ch.stoppedCount != 1 {
		t.Fatalf("expected stop count 1, got %d", ch.stoppedCount)
	}
}

func TestManager_RejectsDuplicateNames(t *testing.T) {
	m := NewManager()
	if err := m.Register(&testChannel{name: "slack"}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := m.Register(&testChannel{name: "slack"}); err == nil {
		t.Fatal("expected duplicate registration error")
	}
}

func TestManager_StopCancelsAnInitialStart(t *testing.T) {
	m := NewManager()
	ch := &blockingStartChannel{name: "slack", started: make(chan struct{}), stopped: make(chan struct{})}
	if err := m.Register(ch); err != nil {
		t.Fatalf("Register: %v", err)
	}
	startErr := make(chan error, 1)
	go func() { startErr <- m.Start(context.Background(), "SLACK", bus.New(1)) }()
	select {
	case <-ch.started:
	case <-time.After(time.Second):
		t.Fatal("channel start did not begin")
	}
	stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := m.Stop(stopCtx, "slack"); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	select {
	case <-ch.stopped:
	case <-time.After(time.Second):
		t.Fatal("channel stop was not called")
	}
	select {
	case err := <-startErr:
		if err == nil {
			t.Fatal("expected canceled start error")
		}
	case <-time.After(time.Second):
		t.Fatal("start did not finish after stop")
	}
	if status := m.ConnectionStatuses()["slack"]; status != "stopped" {
		t.Fatalf("expected stopped status after canceled start, got %q", status)
	}
}

func TestReplyMeta_PreservesThreadingFieldsOnly(t *testing.T) {
	meta := map[string]any{
		"channel_id":         "C1",
		MetaThreadTS:         "123.45",
		MetaReplyToMessageID: int64(44),
		MetaMessageReference: "m-1",
		"attachments":        []string{"artifact"},
		MetaMediaPaths:       []string{"/tmp/file.txt"},
	}
	got := ReplyMeta(meta)
	want := map[string]any{
		MetaThreadTS:         "123.45",
		MetaReplyToMessageID: int64(44),
		MetaMessageReference: "m-1",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expected reply meta %#v, got %#v", want, got)
	}
}

func TestReplyMeta_IgnoresEmptyValues(t *testing.T) {
	got := ReplyMeta(map[string]any{
		MetaThreadTS:         " ",
		MetaReplyToMessageID: int64(0),
	})
	if got != nil {
		t.Fatalf("expected nil reply meta, got %#v", got)
	}
}

func TestIngressDeduplicator_BlocksDuplicates(t *testing.T) {
	d := NewIngressDeduplicator(time.Minute)
	if d.IsDuplicate("msg-1") {
		t.Fatal("first call should not be a duplicate")
	}
	if !d.IsDuplicate("msg-1") {
		t.Fatal("second call with same key should be a duplicate")
	}
}

func TestIngressDeduplicator_AllowsAfterTTLExpiry(t *testing.T) {
	d := NewIngressDeduplicator(50 * time.Millisecond)
	if d.IsDuplicate("msg-a") {
		t.Fatal("first call for msg-a should not be a duplicate")
	}
	if d.IsDuplicate("msg-b") {
		t.Fatal("msg-b should not be a duplicate of msg-a")
	}
	time.Sleep(60 * time.Millisecond)
	if d.IsDuplicate("msg-a") {
		t.Fatal("msg-a should not be a duplicate after TTL expiry")
	}
}

func TestIngressDeduplicator_DefaultTTL(t *testing.T) {
	d := NewIngressDeduplicator(0)
	if d.ttl != defaultDeduplicatorTTL {
		t.Fatalf("expected default TTL %v, got %v", defaultDeduplicatorTTL, d.ttl)
	}
}

func TestIngressDeduplicator_BoundsUniqueKeys(t *testing.T) {
	d := NewIngressDeduplicator(time.Minute)
	for i := 0; i < defaultDeduplicatorMaxEntries+32; i++ {
		if d.IsDuplicate("message-" + strconv.Itoa(i)) {
			t.Fatalf("unexpected duplicate at %d", i)
		}
	}
	d.mu.Lock()
	count := len(d.seen)
	d.mu.Unlock()
	if count > defaultDeduplicatorMaxEntries {
		t.Fatalf("expected at most %d cached keys, got %d", defaultDeduplicatorMaxEntries, count)
	}
}
