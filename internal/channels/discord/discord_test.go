package discord

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"or3-intern/internal/bus"
	"or3-intern/internal/config"
)

func TestChannel_StartReceivesMessage(t *testing.T) {
	upgrader := websocket.Upgrader{}
	identified := make(chan bool, 1)
	wsServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatalf("upgrade: %v", err)
		}
		defer conn.Close()
		_ = conn.WriteJSON(map[string]any{"op": 10, "d": map[string]any{"heartbeat_interval": 10000}})
		_, raw, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("Read identify: %v", err)
		}
		if strings.Contains(string(raw), `"op":2`) {
			identified <- true
		}
		_ = conn.WriteJSON(map[string]any{"op": 0, "t": "READY", "d": map[string]any{"user": map[string]any{"id": "B1"}}})
		_ = conn.WriteJSON(map[string]any{"op": 0, "t": "MESSAGE_CREATE", "d": map[string]any{"id": "m1", "channel_id": "C1", "content": "<@B1> hello", "author": map[string]any{"id": "U1", "bot": false}, "mentions": []map[string]any{{"id": "B1"}}}})
		<-time.After(100 * time.Millisecond)
	}))
	defer wsServer.Close()
	b := bus.New(1)
	ch := &Channel{Config: config.DiscordChannelConfig{Token: "token", GatewayURL: "ws" + strings.TrimPrefix(wsServer.URL, "http"), RequireMention: true}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := ch.Start(ctx, b); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer ch.Stop(context.Background())
	select {
	case <-identified:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for identify")
	}
	select {
	case ev := <-b.Channel():
		if ev.Channel != "discord" || ev.Message != "hello" {
			t.Fatalf("unexpected event: %#v", ev)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for discord event")
	}
}

func TestChannel_DeliverPostsMessage(t *testing.T) {
	var got map[string]any
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/channels/C1/messages" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"1"}`))
	}))
	defer apiServer.Close()
	ch := &Channel{Config: config.DiscordChannelConfig{Token: "token", APIBase: apiServer.URL, DefaultChannelID: "C1"}}
	if err := ch.Deliver(context.Background(), "", "hello", map[string]any{"message_reference": "m1"}); err != nil {
		t.Fatalf("Deliver: %v", err)
	}
	if got["content"] != "hello" {
		t.Fatalf("unexpected payload: %#v", got)
	}
}
