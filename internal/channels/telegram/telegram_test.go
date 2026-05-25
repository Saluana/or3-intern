package telegram

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

	"or3-intern/internal/approval"
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
	}))
	defer server.Close()

	ch := &Channel{Config: config.TelegramChannelConfig{Token: "token", APIBase: server.URL, PollSeconds: 1, OpenAccess: true}}
	b := bus.New(1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := ch.Start(ctx, b); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = ch.Stop(context.Background()) }()
	select {
	case ev := <-b.Channel():
		if ev.Channel != "telegram" || ev.SessionKey != "telegram:123" || ev.Message != "hello telegram" {
			t.Fatalf("unexpected event: %#v", ev)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for telegram event")
	}
}

func TestChannel_FetchUpdatesDeduplicatesRepeatedMessageID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok": true,
			"result": []map[string]any{
				{
					"update_id": 1,
					"message": map[string]any{
						"message_id": 99,
						"text":       "hello telegram",
						"chat":       map[string]any{"id": 123},
						"from":       map[string]any{"id": 456, "username": "alice"},
					},
				},
				{
					"update_id": 2,
					"message": map[string]any{
						"message_id": 99,
						"text":       "hello telegram",
						"chat":       map[string]any{"id": 123},
						"from":       map[string]any{"id": 456, "username": "alice"},
					},
				},
			},
		})
	}))
	defer server.Close()

	ch := &Channel{Config: config.TelegramChannelConfig{Token: "token", APIBase: server.URL, PollSeconds: 1, OpenAccess: true}}
	b := bus.New(2)
	if err := ch.fetchUpdates(context.Background(), b); err != nil {
		t.Fatalf("fetchUpdates: %v", err)
	}
	select {
	case <-b.Channel():
	default:
		t.Fatal("expected first telegram event")
	}
	select {
	case ev := <-b.Channel():
		t.Fatalf("expected duplicate telegram message to be suppressed, got %#v", ev)
	default:
	}
}

func TestChannel_FetchUpdatesCachesRecentChatsBeforeAllowlist(t *testing.T) {
	clearRecentChatsForTest()
	t.Cleanup(clearRecentChatsForTest)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok": true,
			"result": []map[string]any{{
				"update_id": 1,
				"message": map[string]any{
					"message_id": 99,
					"date":       200,
					"text":       "please add me",
					"chat":       map[string]any{"id": 123, "type": "private", "first_name": "Brendon"},
					"from":       map[string]any{"id": 456, "username": "alice"},
				},
			}},
		})
	}))
	defer server.Close()

	ch := &Channel{Config: config.TelegramChannelConfig{Token: "token", APIBase: server.URL, PollSeconds: 1}}
	b := bus.New(1)
	if err := ch.fetchUpdates(context.Background(), b); err != nil {
		t.Fatalf("fetchUpdates: %v", err)
	}
	select {
	case ev := <-b.Channel():
		t.Fatalf("expected untrusted chat not to publish event, got %#v", ev)
	default:
	}
	recent := RecentChats(server.URL, "token", 20)
	if len(recent) != 1 || recent[0].ID != "123" || recent[0].DisplayName != "Brendon" || recent[0].LastMessageText != "please add me" {
		t.Fatalf("unexpected recent chats: %#v", recent)
	}
}

