// Package whatsapp implements the WhatsApp bridge channel adapter.
package whatsapp

import (
	"context"
	"encoding/base64"
	"fmt"
	"log"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"or3-intern/internal/approval"
	"or3-intern/internal/artifacts"
	"or3-intern/internal/bus"
	rootchannels "or3-intern/internal/channels"
	"or3-intern/internal/channels/shared"
	"or3-intern/internal/config"
)

// Channel reads and writes messages over the configured bridge websocket.
type Channel struct {
	Config         config.WhatsAppBridgeConfig
	Dialer         *websocket.Dialer
	Artifacts      *artifacts.Store
	MaxMediaBytes  int
	IsolatePeers   bool
	ApprovalBroker *approval.Broker

	mu     sync.Mutex
	conn   *websocket.Conn
	cancel context.CancelFunc
	done   chan struct{}
	state  string
	dedupe *rootchannels.IngressDeduplicator

	// reconnectDelay is test-only injection for prompt deterministic retries.
	reconnectDelay func(int) time.Duration
}

// Name returns the registered channel name.
func (c *Channel) Name() string { return "whatsapp" }

// Start connects to the bridge and begins reading inbound messages.
func (c *Channel) Start(ctx context.Context, eventBus *bus.Bus) error {
	if strings.TrimSpace(c.Config.BridgeURL) == "" {
		return fmt.Errorf("whatsapp bridge url not configured")
	}
	c.mu.Lock()
	alreadyRunning := c.cancel != nil
	if alreadyRunning {
		c.mu.Unlock()
		return nil
	}
	childCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	c.cancel = cancel
	c.done = done
	c.state = "connecting"
	c.mu.Unlock()

	conn, err := c.connect(childCtx)
	if err != nil {
		cancel()
		c.finishStart(done)
		return err
	}
	c.mu.Lock()
	if c.done != done || childCtx.Err() != nil {
		c.mu.Unlock()
		cancel()
		_ = conn.Close()
		c.finishStart(done)
		if err := childCtx.Err(); err != nil {
			return err
		}
		return fmt.Errorf("whatsapp start interrupted")
	}
	c.conn = conn
	c.state = "connected"
	c.mu.Unlock()
	go c.supervise(childCtx, eventBus, conn, done)
	return nil
}

func (c *Channel) finishStart(done chan struct{}) {
	c.mu.Lock()
	if c.done == done {
		c.conn = nil
		c.cancel = nil
		c.done = nil
		if c.state != "stopped" {
			c.state = "failed"
		}
	}
	c.mu.Unlock()
	close(done)
}

