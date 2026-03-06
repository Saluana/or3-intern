package telegram

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"or3-intern/internal/bus"
	"or3-intern/internal/config"
)

func TestChannel_FetchUpdatesPublishesMessage(t *testing.T) {
	var mu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok": true,
			"result": []map[string]any{{
				"update_id": 1,
				"message": map[string]any{
					"message_id": 99,
					"text":       "hello telegram",
					"chat":       map[string]any{"id": 123},
					"from":       map[string]any{"id": 456, "username": "alice"},
				},
			}},
		})
		mu.Lock()
		mu.Unlock()
	}))
	defer server.Close()

	ch := &Channel{Config: config.TelegramChannelConfig{Token: "token", APIBase: server.URL, PollSeconds: 1}}
	b := bus.New(1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := ch.Start(ctx, b); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer ch.Stop(context.Background())
	select {
	case ev := <-b.Channel():
		if ev.Channel != "telegram" || ev.SessionKey != "telegram:123" || ev.Message != "hello telegram" {
			t.Fatalf("unexpected event: %#v", ev)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for telegram event")
	}
}

func TestChannel_DeliverSendsMessage(t *testing.T) {
	var got map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/bottoken/sendMessage" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": map[string]any{"message_id": 1}})
	}))
	defer server.Close()

	ch := &Channel{Config: config.TelegramChannelConfig{Token: "token", APIBase: server.URL, DefaultChatID: "123"}}
	if err := ch.Deliver(context.Background(), "", "hello", map[string]any{"reply_to_message_id": int64(44)}); err != nil {
		t.Fatalf("Deliver: %v", err)
	}
	if got["chat_id"] != "123" || got["text"] != "hello" {
		t.Fatalf("unexpected payload: %#v", got)
	}
}
