package slack

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

func TestChannel_StartReceivesEventAndAcks(t *testing.T) {
	upgrader := websocket.Upgrader{}
	ackSeen := make(chan string, 1)
	wsServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatalf("upgrade: %v", err)
		}
		defer conn.Close()
		_ = conn.WriteJSON(map[string]any{
			"envelope_id": "env1",
			"type":        "events_api",
			"payload": map[string]any{
				"authorizations": []map[string]any{{"user_id": "B123"}},
				"event": map[string]any{"type": "message", "text": "<@B123> hello", "user": "U1", "channel": "C1"},
			},
		})
		var ack map[string]any
		if err := conn.ReadJSON(&ack); err != nil {
			t.Fatalf("ReadJSON ack: %v", err)
		}
		ackSeen <- ack["envelope_id"].(string)
	}))
	defer wsServer.Close()
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/apps.connections.open" {
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "url": "ws" + strings.TrimPrefix(wsServer.URL, "http")})
			return
		}
		t.Fatalf("unexpected api path: %s", r.URL.Path)
	}))
	defer apiServer.Close()

	b := bus.New(1)
	ch := &Channel{Config: config.SlackChannelConfig{AppToken: "app", BotToken: "bot", APIBase: apiServer.URL, RequireMention: true}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := ch.Start(ctx, b); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer ch.Stop(context.Background())
	select {
	case env := <-ackSeen:
		if env != "env1" {
			t.Fatalf("unexpected ack envelope: %s", env)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for slack ack")
	}
	select {
	case ev := <-b.Channel():
		if ev.Channel != "slack" || ev.Message != "hello" {
			t.Fatalf("unexpected event: %#v", ev)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for slack event")
	}
}

func TestChannel_DeliverPostsMessage(t *testing.T) {
	var got map[string]any
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat.postMessage" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))
	defer apiServer.Close()
	ch := &Channel{Config: config.SlackChannelConfig{BotToken: "bot", APIBase: apiServer.URL, DefaultChannelID: "C1"}}
	if err := ch.Deliver(context.Background(), "", "hello", map[string]any{"thread_ts": "123.45"}); err != nil {
		t.Fatalf("Deliver: %v", err)
	}
	if got["channel"] != "C1" || got["text"] != "hello" || got["thread_ts"] != "123.45" {
		t.Fatalf("unexpected payload: %#v", got)
	}
}
