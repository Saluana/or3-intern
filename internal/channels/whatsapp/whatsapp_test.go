package whatsapp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"or3-intern/internal/bus"
	"or3-intern/internal/config"
)

func TestChannel_StartPublishesInboundMessage(t *testing.T) {
	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatalf("upgrade: %v", err)
		}
		defer conn.Close()
		_ = conn.WriteJSON(map[string]any{"type": "message", "id": "m1", "chat": "group1", "from": "123", "text": "hello", "isGroup": true})
		<-time.After(100 * time.Millisecond)
	}))
	defer server.Close()
	b := bus.New(1)
	ch := &Channel{Config: config.WhatsAppBridgeConfig{BridgeURL: "ws" + server.URL[len("http"):]} }
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := ch.Start(ctx, b); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer ch.Stop(context.Background())
	select {
	case ev := <-b.Channel():
		if ev.Channel != "whatsapp" || ev.SessionKey != "whatsapp:group1" || ev.Message != "hello" {
			t.Fatalf("unexpected event: %#v", ev)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for inbound whatsapp message")
	}
}

func TestChannel_DeliverWritesSendCommand(t *testing.T) {
	upgrader := websocket.Upgrader{}
	got := make(chan map[string]any, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatalf("upgrade: %v", err)
		}
		defer conn.Close()
		var msg map[string]any
		if err := conn.ReadJSON(&msg); err != nil {
			t.Fatalf("ReadJSON: %v", err)
		}
		got <- msg
	}))
	defer server.Close()
	ch := &Channel{Config: config.WhatsAppBridgeConfig{BridgeURL: "ws" + server.URL[len("http"):], DefaultTo: "123"}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := ch.Start(ctx, bus.New(1)); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer ch.Stop(context.Background())
	if err := ch.Deliver(context.Background(), "", "hello", nil); err != nil {
		t.Fatalf("Deliver: %v", err)
	}
	select {
	case msg := <-got:
		if msg["type"] != "send" || msg["to"] != "123" || msg["text"] != "hello" {
			t.Fatalf("unexpected send command: %#v", msg)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for send command")
	}
}
