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
	"strconv"
	"strings"
	"sync"
	"time"

	"or3-intern/internal/bus"
	"or3-intern/internal/config"
)

type WebhookServer struct {
	Config     config.WebhookConfig
	Bus        *bus.Bus
	SessionKey string
	server     *http.Server
	errors     chan error
	mu         sync.Mutex
	seenNonces map[string]time.Time
	now        func() time.Time
}

const (
	structuredBodyPreviewMaxChars = 512
	webhookSignatureMaxSkew       = 5 * time.Minute
	webhookReplayCacheMax         = 4096
)

func NewWebhookServer(cfg config.WebhookConfig, b *bus.Bus, sessionKey string) *WebhookServer {
	return &WebhookServer{Config: cfg, Bus: b, SessionKey: sessionKey, errors: make(chan error, 1), seenNonces: map[string]time.Time{}, now: time.Now}
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
			select {
			case w.errors <- err:
			default:
			}
		}
	}()
	return nil
}

// Errors reports unexpected listener failures so the owning runtime can fail
// readiness instead of silently continuing without webhook ingress.
func (w *WebhookServer) Errors() <-chan error {
	if w == nil {
		return nil
	}
	return w.errors
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
	timestampRaw := strings.TrimSpace(r.Header.Get("X-Webhook-Timestamp"))
	nonce := strings.TrimSpace(r.Header.Get("X-Webhook-Nonce"))
	sig := strings.TrimSpace(r.Header.Get("X-Webhook-Signature"))
	if timestampRaw == "" || nonce == "" || !strings.HasPrefix(sig, "v1=") {
		return false
	}
	timestamp, err := strconv.ParseInt(timestampRaw, 10, 64)
	if err != nil {
		return false
	}
	now := time.Now().UTC()
	if w.now != nil {
		now = w.now().UTC()
	}
	signedAt := time.Unix(timestamp, 0).UTC()
	if signedAt.After(now.Add(webhookSignatureMaxSkew)) || now.Sub(signedAt) > webhookSignatureMaxSkew {
		return false
	}
	bodyHash := sha256.Sum256(body)
	canonical := strings.Join([]string{"v1", timestampRaw, strings.ToUpper(r.Method), r.URL.EscapedPath(), strings.TrimSpace(r.Header.Get("Content-Type")), hex.EncodeToString(bodyHash[:])}, "\n")
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(canonical))
	expected := hex.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(strings.TrimPrefix(sig, "v1=")), []byte(expected)) {
		return false
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	for cachedNonce, expires := range w.seenNonces {
		if !expires.After(now) {
			delete(w.seenNonces, cachedNonce)
		}
	}
	if _, exists := w.seenNonces[nonce]; exists || len(w.seenNonces) >= webhookReplayCacheMax {
		return false
	}
	w.seenNonces[nonce] = now.Add(webhookSignatureMaxSkew)
	return true
}
