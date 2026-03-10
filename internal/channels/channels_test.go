package channels

import (
	"context"
	"reflect"
	"testing"

	"or3-intern/internal/bus"
)

type testChannel struct {
	name         string
	startedCount int
	stoppedCount int
	delivered    []string
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

func TestReplyMeta_PreservesThreadingFieldsOnly(t *testing.T) {
	meta := map[string]any{
		"channel_id":         "C1",
		MetaThreadTS:          "123.45",
		MetaReplyToMessageID:  int64(44),
		MetaMessageReference:  "m-1",
		"attachments":        []string{"artifact"},
		MetaMediaPaths:        []string{"/tmp/file.txt"},
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
