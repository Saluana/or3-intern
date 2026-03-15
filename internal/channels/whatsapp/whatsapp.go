// Package whatsapp implements the WhatsApp bridge channel adapter.
package whatsapp

import (
	"context"
	"encoding/base64"
	"fmt"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"or3-intern/internal/artifacts"
	"or3-intern/internal/bus"
	rootchannels "or3-intern/internal/channels"
	"or3-intern/internal/config"
)

// Channel reads and writes messages over the configured bridge websocket.
type Channel struct {
	Config        config.WhatsAppBridgeConfig
	Dialer        *websocket.Dialer
	Artifacts     *artifacts.Store
	MaxMediaBytes int
	IsolatePeers  bool

	mu     sync.Mutex
	conn   *websocket.Conn
	cancel context.CancelFunc
	closed bool
	dedupe *rootchannels.IngressDeduplicator
}

// Name returns the registered channel name.
func (c *Channel) Name() string { return "whatsapp" }

// Start connects to the bridge and begins reading inbound messages.
func (c *Channel) Start(ctx context.Context, eventBus *bus.Bus) error {
	if strings.TrimSpace(c.Config.BridgeURL) == "" {
		return fmt.Errorf("whatsapp bridge url not configured")
	}
	conn, err := c.connect(ctx)
	if err != nil {
		return err
	}
	childCtx, cancel := context.WithCancel(ctx)
	c.mu.Lock()
	c.conn = conn
	c.cancel = cancel
	c.closed = false
	c.mu.Unlock()
	go c.readLoop(childCtx, eventBus)
	return nil
}

// Stop closes the bridge connection.
func (c *Channel) Stop(ctx context.Context) error {
	_ = ctx
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cancel != nil {
		c.cancel()
	}
	if c.conn != nil {
		_ = c.conn.Close()
	}
	c.conn = nil
	c.cancel = nil
	c.closed = true
	return nil
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
	defer c.mu.Unlock()
	if c.conn == nil {
		return fmt.Errorf("whatsapp bridge not connected")
	}
	return c.conn.WriteJSON(cmd)
}

func (c *Channel) connect(ctx context.Context) (*websocket.Conn, error) {
	dialer := c.Dialer
	if dialer == nil {
		dialer = websocket.DefaultDialer
	}
	headers := http.Header{}
	if token := strings.TrimSpace(c.Config.BridgeToken); token != "" {
		headers.Set("Authorization", "Bearer "+token)
	}
	conn, _, err := dialer.DialContext(ctx, c.Config.BridgeURL, headers)
	if err != nil {
		return nil, err
	}
	return conn, nil
}

func (c *Channel) readLoop(ctx context.Context, eventBus *bus.Bus) {
	for {
		c.mu.Lock()
		conn := c.conn
		c.mu.Unlock()
		if conn == nil {
			return
		}
		var msg inboundMessage
		if err := conn.ReadJSON(&msg); err != nil {
			select {
			case <-ctx.Done():
				return
			default:
				return
			}
		}
		if msg.Type != "message" {
			continue
		}
		if key := whatsappDedupeKey(msg); key != "" && c.ingressDeduper().IsDuplicate(key) {
			continue
		}
		if !c.allowedFrom(msg.From) {
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
		eventBus.Publish(bus.Event{
			Type:       bus.EventUserMessage,
			SessionKey: sessionKey,
			Channel:    "whatsapp",
			From:       msg.From,
			Message:    content,
			Meta:       meta,
		})
	}
}

func (c *Channel) allowedFrom(from string) bool {
	if len(c.Config.AllowedFrom) == 0 {
		return c.Config.OpenAccess
	}
	for _, allowed := range c.Config.AllowedFrom {
		if strings.TrimSpace(allowed) == strings.TrimSpace(from) {
			return true
		}
	}
	return false
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
