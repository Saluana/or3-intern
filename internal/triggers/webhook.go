package triggers

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"time"

	"or3-intern/internal/bus"
	"or3-intern/internal/config"
)

type WebhookServer struct {
	Config     config.WebhookConfig
	Bus        *bus.Bus
	SessionKey string
	server     *http.Server
}

const structuredBodyPreviewMaxChars = 512

func NewWebhookServer(cfg config.WebhookConfig, b *bus.Bus, sessionKey string) *WebhookServer {
	return &WebhookServer{Config: cfg, Bus: b, SessionKey: sessionKey}
}

func (w *WebhookServer) Start(ctx context.Context) error {
	if !w.Config.Enabled || strings.TrimSpace(w.Config.Secret) == "" {
		return nil
	}
	addr := strings.TrimSpace(w.Config.Addr)
	if addr == "" {
		addr = "127.0.0.1:8765"
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/webhook", w.handle)
	mux.HandleFunc("/webhook/", w.handle)
	w.server = &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("webhook listen %s: %w", addr, err)
	}
	go func() {
		if err := w.server.Serve(ln); err != nil && err != http.ErrServerClosed {
			log.Printf("webhook server error: %v", err)
		}
	}()
	return nil
}

func (w *WebhookServer) Stop(ctx context.Context) error {
	if w.server == nil {
		return nil
	}
	return w.server.Shutdown(ctx)
}

func (w *WebhookServer) handle(rw http.ResponseWriter, r *http.Request) {
	maxBytes := int64(w.Config.MaxBodyKB) * 1024
	if maxBytes <= 0 {
		maxBytes = 64 * 1024
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxBytes+1))
	if err != nil {
		http.Error(rw, "read error", http.StatusInternalServerError)
		return
	}
	if int64(len(body)) > maxBytes {
		http.Error(rw, "request too large", http.StatusRequestEntityTooLarge)
		return
	}

	if !w.authenticate(r, body) {
		http.Error(rw, "unauthorized", http.StatusUnauthorized)
		return
	}

	route := strings.TrimPrefix(r.URL.Path, "/webhook")
	route = strings.TrimPrefix(route, "/")
	preview := strings.TrimSpace(string(body))
	if len(preview) > structuredBodyPreviewMaxChars {
		preview = preview[:structuredBodyPreviewMaxChars] + "...[truncated]"
	}

	ev := bus.Event{
		Type:       bus.EventWebhook,
		SessionKey: w.SessionKey,
		Channel:    "webhook",
		From:       r.RemoteAddr,
		Message:    string(body),
		Meta: map[string]any{
			"route":        route,
			"content_type": r.Header.Get("Content-Type"),
			"x-request-id": r.Header.Get("X-Request-ID"),
			MetaKeyStructuredEvent: StructuredEventMap(StructuredEvent{
				Type:    string(bus.EventWebhook),
				Source:  "webhook",
				Trusted: false,
				Details: map[string]any{
					"route":        route,
					"content_type": r.Header.Get("Content-Type"),
					"request_id":   r.Header.Get("X-Request-ID"),
					"remote_addr":  r.RemoteAddr,
					"body_preview": preview,
					"body_bytes":   len(body),
				},
			}),
		},
	}
	if structuredTasks, ok := ParseStructuredTasksText(string(body)); ok {
		ev.Meta[MetaKeyStructuredTasks] = StructuredTasksMap(structuredTasks)
	}
	if ok := w.Bus.Publish(ev); !ok {
		http.Error(rw, "bus full", http.StatusServiceUnavailable)
		return
	}
	rw.WriteHeader(http.StatusOK)
	_, _ = fmt.Fprint(rw, "ok")
}

func (w *WebhookServer) authenticate(r *http.Request, body []byte) bool {
	secret := w.Config.Secret
	if secret == "" {
		return false
	}
	// Check HMAC-SHA256 in X-Hub-Signature-256
	sig := r.Header.Get("X-Hub-Signature-256")
	if strings.HasPrefix(sig, "sha256=") {
		sig = strings.TrimPrefix(sig, "sha256=")
		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write(body)
		expected := hex.EncodeToString(mac.Sum(nil))
		return hmac.Equal([]byte(sig), []byte(expected))
	}
	// Fall back to simple shared secret in X-Webhook-Secret header
	return r.Header.Get("X-Webhook-Secret") == secret
}
