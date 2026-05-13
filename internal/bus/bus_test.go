package bus

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestNew_DefaultBuffer(t *testing.T) {
	b := New(0)
	if b == nil {
		t.Fatal("expected non-nil bus")
	}
	_, unsubscribe := b.Subscribe()
	defer unsubscribe()
	// should accept at least 128 events without blocking
	for i := 0; i < 128; i++ {
		ok := b.Publish(Event{Type: EventUserMessage, Message: "test"})
		if !ok {
			t.Fatalf("expected publish to succeed at i=%d", i)
		}
	}
}

func TestNew_CustomBuffer(t *testing.T) {
	b := New(4)
	if b == nil {
		t.Fatal("expected non-nil bus")
	}
	_, unsubscribe := b.Subscribe()
	defer unsubscribe()
	// first 4 succeed
	for i := 0; i < 4; i++ {
		ok := b.Publish(Event{Type: EventUserMessage})
		if !ok {
			t.Fatalf("expected publish to succeed at i=%d", i)
		}
	}
	// 5th should fail (buffer full)
	ok := b.Publish(Event{Type: EventUserMessage})
	if ok {
		t.Fatal("expected publish to fail on full buffer")
	}
}

func TestPublish_Success(t *testing.T) {
	b := New(10)
	ch, unsubscribe := b.Subscribe()
	defer unsubscribe()
	ev := Event{
		Type:       EventUserMessage,
		SessionKey: "session1",
		Channel:    "cli",
		From:       "user",
		Message:    "hello",
		Meta:       map[string]any{"key": "val"},
	}
	ok := b.Publish(ev)
	if !ok {
		t.Fatal("expected publish to succeed")
	}

	var got Event
	select {
	case got = <-ch:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timeout waiting for published event")
	}
	if got.Type != EventUserMessage {
		t.Errorf("expected type %s, got %s", EventUserMessage, got.Type)
	}
	if got.SessionKey != "session1" {
		t.Errorf("expected session key 'session1', got %q", got.SessionKey)
	}
	if got.Message != "hello" {
		t.Errorf("expected message 'hello', got %q", got.Message)
	}
	if got.Meta["key"] != "val" {
		t.Errorf("expected meta key 'val', got %v", got.Meta["key"])
	}
}

