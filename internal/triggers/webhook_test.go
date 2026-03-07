package triggers

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"or3-intern/internal/bus"
	"or3-intern/internal/config"
)

func newTestWebhookServer(t *testing.T, secret string) (*WebhookServer, *bus.Bus) {
	t.Helper()
	b := bus.New(16)
	cfg := config.WebhookConfig{
		Enabled:   true,
		Secret:    secret,
		MaxBodyKB: 1,
	}
	srv := NewWebhookServer(cfg, b, "test-session")
	return srv, b
}

func doRequest(t *testing.T, srv *WebhookServer, body string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(body))
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rw := httptest.NewRecorder()
	srv.handle(rw, req)
	return rw
}

func TestWebhookAuthFailure(t *testing.T) {
	srv, _ := newTestWebhookServer(t, "mysecret")
	rw := doRequest(t, srv, "hello", nil)
	if rw.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rw.Code)
	}
}

func TestWebhookAuthSuccess(t *testing.T) {
	srv, b := newTestWebhookServer(t, "mysecret")
	rw := doRequest(t, srv, "hello", map[string]string{
		"X-Webhook-Secret": "mysecret",
	})
	if rw.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", rw.Code, rw.Body.String())
	}
	select {
	case ev := <-b.Channel():
		if ev.Message != "hello" {
			t.Errorf("expected message 'hello', got %q", ev.Message)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for bus event")
	}
}

func TestWebhookHMAC(t *testing.T) {
	secret := "hmac-secret"
	body := `{"event":"push"}`
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(body))
	sig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	srv, b := newTestWebhookServer(t, secret)
	rw := doRequest(t, srv, body, map[string]string{
		"X-Hub-Signature-256": sig,
	})
	if rw.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", rw.Code, rw.Body.String())
	}
	select {
	case ev := <-b.Channel():
		if ev.Message != body {
			t.Errorf("expected body as message, got %q", ev.Message)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for bus event")
	}
}

func TestWebhookBodySizeLimit(t *testing.T) {
	srv, _ := newTestWebhookServer(t, "mysecret")
	// MaxBodyKB is 1, so generate > 1KB body
	bigBody := strings.Repeat("x", 1025)
	rw := doRequest(t, srv, bigBody, map[string]string{
		"X-Webhook-Secret": "mysecret",
	})
	if rw.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("expected 413, got %d", rw.Code)
	}
}

func TestWebhookPublishesToBus(t *testing.T) {
	srv, b := newTestWebhookServer(t, "s3cr3t")
	payload := `{"action":"test"}`
	rw := doRequest(t, srv, payload, map[string]string{
		"X-Webhook-Secret": "s3cr3t",
		"X-Request-ID":     "req-123",
	})
	if rw.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rw.Code)
	}
	resp, _ := io.ReadAll(rw.Body)
	if string(resp) != "ok" {
		t.Errorf("expected body 'ok', got %q", string(resp))
	}
	select {
	case ev := <-b.Channel():
		if ev.Type != "webhook" {
			t.Errorf("expected EventWebhook, got %q", ev.Type)
		}
		if ev.SessionKey != "test-session" {
			t.Errorf("expected session key 'test-session', got %q", ev.SessionKey)
		}
		if ev.Message != payload {
			t.Errorf("expected message %q, got %q", payload, ev.Message)
		}
		if fmt.Sprint(ev.Meta["x-request-id"]) != "req-123" {
			t.Errorf("expected x-request-id 'req-123', got %q", ev.Meta["x-request-id"])
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for bus event")
	}
}