// Stop closes the bridge connection.
func (c *Channel) Stop(ctx context.Context) error {
	c.mu.Lock()
	cancel := c.cancel
	conn := c.conn
	done := c.done
	c.conn = nil
	c.cancel = nil
	c.done = nil
	c.state = "stopped"
	c.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if conn != nil {
		_ = conn.Close()
	}
	if done == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// ConnectionStatus reports the current bridge state for diagnostics.
func (c *Channel) ConnectionStatus() string {
	if c == nil {
		return "stopped"
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.state == "" {
		return "stopped"
	}
	return c.state
}

// Deliver sends a bridge command for a text or media message.
func (c *Channel) Deliver(ctx context.Context, to, text string, meta map[string]any) error {
	target := strings.TrimSpace(to)
	if target == "" {
		target = strings.TrimSpace(c.Config.DefaultTo)
	}
	if target == "" {
		return fmt.Errorf("whatsapp target required")
	}
	cmd := map[string]any{"type": "send", "to": target, "text": text}
	if mediaPaths := rootchannels.MediaPaths(meta); len(mediaPaths) > 0 {
		attachments, err := c.outboundAttachments(mediaPaths)
		if err != nil {
			return err
		}
		cmd["attachments"] = attachments
	}
	for k, v := range meta {
		if k == "media_paths" {
			continue
		}
		cmd[k] = v
	}
	c.mu.Lock()
	conn := c.conn
	c.mu.Unlock()
	if conn == nil {
		return fmt.Errorf("whatsapp bridge not connected")
	}
	_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	return conn.WriteJSON(cmd)
}

func (c *Channel) connect(ctx context.Context) (*websocket.Conn, error) {
	headers := http.Header{}
	if token := strings.TrimSpace(c.Config.BridgeToken); token != "" {
		headers.Set("Authorization", "Bearer "+token)
	}
	conn, resp, err := shared.DialWebSocketContext(ctx, c.Dialer, c.Config.BridgeURL, headers)
	if err != nil {
		if resp != nil && (resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden) {
			return nil, fmt.Errorf("whatsapp bridge authentication failed: %s", resp.Status)
		}
		return nil, err
	}
	return conn, nil
}

func (c *Channel) supervise(ctx context.Context, eventBus *bus.Bus, conn *websocket.Conn, done chan struct{}) {
	defer func() {
		c.mu.Lock()
		if c.done == done {
			c.conn = nil
			c.cancel = nil
			c.done = nil
			if c.state != "stopped" {
				c.state = "failed"
			}
		}
		c.mu.Unlock()
		close(done)
	}()

	attempt := 0
	for {
		connectedAt := time.Now()
		err := c.readConnection(ctx, eventBus, conn)
		_ = conn.Close()
		if ctx.Err() != nil {
			return
		}
		if time.Since(connectedAt) >= 5*time.Second {
			attempt = 0
		}
		if !whatsappRetryable(err) {
			log.Printf("whatsapp bridge stopped: %v", err)
			return
		}
		c.setConnectionState(done, nil, "reconnecting")
		for {
			delay := c.nextReconnectDelay(attempt)
			attempt++
			log.Printf("whatsapp bridge disconnected: %v; reconnecting in %s", err, delay.Round(time.Millisecond))
			if !shared.WaitForReconnect(ctx, delay) {
				return
			}
			conn, err = c.connect(ctx)
			if err == nil {
				if !c.setConnectionState(done, conn, "connected") {
					_ = conn.Close()
					return
				}
				break
			}
			if !whatsappRetryable(err) {
				log.Printf("whatsapp bridge stopped: %v", err)
				return
			}
		}
	}
}

func (c *Channel) setConnectionState(done chan struct{}, conn *websocket.Conn, state string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.done != done || c.cancel == nil {
		return false
	}
	c.conn = conn
	c.state = state
	return true
}

func (c *Channel) nextReconnectDelay(attempt int) time.Duration {
	if c.reconnectDelay != nil {
		return c.reconnectDelay(attempt)
	}
	return shared.ReconnectDelay(attempt)
}

func whatsappRetryable(err error) bool {
	return err != nil && !shared.IsPermanentConnectionError(err)
}

func (c *Channel) readConnection(ctx context.Context, eventBus *bus.Bus, conn *websocket.Conn) error {
	for {
		var msg inboundMessage
		if err := conn.ReadJSON(&msg); err != nil {
			return err
		}
		if msg.Type != "message" {
			continue
		}
		if key := whatsappDedupeKey(msg); key != "" && c.ingressDeduper().IsDuplicate(key) {
			continue
		}
		if !c.allowedFrom(ctx, msg.From) {
			continue
		}
		target := strings.TrimSpace(msg.Chat)
		if target == "" {
			target = strings.TrimSpace(msg.From)
		}
		sessionKey := "whatsapp:" + target
		if c.IsolatePeers {
			sessionKey += ":" + strings.TrimSpace(msg.From)
		}
		attachments, markers := c.captureAttachments(ctx, sessionKey, msg.Attachments)
		content := rootchannels.ComposeMessageText(msg.Text, markers)
		if content == "" {
			continue
		}
		meta := map[string]any{
			"chat_id":             target,
			"message_id":          msg.ID,
			"reply_to_message_id": msg.ID,
			"is_group":            msg.IsGroup,
		}
		if len(attachments) > 0 {
			meta["attachments"] = attachments
		}
		if ok := eventBus.Publish(bus.Event{
			Type:       bus.EventUserMessage,
			SessionKey: sessionKey,
			Channel:    "whatsapp",
			From:       msg.From,
			Message:    content,
			Meta:       meta,
		}); !ok {
			log.Printf("whatsapp event dropped: queue unavailable for target=%s from=%s", target, msg.From)
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
	}
}

func (c *Channel) allowedFrom(ctx context.Context, from string) bool {
	return shared.AllowInboundIdentity(ctx, shared.InboundAccessInput{Policy: c.Config.InboundPolicy, OpenAccess: c.Config.OpenAccess, Allowlist: c.Config.AllowedFrom, Channel: "whatsapp", Identity: from, Broker: c.ApprovalBroker})
}

type inboundMessage struct {
	Type        string             `json:"type"`
	ID          string             `json:"id"`
	Chat        string             `json:"chat"`
	From        string             `json:"from"`
	Text        string             `json:"text"`
	IsGroup     bool               `json:"isGroup"`
	Attachments []bridgeAttachment `json:"attachments"`
}

type bridgeAttachment struct {
	Path       string `json:"path,omitempty"`
	DataBase64 string `json:"data_base64,omitempty"`
	Filename   string `json:"filename,omitempty"`
	Mime       string `json:"mime,omitempty"`
	Kind       string `json:"kind,omitempty"`
	SizeBytes  int64  `json:"size_bytes,omitempty"`
}

func (c *Channel) captureAttachments(ctx context.Context, sessionKey string, refs []bridgeAttachment) ([]artifacts.Attachment, []string) {
	attachments := make([]artifacts.Attachment, 0, len(refs))
	markers := make([]string, 0, len(refs))
	for _, ref := range refs {
		filename := artifacts.NormalizeFilename(ref.Filename, ref.Mime)
		kind := strings.TrimSpace(ref.Kind)
		if kind == "" {
			kind = artifacts.DetectKind(filename, ref.Mime)
		}
		if c.MaxMediaBytes == 0 {
			markers = append(markers, artifacts.FailureMarker(kind, filename, "disabled by config"))
			continue
		}
		if c.MaxMediaBytes > 0 && ref.SizeBytes > int64(c.MaxMediaBytes) {
			markers = append(markers, artifacts.FailureMarker(kind, filename, "too large"))
			continue
		}
		if c.Artifacts == nil {
			markers = append(markers, artifacts.FailureMarker(kind, filename, "storage unavailable"))
			continue
		}
		data, err := decodeBridgeAttachment(ref, c.MaxMediaBytes)
		if err != nil {
			reason := "invalid media payload"
			if strings.Contains(err.Error(), "too large") {
				reason = "too large"
			}
			markers = append(markers, artifacts.FailureMarker(kind, filename, reason))
			continue
		}
		att, err := c.Artifacts.SaveNamed(ctx, sessionKey, filename, ref.Mime, data)
		if err != nil {
			markers = append(markers, artifacts.FailureMarker(kind, filename, "save failed"))
			continue
		}
		attachments = append(attachments, att)
		markers = append(markers, artifacts.Marker(att))
	}
	return attachments, markers
}

func (c *Channel) outboundAttachments(paths []string) ([]bridgeAttachment, error) {
	attachments := make([]bridgeAttachment, 0, len(paths))
	for _, mediaPath := range paths {
		info, err := os.Stat(mediaPath)
		if err != nil {
			return nil, err
		}
		if c.MaxMediaBytes == 0 {
			return nil, fmt.Errorf("media attachments disabled by config")
		}
		if c.MaxMediaBytes > 0 && info.Size() > int64(c.MaxMediaBytes) {
			return nil, fmt.Errorf("media path exceeds maxMediaBytes: %s", mediaPath)
		}
		data, err := os.ReadFile(mediaPath)
		if err != nil {
			return nil, err
		}
		mimeType := mime.TypeByExtension(strings.ToLower(filepath.Ext(mediaPath)))
		attachments = append(attachments, bridgeAttachment{
			DataBase64: base64.StdEncoding.EncodeToString(data),
			Filename:   filepath.Base(mediaPath),
			Mime:       mimeType,
			Kind:       artifacts.DetectKind(mediaPath, mimeType),
			SizeBytes:  info.Size(),
		})
	}
	return attachments, nil
}

func decodeBridgeAttachment(ref bridgeAttachment, maxBytes int) ([]byte, error) {
	raw := strings.TrimSpace(ref.DataBase64)
	if raw == "" {
		return nil, fmt.Errorf("missing inline data")
	}
	if maxBytes > 0 && base64.StdEncoding.DecodedLen(len(raw)) > maxBytes {
		return nil, fmt.Errorf("attachment too large")
	}
	data, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return nil, err
	}
	if maxBytes > 0 && len(data) > maxBytes {
		return nil, fmt.Errorf("attachment too large")
	}
	return data, nil
}

// BridgeURL normalizes a base WhatsApp bridge URL to its websocket endpoint.
func BridgeURL(base string) string {
	u, err := url.Parse(strings.TrimSpace(base))
	if err != nil || u == nil {
		return ""
	}
	if u.Path == "" {
		u.Path = "/ws"
	}
	return u.String()
}

// NewTestDialer returns a short-timeout dialer for bridge tests.
func NewTestDialer() *websocket.Dialer {
	return &websocket.Dialer{HandshakeTimeout: 5 * time.Second}
}

func (c *Channel) ingressDeduper() *rootchannels.IngressDeduplicator {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.dedupe == nil {
		c.dedupe = rootchannels.NewIngressDeduplicator(0)
	}
	return c.dedupe
}

func whatsappDedupeKey(msg inboundMessage) string {
	if strings.TrimSpace(msg.ID) != "" {
		return msg.ID
	}
	target := strings.TrimSpace(msg.Chat)
	if target == "" {
		target = strings.TrimSpace(msg.From)
	}
	if target == "" || strings.TrimSpace(msg.From) == "" {
		return ""
	}
	return strings.Join([]string{target, msg.From, msg.Text}, "|")
}
