package whatsapp

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"or3-intern/internal/artifacts"
	"or3-intern/internal/bus"
	"or3-intern/internal/config"
	"or3-intern/internal/db"
)

func openWhatsAppTestDB(t *testing.T) *db.DB {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "whatsapp.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return d
}

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
	ch := &Channel{Config: config.WhatsAppBridgeConfig{BridgeURL: "ws" + server.URL[len("http"):], OpenAccess: true}}
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

func TestChannel_StartDeduplicatesRepeatedMessageID(t *testing.T) {
	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatalf("upgrade: %v", err)
		}
		defer conn.Close()
		payload := map[string]any{"type": "message", "id": "m1", "chat": "group1", "from": "123", "text": "hello", "isGroup": true}
		_ = conn.WriteJSON(payload)
		_ = conn.WriteJSON(payload)
		<-time.After(100 * time.Millisecond)
	}))
	defer server.Close()
	b := bus.New(2)
	ch := &Channel{Config: config.WhatsAppBridgeConfig{BridgeURL: "ws" + server.URL[len("http"):], OpenAccess: true}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := ch.Start(ctx, b); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer ch.Stop(context.Background())

	select {
	case <-b.Channel():
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for first inbound whatsapp message")
	}
	select {
	case ev := <-b.Channel():
		t.Fatalf("expected duplicate whatsapp event to be suppressed, got %#v", ev)
	case <-time.After(200 * time.Millisecond):
	}
}

func TestChannel_StartPublishesIsolatedInboundMessageWhenEnabled(t *testing.T) {
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
	ch := &Channel{Config: config.WhatsAppBridgeConfig{BridgeURL: "ws" + server.URL[len("http"):], OpenAccess: true}, IsolatePeers: true}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := ch.Start(ctx, b); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer ch.Stop(context.Background())
	select {
	case ev := <-b.Channel():
		if ev.SessionKey != "whatsapp:group1:123" {
			t.Fatalf("expected isolated session key, got %#v", ev)
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
	ch := &Channel{Config: config.WhatsAppBridgeConfig{BridgeURL: "ws" + server.URL[len("http"):], DefaultTo: "123", OpenAccess: true}}
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

func TestChannel_StartPublishesInboundAttachmentMessage(t *testing.T) {
	d := openWhatsAppTestDB(t)
	store := &artifacts.Store{Dir: t.TempDir(), DB: d}

	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatalf("upgrade: %v", err)
		}
		defer conn.Close()
		_ = conn.WriteJSON(map[string]any{
			"type": "message",
			"id":   "m1",
			"chat": "group1",
			"from": "123",
			"text": "",
			"attachments": []map[string]any{{
				"data_base64": base64.StdEncoding.EncodeToString([]byte("image-data")),
				"filename":    "photo.png",
				"mime":        "image/png",
				"kind":        "image",
				"size_bytes":  10,
			}},
		})
		<-time.After(100 * time.Millisecond)
	}))
	defer server.Close()
	b := bus.New(1)
	ch := &Channel{
		Config:        config.WhatsAppBridgeConfig{BridgeURL: "ws" + server.URL[len("http"):], OpenAccess: true},
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
		if ev.Message != "[image: photo.png]" {
			t.Fatalf("unexpected event message: %#v", ev.Message)
		}
		raw, ok := ev.Meta["attachments"].([]artifacts.Attachment)
		if !ok || len(raw) != 1 {
			t.Fatalf("expected one attachment in meta, got %#v", ev.Meta["attachments"])
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for inbound whatsapp media message")
	}
}

func TestChannel_DeliverWritesSendCommandWithAttachments(t *testing.T) {
	mediaPath := filepath.Join(t.TempDir(), "photo.png")
	if err := os.WriteFile(mediaPath, []byte("image-data"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

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
	ch := &Channel{Config: config.WhatsAppBridgeConfig{BridgeURL: "ws" + server.URL[len("http"):], DefaultTo: "123", OpenAccess: true}, MaxMediaBytes: 1024}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := ch.Start(ctx, bus.New(1)); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer ch.Stop(context.Background())
	if err := ch.Deliver(context.Background(), "", "hello", map[string]any{"media_paths": []string{mediaPath}}); err != nil {
		t.Fatalf("Deliver: %v", err)
	}
	select {
	case msg := <-got:
		attachments, ok := msg["attachments"].([]any)
		if msg["type"] != "send" || msg["to"] != "123" || msg["text"] != "hello" || !ok || len(attachments) != 1 {
			t.Fatalf("unexpected send command: %#v", msg)
		}
		first, ok := attachments[0].(map[string]any)
		if !ok || first["data_base64"] == "" {
			t.Fatalf("expected inline attachment payload, got %#v", attachments[0])
		}
		if _, hasPath := first["path"]; hasPath {
			t.Fatalf("expected outbound bridge payload to omit local path, got %#v", first)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for media send command")
	}
}

func TestChannel_StartRejectsPathOnlyInboundAttachment(t *testing.T) {
	d := openWhatsAppTestDB(t)
	store := &artifacts.Store{Dir: t.TempDir(), DB: d}
	attachmentPath := filepath.Join(t.TempDir(), "photo.png")
	if err := os.WriteFile(attachmentPath, []byte("image-data"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatalf("upgrade: %v", err)
		}
		defer conn.Close()
		_ = conn.WriteJSON(map[string]any{
			"type": "message",
			"id":   "m1",
			"chat": "group1",
			"from": "123",
			"text": "",
			"attachments": []map[string]any{{
				"path":       attachmentPath,
				"filename":   "photo.png",
				"mime":       "image/png",
				"kind":       "image",
				"size_bytes": 10,
			}},
		})
		<-time.After(100 * time.Millisecond)
	}))
	defer server.Close()
	b := bus.New(1)
	ch := &Channel{
		Config:        config.WhatsAppBridgeConfig{BridgeURL: "ws" + server.URL[len("http"):], OpenAccess: true},
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
		if ev.Message != "[image: photo.png - invalid media payload]" {
			t.Fatalf("unexpected event message: %#v", ev.Message)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for inbound whatsapp invalid media marker")
	}
}
