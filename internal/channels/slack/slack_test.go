package slack

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

func openSlackTestDB(t *testing.T) *db.DB {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "slack.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return d
}

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
				"event":          map[string]any{"type": "message", "text": "<@B123> hello", "user": "U1", "channel": "C1"},
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
	ch := &Channel{Config: config.SlackChannelConfig{AppToken: "app", BotToken: "bot", APIBase: apiServer.URL, RequireMention: true, OpenAccess: true}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := ch.Start(ctx, b); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = ch.Stop(context.Background()) }()
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
		if ev.SessionKey != "slack:C1" {
			t.Fatalf("expected channel-scoped session by default, got %#v", ev)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for slack event")
	}
}

func TestChannel_StartDeduplicatesRepeatedEnvelope(t *testing.T) {
	upgrader := websocket.Upgrader{}
	wsServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatalf("upgrade: %v", err)
		}
		defer conn.Close()
		payload := map[string]any{
			"envelope_id": "env1",
			"type":        "events_api",
			"payload": map[string]any{
				"authorizations": []map[string]any{{"user_id": "B123"}},
				"event":          map[string]any{"type": "message", "text": "<@B123> hello", "user": "U1", "channel": "C1"},
			},
		}
		_ = conn.WriteJSON(payload)
		var ack map[string]any
		_ = conn.ReadJSON(&ack)
		_ = conn.WriteJSON(payload)
		_ = conn.ReadJSON(&ack)
		<-time.After(100 * time.Millisecond)
	}))
	defer wsServer.Close()
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "url": "ws" + strings.TrimPrefix(wsServer.URL, "http")})
	}))
	defer apiServer.Close()

	b := bus.New(2)
	ch := &Channel{Config: config.SlackChannelConfig{AppToken: "app", BotToken: "bot", APIBase: apiServer.URL, RequireMention: true, OpenAccess: true}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := ch.Start(ctx, b); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = ch.Stop(context.Background()) }()

	select {
	case <-b.Channel():
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for first slack event")
	}
	select {
	case ev := <-b.Channel():
		t.Fatalf("expected duplicate slack event to be suppressed, got %#v", ev)
	case <-time.After(200 * time.Millisecond):
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
		_ = conn.WriteJSON(map[string]any{
			"envelope_id": "env1",
			"type":        "events_api",
			"payload": map[string]any{
				"authorizations": []map[string]any{{"user_id": "B123"}},
				"event":          map[string]any{"type": "message", "text": "<@B123> hello", "user": "U1", "channel": "C1"},
			},
		})
		var ack map[string]any
		_ = conn.ReadJSON(&ack)
	}))
	defer wsServer.Close()
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "url": "ws" + strings.TrimPrefix(wsServer.URL, "http")})
	}))
	defer apiServer.Close()
	b := bus.New(1)
	ch := &Channel{Config: config.SlackChannelConfig{AppToken: "app", BotToken: "bot", APIBase: apiServer.URL, RequireMention: true, OpenAccess: true}, IsolatePeers: true}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := ch.Start(ctx, b); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = ch.Stop(context.Background()) }()
	select {
	case ev := <-b.Channel():
		if ev.SessionKey != "slack:C1:U1" {
			t.Fatalf("expected isolated session key, got %#v", ev)
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
	ch := &Channel{Config: config.SlackChannelConfig{BotToken: "bot", APIBase: apiServer.URL, DefaultChannelID: "C1", OpenAccess: true}}
	if err := ch.Deliver(context.Background(), "", "hello", map[string]any{"thread_ts": "123.45"}); err != nil {
		t.Fatalf("Deliver: %v", err)
	}
	if got["channel"] != "C1" || got["text"] != "hello" || got["thread_ts"] != "123.45" {
		t.Fatalf("unexpected payload: %#v", got)
	}
}

func TestChannel_DeliverSurfacesRateLimit(t *testing.T) {
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "3")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer apiServer.Close()
	ch := &Channel{Config: config.SlackChannelConfig{BotToken: "bot", APIBase: apiServer.URL, DefaultChannelID: "C1", OpenAccess: true}}
	err := ch.Deliver(context.Background(), "", "hello", nil)
	if err == nil || !strings.Contains(err.Error(), "slack rate limited") || !strings.Contains(err.Error(), "3s") {
		t.Fatalf("expected slack rate-limit error, got %v", err)
	}
}