func TestChannel_AllowedChatSupportsPairingPolicy(t *testing.T) {
	broker := &approval.Broker{DB: openTelegramTestDB(t)}
	if _, _, err := broker.RotateDeviceToken(context.Background(), "telegram:123", approval.RoleOperator, "Telegram Chat", map[string]any{"channel": "telegram", "identity": "123"}); err != nil {
		t.Fatalf("RotateDeviceToken: %v", err)
	}
	ch := &Channel{
		Config:         config.TelegramChannelConfig{InboundPolicy: config.InboundPolicyPairing},
		ApprovalBroker: broker,
	}
	if !ch.allowedChat(context.Background(), "123") {
		t.Fatal("expected paired telegram chat to be allowed")
	}
	if ch.allowedChat(context.Background(), "999") {
		t.Fatal("expected unknown telegram chat to be denied")
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

func TestChannel_DeliverAddsInlineApprovalButtons(t *testing.T) {
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
	text := "Approval is needed.\n\nReply `/approve 42` to continue or `/deny 42` to stop."
	if err := ch.Deliver(context.Background(), "", text, nil); err != nil {
		t.Fatalf("Deliver: %v", err)
	}
	markup, ok := got["reply_markup"].(map[string]any)
	if !ok {
		t.Fatalf("expected reply markup, got %#v", got)
	}
	keyboard, ok := markup["inline_keyboard"].([]any)
	if !ok || len(keyboard) != 1 {
		t.Fatalf("expected inline keyboard, got %#v", markup)
	}
	row, ok := keyboard[0].([]any)
	if !ok || len(row) != 2 {
		t.Fatalf("expected two approval buttons, got %#v", keyboard)
	}
	approve, ok := row[0].(map[string]any)
	if !ok || approve["callback_data"] != "or3:approval:approve:42" {
		t.Fatalf("unexpected approve button: %#v", row[0])
	}
	deny, ok := row[1].(map[string]any)
	if !ok || deny["callback_data"] != "or3:approval:deny:42" {
		t.Fatalf("unexpected deny button: %#v", row[1])
	}
}

func TestChannel_DeliverSurfacesRateLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":          false,
			"description": "Too Many Requests: retry later",
			"parameters":  map[string]any{"retry_after": 2},
		})
	}))
	defer server.Close()

	ch := &Channel{Config: config.TelegramChannelConfig{Token: "token", APIBase: server.URL, DefaultChatID: "123", OpenAccess: true}}
	err := ch.Deliver(context.Background(), "", "hello", nil)
	if err == nil || !strings.Contains(err.Error(), "telegram rate limited") || !strings.Contains(err.Error(), "2s") {
		t.Fatalf("expected telegram rate-limit error, got %v", err)
	}
}

func TestChannel_StartTypingSendsChatAction(t *testing.T) {
	got := make(chan map[string]any, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/bottoken/sendChatAction" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		got <- body
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": true})
	}))
	defer server.Close()

	ch := &Channel{Config: config.TelegramChannelConfig{Token: "token", APIBase: server.URL, DefaultChatID: "fallback", OpenAccess: true}}
	stop := ch.StartTyping(context.Background(), "", map[string]any{"chat_id": "123"})
	defer stop()

	select {
	case body := <-got:
		if body["chat_id"] != "123" || body["action"] != "typing" {
			t.Fatalf("unexpected payload: %#v", body)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for typing chat action")
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
	defer func() { _ = ch.Stop(context.Background()) }()
	select {
	case ev := <-b.Channel():
		if ev.SessionKey != "telegram:123:456" {
			t.Fatalf("expected isolated session key, got %#v", ev)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for telegram event")
	}
}

func TestChannel_FetchUpdatesPublishesApprovalCallback(t *testing.T) {
	requests := make(chan string, 4)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests <- r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/bottoken/getUpdates":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok": true,
				"result": []map[string]any{{
					"update_id": 5,
					"callback_query": map[string]any{
						"id":   "cb-1",
						"data": "or3:approval:approve:42",
						"from": map[string]any{"id": 456, "username": "alice"},
						"message": map[string]any{
							"message_id": 99,
							"chat":       map[string]any{"id": 123, "type": "private"},
						},
					},
				}},
			})
		case "/bottoken/answerCallbackQuery":
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": true})
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	ch := &Channel{Config: config.TelegramChannelConfig{Token: "token", APIBase: server.URL, PollSeconds: 1, OpenAccess: true}}
	b := bus.New(1)
	if err := ch.fetchUpdates(context.Background(), b); err != nil {
		t.Fatalf("fetchUpdates: %v", err)
	}
	select {
	case ev := <-b.Channel():
		if ev.Channel != "telegram" || ev.SessionKey != "telegram:123" || ev.From != "456" || ev.Message != "/approve 42" {
			t.Fatalf("unexpected callback event: %#v", ev)
		}
		if ev.Meta["callback_query_id"] != "cb-1" {
			t.Fatalf("expected callback metadata, got %#v", ev.Meta)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for callback event")
	}

	var sawAnswer bool
	requestCount := len(requests)
	for i := 0; i < requestCount; i++ {
		if <-requests == "/bottoken/answerCallbackQuery" {
			sawAnswer = true
		}
	}
	if !sawAnswer {
		t.Fatal("expected callback query answer")
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
	defer func() { _ = ch.Stop(context.Background()) }()

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
