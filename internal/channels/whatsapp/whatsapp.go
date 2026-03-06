package whatsapp

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"or3-intern/internal/bus"
	"or3-intern/internal/config"
)

type Channel struct {
	Config config.WhatsAppBridgeConfig
	Dialer *websocket.Dialer

	mu     sync.Mutex
	conn   *websocket.Conn
	cancel context.CancelFunc
	closed bool
}

func (c *Channel) Name() string { return "whatsapp" }

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

func (c *Channel) Deliver(ctx context.Context, to, text string, meta map[string]any) error {
	target := strings.TrimSpace(to)
	if target == "" {
		target = strings.TrimSpace(c.Config.DefaultTo)
	}
	if target == "" {
		return fmt.Errorf("whatsapp target required")
	}
	cmd := map[string]any{"type": "send", "to": target, "text": text}
	if meta != nil {
		for k, v := range meta {
			cmd[k] = v
		}
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
		if msg.Type != "message" || strings.TrimSpace(msg.Text) == "" {
			continue
		}
		if !c.allowedFrom(msg.From) {
			continue
		}
		target := strings.TrimSpace(msg.Chat)
		if target == "" {
			target = strings.TrimSpace(msg.From)
		}
		eventBus.Publish(bus.Event{
			Type:       bus.EventUserMessage,
			SessionKey: "whatsapp:" + target,
			Channel:    "whatsapp",
			From:       msg.From,
			Message:    strings.TrimSpace(msg.Text),
			Meta: map[string]any{
				"chat_id":             target,
				"message_id":          msg.ID,
				"reply_to_message_id": msg.ID,
				"is_group":            msg.IsGroup,
			},
		})
	}
}

func (c *Channel) allowedFrom(from string) bool {
	if len(c.Config.AllowedFrom) == 0 {
		return true
	}
	for _, allowed := range c.Config.AllowedFrom {
		if strings.TrimSpace(allowed) == strings.TrimSpace(from) {
			return true
		}
	}
	return false
}

type inboundMessage struct {
	Type    string `json:"type"`
	ID      string `json:"id"`
	Chat    string `json:"chat"`
	From    string `json:"from"`
	Text    string `json:"text"`
	IsGroup bool   `json:"isGroup"`
}

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

func NewTestDialer() *websocket.Dialer {
	return &websocket.Dialer{HandshakeTimeout: 5 * time.Second}
}