func TestChannel_ReturnsSubscription(t *testing.T) {
	b := New(1)
	ch := b.Channel()
	if ch == nil {
		t.Fatal("expected non-nil channel")
	}
	if !b.Publish(Event{Type: EventUserMessage, Message: "hello"}) {
		t.Fatal("expected publish to succeed")
	}
	select {
	case ev := <-ch:
		if ev.Message != "hello" {
			t.Fatalf("expected published message, got %#v", ev)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timeout waiting for channel event")
	}
}

func TestEventTypes(t *testing.T) {
	cases := []EventType{EventUserMessage, EventCron, EventHeartbeat, EventSystem, EventWebhook, EventFileChange}
	for _, et := range cases {
		b := New(1)
		ch, unsubscribe := b.Subscribe()
		if !b.Publish(Event{Type: et}) {
			unsubscribe()
			t.Fatalf("expected publish to succeed for type %s", et)
		}
		var ev Event
		select {
		case ev = <-ch:
		case <-time.After(100 * time.Millisecond):
			unsubscribe()
			t.Fatalf("timeout waiting for event type %s", et)
		}
		unsubscribe()
		if ev.Type != et {
			t.Errorf("expected type %s, got %s", et, ev.Type)
		}
	}
}

func TestBus_EventFlow(t *testing.T) {
	b := New(10)
	ch, unsubscribe := b.Subscribe()
	defer unsubscribe()

	events := []Event{
		{Type: EventUserMessage, SessionKey: "s1", Message: "msg1"},
		{Type: EventCron, SessionKey: "s2", Message: "cron1"},
		{Type: EventSystem, SessionKey: "s3", Message: "sys1"},
	}

	for _, ev := range events {
		if ok := b.Publish(ev); !ok {
			t.Fatalf("expected publish to succeed for %+v", ev)
		}
	}
	for _, want := range events {
		select {
		case got := <-ch:
			if got.Type != want.Type || got.Message != want.Message {
				t.Errorf("got {%s,%s}, want {%s,%s}", got.Type, got.Message, want.Type, want.Message)
			}
		case <-time.After(100 * time.Millisecond):
			t.Fatal("timeout waiting for event")
		}
	}
}

func TestPublish_Overflow(t *testing.T) {
	b := New(2)
	_, unsubscribe := b.Subscribe()
	defer unsubscribe()
	if ok := b.Publish(Event{Type: EventUserMessage}); !ok {
		t.Fatal("expected first publish to succeed")
	}
	if ok := b.Publish(Event{Type: EventUserMessage}); !ok {
		t.Fatal("expected second publish to succeed")
	}
	// third publish should drop
	ok := b.Publish(Event{Type: EventUserMessage})
	if ok {
		t.Fatal("expected overflow publish to return false")
	}
}

func TestPublish_SlowSubscriberDoesNotFailWhenAnotherSubscriberAccepts(t *testing.T) {
	b := New(1)
	slow, unsubscribeSlow := b.Subscribe()
	defer unsubscribeSlow()
	fast, unsubscribeFast := b.Subscribe()
	defer unsubscribeFast()

	if !b.Publish(Event{Type: EventUserMessage, Message: "first"}) {
		t.Fatal("expected first publish to succeed")
	}
	select {
	case <-fast:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timeout waiting for fast subscriber")
	}
	if !b.Publish(Event{Type: EventUserMessage, Message: "second"}) {
		t.Fatal("expected publish to succeed because fast subscriber accepted it")
	}
	select {
	case ev := <-fast:
		if ev.Message != "second" {
			t.Fatalf("expected second event, got %#v", ev)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timeout waiting for second fast subscriber event")
	}
	select {
	case ev := <-slow:
		if ev.Message != "first" {
			t.Fatalf("expected slow subscriber to retain first event, got %#v", ev)
		}
	default:
		t.Fatal("expected slow subscriber to keep buffered first event")
	}
}

func TestPublish_SubscribeOnlyDoesNotFillLegacyChannel(t *testing.T) {
	b := New(1)
	ch, unsubscribe := b.Subscribe()
	defer unsubscribe()

	if !b.Publish(Event{Type: EventUserMessage, Message: "first"}) {
		t.Fatal("expected first publish to active subscriber to succeed")
	}
	select {
	case <-ch:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timeout waiting for first event")
	}
	if !b.Publish(Event{Type: EventUserMessage, Message: "second"}) {
		t.Fatal("expected second publish to succeed without unused legacy subscriber")
	}
}

func TestChannel_SharedQueueSplitsWork(t *testing.T) {
	b := New(4)
	ch1 := b.Channel()
	ch2 := b.Channel()
	if ch1 != ch2 {
		t.Fatal("expected Channel to return the shared queue")
	}
	if !b.Publish(Event{Type: EventUserMessage, Message: "one"}) {
		t.Fatal("expected first publish to succeed")
	}
	if !b.Publish(Event{Type: EventUserMessage, Message: "two"}) {
		t.Fatal("expected second publish to succeed")
	}

	seen := map[string]bool{}
	for i := 0; i < 2; i++ {
		select {
		case ev := <-ch1:
			seen[ev.Message] = true
		case <-time.After(100 * time.Millisecond):
			t.Fatal("timeout waiting for queued event")
		}
	}
	if !seen["one"] || !seen["two"] {
		t.Fatalf("expected both queued events exactly once, got %#v", seen)
	}
}

func TestPublish_ConcurrentFanOutLoad(t *testing.T) {
	b := New(512)
	ch1, unsub1 := b.Subscribe()
	defer unsub1()
	ch2, unsub2 := b.Subscribe()
	defer unsub2()

	const publishers = 8
	const perPublisher = 50
	const totalEvents = publishers * perPublisher

	counts := make(chan int, 2)
	consume := func(ch <-chan Event) {
		count := 0
		timeout := time.After(2 * time.Second)
		for count < totalEvents {
			select {
			case _, ok := <-ch:
				if !ok {
					counts <- count
					return
				}
				count++
			case <-timeout:
				counts <- count
				return
			}
		}
		counts <- count
	}
	go consume(ch1)
	go consume(ch2)

	var publishFailures atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < publishers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < perPublisher; j++ {
				if !b.Publish(Event{Type: EventUserMessage, Message: "load"}) {
					publishFailures.Add(1)
				}
			}
		}()
	}
	wg.Wait()

	got1 := <-counts
	got2 := <-counts
	if got1 != totalEvents || got2 != totalEvents {
		t.Fatalf("expected both subscribers to receive %d events, got %d and %d", totalEvents, got1, got2)
	}
	if publishFailures.Load() != 0 {
		t.Fatalf("expected zero publish failures, got %d", publishFailures.Load())
	}
}