func TestChannel_StartReceivesFileShare(t *testing.T) {
	d := openSlackTestDB(t)
	store := &artifacts.Store{Dir: t.TempDir(), DB: d}
	fileServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if auth := r.Header.Get("Authorization"); auth != "Bearer bot" {
			t.Fatalf("expected bot auth header, got %q", auth)
		}
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
		_ = conn.WriteJSON(map[string]any{
			"envelope_id": "env1",
			"type":        "events_api",
			"payload": map[string]any{
				"authorizations": []map[string]any{{"user_id": "B123"}},
				"event": map[string]any{
					"type":    "message",
					"text":    "",
					"user":    "U1",
					"channel": "C1",
					"files": []map[string]any{{
						"id":                   "F1",
						"name":                 "image.png",
						"mimetype":             "image/png",
						"size":                 10,
						"url_private_download": fileServer.URL + "/download",
					}},
				},
			},
		})
		var ack map[string]any
		if err := conn.ReadJSON(&ack); err != nil {
			t.Fatalf("ReadJSON ack: %v", err)
		}
		if ack["envelope_id"] != "env1" {
			t.Fatalf("unexpected ack: %#v", ack)
		}
		<-time.After(100 * time.Millisecond)
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
	ch := &Channel{
		Config:        config.SlackChannelConfig{AppToken: "app", BotToken: "bot", APIBase: apiServer.URL, RequireMention: false, OpenAccess: true},
		Artifacts:     store,
		MaxMediaBytes: 1024,
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := ch.Start(ctx, b); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() {
		if err := ch.Stop(context.Background()); err != nil {
			t.Fatalf("Stop: %v", err)
		}
	}()

	select {
	case ev := <-b.Channel():
		if ev.Message != "[image: image.png]" {
			t.Fatalf("unexpected event message: %#v", ev.Message)
		}
		raw, ok := ev.Meta["attachments"].([]artifacts.Attachment)
		if !ok || len(raw) != 1 {
			t.Fatalf("expected one attachment in meta, got %#v", ev.Meta["attachments"])
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for slack media event")
	}
}

func TestChannel_StartReceivesFileShareWhenMentionRequired(t *testing.T) {
	d := openSlackTestDB(t)
	store := &artifacts.Store{Dir: t.TempDir(), DB: d}
	fileServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if auth := r.Header.Get("Authorization"); auth != "Bearer bot" {
			t.Fatalf("expected bot auth header, got %q", auth)
		}
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
		_ = conn.WriteJSON(map[string]any{
			"envelope_id": "env1",
			"type":        "events_api",
			"payload": map[string]any{
				"authorizations": []map[string]any{{"user_id": "B123"}},
				"event": map[string]any{
					"type":    "message",
					"text":    "",
					"user":    "U1",
					"channel": "C1",
					"files": []map[string]any{{
						"id":                   "F1",
						"name":                 "image.png",
						"mimetype":             "image/png",
						"size":                 10,
						"url_private_download": fileServer.URL + "/download",
					}},
				},
			},
		})
		var ack map[string]any
		if err := conn.ReadJSON(&ack); err != nil {
			t.Fatalf("ReadJSON ack: %v", err)
		}
		if ack["envelope_id"] != "env1" {
			t.Fatalf("unexpected ack: %#v", ack)
		}
		<-time.After(100 * time.Millisecond)
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
	ch := &Channel{
		Config:        config.SlackChannelConfig{AppToken: "app", BotToken: "bot", APIBase: apiServer.URL, RequireMention: true, OpenAccess: true},
		Artifacts:     store,
		MaxMediaBytes: 1024,
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := ch.Start(ctx, b); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() {
		if err := ch.Stop(context.Background()); err != nil {
			t.Fatalf("Stop: %v", err)
		}
	}()

	select {
	case ev := <-b.Channel():
		if ev.Message != "[image: image.png]" {
			t.Fatalf("unexpected event message: %#v", ev.Message)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for slack media event with mention requirement")
	}
}

func TestChannel_DeliverUploadsMedia(t *testing.T) {
	mediaPath := filepath.Join(t.TempDir(), "image.png")
	if err := os.WriteFile(mediaPath, []byte("image-data"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	uploadServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST upload, got %s", r.Method)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer uploadServer.Close()

	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/files.getUploadURLExternal":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok":         true,
				"upload_url": uploadServer.URL + "/upload",
				"file_id":    "F1",
			})
		case "/files.completeUploadExternal":
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
		default:
			t.Fatalf("unexpected api path: %s", r.URL.Path)
		}
	}))
	defer apiServer.Close()

	ch := &Channel{Config: config.SlackChannelConfig{BotToken: "bot", APIBase: apiServer.URL, DefaultChannelID: "C1", OpenAccess: true}, MaxMediaBytes: 1024}
	if err := ch.Deliver(context.Background(), "", "hello", map[string]any{"media_paths": []string{mediaPath}, "thread_ts": "123.45"}); err != nil {
		t.Fatalf("Deliver: %v", err)
	}
}
