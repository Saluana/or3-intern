package telegram

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"or3-intern/internal/artifacts"
	"or3-intern/internal/bus"
	"or3-intern/internal/config"
	"or3-intern/internal/db"
)

func openTelegramTestDB(t *testing.T) *db.DB {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "telegram.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return d
}

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

	ch := &Channel{Config: config.TelegramChannelConfig{Token: "token", APIBase: server.URL, PollSeconds: 1, OpenAccess: true}}
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

	ch := &Channel{Config: config.TelegramChannelConfig{Token: "token", APIBase: server.URL, DefaultChatID: "123", OpenAccess: true}}
	if err := ch.Deliver(context.Background(), "", "hello", map[string]any{"reply_to_message_id": int64(44)}); err != nil {
		t.Fatalf("Deliver: %v", err)
	}
	if got["chat_id"] != "123" || got["text"] != "hello" {
		t.Fatalf("unexpected payload: %#v", got)
	}
}

func TestChannel_FetchUpdatesPublishesIsolatedGroupMessageWhenEnabled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok": true,
			"result": []map[string]any{{
				"update_id": 1,
				"message": map[string]any{
					"message_id": 99,
					"text":       "hello group",
					"chat":       map[string]any{"id": 123, "type": "group"},
					"from":       map[string]any{"id": 456, "username": "alice"},
				},
			}},
		})
	}))
	defer server.Close()
	ch := &Channel{Config: config.TelegramChannelConfig{Token: "token", APIBase: server.URL, PollSeconds: 1, OpenAccess: true}, IsolatePeers: true}
	b := bus.New(1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := ch.Start(ctx, b); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer ch.Stop(context.Background())
	select {
	case ev := <-b.Channel():
		if ev.SessionKey != "telegram:123:456" {
			t.Fatalf("expected isolated session key, got %#v", ev)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for telegram event")
	}
}

func TestChannel_FetchUpdatesPublishesPhotoAttachment(t *testing.T) {
	d := openTelegramTestDB(t)
	store := &artifacts.Store{Dir: t.TempDir(), DB: d}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/bottoken/getUpdates":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok": true,
				"result": []map[string]any{{
					"update_id": 2,
					"message": map[string]any{
						"message_id": 100,
						"caption":    "see image",
						"chat":       map[string]any{"id": 123},
						"from":       map[string]any{"id": 456, "username": "alice"},
						"photo": []map[string]any{{
							"file_id":   "photo-1",
							"file_size": 10,
						}},
					},
				}},
			})
		case "/bottoken/getFile":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok":     true,
				"result": map[string]any{"file_path": "photos/p1.jpg", "file_size": 10},
			})
		case "/file/bottoken/photos/p1.jpg":
			_, _ = w.Write([]byte("image-data"))
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	ch := &Channel{
		Config:        config.TelegramChannelConfig{Token: "token", APIBase: server.URL, PollSeconds: 1, OpenAccess: true},
		Artifacts:     store,
		MaxMediaBytes: 1024,
	}
	b := bus.New(1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := ch.Start(ctx, b); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer ch.Stop(context.Background())

	select {
	case ev := <-b.Channel():
		if ev.Message != "see image\n[image: photo.jpg]" {
			t.Fatalf("unexpected message: %#v", ev.Message)
		}
		raw, ok := ev.Meta["attachments"].([]artifacts.Attachment)
		if !ok || len(raw) != 1 {
			t.Fatalf("expected one attachment in meta, got %#v", ev.Meta["attachments"])
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for telegram media event")
	}
}

func TestChannel_DeliverSendsPhotoUpload(t *testing.T) {
	mediaPath := filepath.Join(t.TempDir(), "photo.png")
	if err := os.WriteFile(mediaPath, []byte("image-data"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/bottoken/sendPhoto" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Fatalf("ParseMultipartForm: %v", err)
		}
		if got := r.FormValue("chat_id"); got != "123" {
			t.Fatalf("expected chat_id 123, got %q", got)
		}
		if got := r.FormValue("caption"); got != "hello" {
			t.Fatalf("expected caption hello, got %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": map[string]any{"message_id": 1}})
	}))
	defer server.Close()

	ch := &Channel{Config: config.TelegramChannelConfig{Token: "token", APIBase: server.URL, DefaultChatID: "123", OpenAccess: true}, MaxMediaBytes: 1024}
	if err := ch.Deliver(context.Background(), "", "hello", map[string]any{"media_paths": []string{mediaPath}}); err != nil {
		t.Fatalf("Deliver: %v", err)
	}
}
