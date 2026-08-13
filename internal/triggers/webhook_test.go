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
	"sync/atomic"
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
	if headers != nil {
		signWebhookTestRequest(req, body, srv.Config.Secret)
	}
	rw := httptest.NewRecorder()
	srv.handle(rw, req)
	return rw
}

var webhookTestNonce atomic.Uint64

func signWebhookTestRequest(req *http.Request, body, secret string) {
	timestamp := time.Now().UTC().Unix()
	timestampRaw := fmt.Sprint(timestamp)
	bodyHash := sha256.Sum256([]byte(body))
	canonical := strings.Join([]string{"v1", timestampRaw, req.Method, req.URL.EscapedPath(), strings.TrimSpace(req.Header.Get("Content-Type")), hex.EncodeToString(bodyHash[:])}, "\n")
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(canonical))
	req.Header.Set("X-Webhook-Timestamp", timestampRaw)
	req.Header.Set("X-Webhook-Nonce", fmt.Sprintf("test-%d", webhookTestNonce.Add(1)))
	req.Header.Set("X-Webhook-Signature", "v1="+hex.EncodeToString(mac.Sum(nil)))
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

	srv, b := newTestWebhookServer(t, secret)
	rw := doRequest(t, srv, body, map[string]string{})
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

func TestWebhookRejectsReplayAndCrossRouteReuse(t *testing.T) {
	srv, _ := newTestWebhookServer(t, "secret")
	req := httptest.NewRequest(http.MethodPost, "/webhook/a", strings.NewReader("hello"))
	signWebhookTestRequest(req, "hello", "secret")
	first := httptest.NewRecorder()
	srv.handle(first, req)
	if first.Code != http.StatusOK {
		t.Fatalf("first request failed: %d", first.Code)
	}

	replay := httptest.NewRequest(http.MethodPost, "/webhook/a", strings.NewReader("hello"))
	replay.Header = req.Header.Clone()
	replayResult := httptest.NewRecorder()
	srv.handle(replayResult, replay)
	if replayResult.Code != http.StatusUnauthorized {
		t.Fatalf("expected replay rejection, got %d", replayResult.Code)
	}

	crossRoute := httptest.NewRequest(http.MethodPost, "/webhook/b", strings.NewReader("hello"))
	crossRoute.Header = req.Header.Clone()
	crossRoute.Header.Set("X-Webhook-Nonce", "different")
	crossResult := httptest.NewRecorder()
	srv.handle(crossResult, crossRoute)
	if crossResult.Code != http.StatusUnauthorized {
		t.Fatalf("expected cross-route signature rejection, got %d", crossResult.Code)
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
		structured, ok := ev.Meta[MetaKeyStructuredEvent].(map[string]any)
		if !ok || structured["source"] != "webhook" {
			t.Fatalf("expected structured webhook metadata, got %#v", ev.Meta)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for bus event")
	}
}

func TestWebhookStructuredPreviewIsBounded(t *testing.T) {
	srv, b := newTestWebhookServer(t, "s3cr3t")
	payload := strings.Repeat("a", structuredBodyPreviewMaxChars+128)
	rw := doRequest(t, srv, payload, map[string]string{"X-Webhook-Secret": "s3cr3t"})
	if rw.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rw.Code)
	}
	select {
	case ev := <-b.Channel():
		structured, ok := ev.Meta[MetaKeyStructuredEvent].(map[string]any)
		if !ok {
			t.Fatalf("expected structured webhook metadata, got %#v", ev.Meta)
		}
		details, ok := structured["details"].(map[string]any)
		if !ok {
			t.Fatalf("expected structured details, got %#v", structured)
		}
		preview := fmt.Sprint(details["body_preview"])
		if len(preview) <= structuredBodyPreviewMaxChars || !strings.Contains(preview, "[truncated]") {
			t.Fatalf("expected truncated preview, got %q", preview)
		}
		if got := int(details["body_bytes"].(int)); got != len(payload) {
			t.Fatalf("expected body_bytes=%d, got %d", len(payload), got)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for bus event")
	}
}

func TestWebhookPublishesStructuredTasks(t *testing.T) {
	srv, b := newTestWebhookServer(t, "s3cr3t")
	payload := `{"tasks":[{"tool":"echo_tool","params":{"text":"hi"}}]}`
	rw := doRequest(t, srv, payload, map[string]string{"X-Webhook-Secret": "s3cr3t"})
	if rw.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rw.Code)
	}
	select {
	case ev := <-b.Channel():
		structuredTasks, ok := ev.Meta[MetaKeyStructuredTasks].(map[string]any)
		if !ok {
			t.Fatalf("expected structured tasks metadata, got %#v", ev.Meta)
		}
		first, ok := firstStructuredTask(structuredTasks)
		if !ok || first["tool"] != "echo_tool" {
			t.Fatalf("expected echo_tool task, got %#v", structuredTasks)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for bus event")
	}
}

func firstStructuredTask(structuredTasks map[string]any) (map[string]any, bool) {
	if rawTasks, ok := structuredTasks["tasks"].([]any); ok && len(rawTasks) > 0 {
		first, ok := rawTasks[0].(map[string]any)
		return first, ok
	}
	if rawTasks, ok := structuredTasks["tasks"].([]map[string]any); ok && len(rawTasks) > 0 {
		return rawTasks[0], true
	}
	return nil, false
}
