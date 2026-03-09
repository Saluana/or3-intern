package discord

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"or3-intern/internal/artifacts"
	"or3-intern/internal/bus"
	"or3-intern/internal/config"
	"or3-intern/internal/db"
)

func openDiscordTestDB(t *testing.T) *db.DB {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "discord.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return d
}

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
	ch := &Channel{Config: config.DiscordChannelConfig{Token: "token", GatewayURL: "ws" + strings.TrimPrefix(wsServer.URL, "http"), RequireMention: true, OpenAccess: true}}
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
		if ev.SessionKey != "discord:C1" {
			t.Fatalf("expected channel-scoped session by default, got %#v", ev)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for discord event")
	}
}

func TestChannel_StartReceivesIsolatedSessionPerUserWhenEnabled(t *testing.T) {
	upgrader := websocket.Upgrader{}
	wsServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatalf("upgrade: %v", err)
		}
		defer conn.Close()
		_ = conn.WriteJSON(map[string]any{"op": 10, "d": map[string]any{"heartbeat_interval": 10000}})
		_, _, _ = conn.ReadMessage()
		_ = conn.WriteJSON(map[string]any{"op": 0, "t": "READY", "d": map[string]any{"user": map[string]any{"id": "B1"}}})
		_ = conn.WriteJSON(map[string]any{"op": 0, "t": "MESSAGE_CREATE", "d": map[string]any{"id": "m1", "channel_id": "C1", "content": "<@B1> hello", "author": map[string]any{"id": "U1", "bot": false}, "mentions": []map[string]any{{"id": "B1"}}}})
		<-time.After(100 * time.Millisecond)
	}))
	defer wsServer.Close()
	b := bus.New(1)
	ch := &Channel{Config: config.DiscordChannelConfig{Token: "token", GatewayURL: "ws" + strings.TrimPrefix(wsServer.URL, "http"), RequireMention: true, OpenAccess: true}, IsolatePeers: true}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := ch.Start(ctx, b); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer ch.Stop(context.Background())
	select {
	case ev := <-b.Channel():
		if ev.SessionKey != "discord:C1:U1" {
			t.Fatalf("expected isolated session key, got %#v", ev)
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
	ch := &Channel{Config: config.DiscordChannelConfig{Token: "token", APIBase: apiServer.URL, DefaultChannelID: "C1", OpenAccess: true}}
	if err := ch.Deliver(context.Background(), "", "hello", map[string]any{"message_reference": "m1"}); err != nil {
		t.Fatalf("Deliver: %v", err)
	}
	if got["content"] != "hello" {
		t.Fatalf("unexpected payload: %#v", got)
	}
}

func TestChannel_StartReceivesAttachmentMessage(t *testing.T) {
	d := openDiscordTestDB(t)
	store := &artifacts.Store{Dir: t.TempDir(), DB: d}
	fileServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("image-data"))
	}))
	defer fileServer.Close()

	upgrader := websocket.Upgrader{}
	wsServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatalf("upgrade: %v", err)
		}
		defer conn.Close()
		_ = conn.WriteJSON(map[string]any{"op": 10, "d": map[string]any{"heartbeat_interval": 10000}})
		_, _, _ = conn.ReadMessage()
		_ = conn.WriteJSON(map[string]any{"op": 0, "t": "READY", "d": map[string]any{"user": map[string]any{"id": "B1"}}})
		_ = conn.WriteJSON(map[string]any{"op": 0, "t": "MESSAGE_CREATE", "d": map[string]any{
			"id":         "m1",
			"channel_id": "C1",
			"content":    "",
			"author":     map[string]any{"id": "U1", "bot": false},
			"mentions":   []map[string]any{},
			"attachments": []map[string]any{{
				"url":          fileServer.URL + "/file.png",
				"filename":     "file.png",
				"content_type": "image/png",
				"size":         10,
			}},
		}})
		<-time.After(100 * time.Millisecond)
	}))
	defer wsServer.Close()

	b := bus.New(1)
	ch := &Channel{
		Config:        config.DiscordChannelConfig{Token: "token", GatewayURL: "ws" + strings.TrimPrefix(wsServer.URL, "http"), RequireMention: false, OpenAccess: true},
		Artifacts:     store,
		MaxMediaBytes: 1024,
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := ch.Start(ctx, b); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer ch.Stop(context.Background())

	select {
	case ev := <-b.Channel():
		if ev.Message != "[image: file.png]" {
			t.Fatalf("unexpected event message: %#v", ev.Message)
		}
		raw, ok := ev.Meta["attachments"].([]artifacts.Attachment)
		if !ok || len(raw) != 1 {
			t.Fatalf("expected one attachment in meta, got %#v", ev.Meta["attachments"])
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for discord media event")
	}
}

func TestChannel_DeliverPostsMultipartWithMedia(t *testing.T) {
	mediaPath := filepath.Join(t.TempDir(), "image.png")
	if err := os.WriteFile(mediaPath, []byte("image-data"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/channels/C1/messages" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if !strings.Contains(r.Header.Get("Content-Type"), "multipart/form-data") {
			t.Fatalf("expected multipart request, got %s", r.Header.Get("Content-Type"))
		}
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Fatalf("ParseMultipartForm: %v", err)
		}
		if r.FormValue("payload_json") == "" {
			t.Fatal("expected payload_json field")
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"1"}`))
	}))
	defer apiServer.Close()

	ch := &Channel{Config: config.DiscordChannelConfig{Token: "token", APIBase: apiServer.URL, DefaultChannelID: "C1", OpenAccess: true}, MaxMediaBytes: 1024}
	if err := ch.Deliver(context.Background(), "", "hello", map[string]any{"media_paths": []string{mediaPath}}); err != nil {
		t.Fatalf("Deliver: %v", err)
	}
}
