package discord

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"or3-intern/internal/bus"
	"or3-intern/internal/config"
)

type Channel struct {
	Config config.DiscordChannelConfig
	HTTP   *http.Client
	Dialer *websocket.Dialer

	mu     sync.Mutex
	conn   *websocket.Conn
	cancel context.CancelFunc
	botID  string
}

func (c *Channel) Name() string { return "discord" }

func (c *Channel) Start(ctx context.Context, eventBus *bus.Bus) error {
	if strings.TrimSpace(c.Config.Token) == "" {
		return fmt.Errorf("discord token not configured")
	}
	url := strings.TrimSpace(c.Config.GatewayURL)
	if url == "" {
		var resp struct{ URL string `json:"url"` }
		if err := c.getJSON(ctx, c.apiBase()+"/gateway/bot", &resp); err != nil {
			return err
		}
		url = resp.URL
	}
	if url == "" {
		return fmt.Errorf("discord gateway url missing")
	}
	dialer := c.Dialer
	if dialer == nil {
		dialer = websocket.DefaultDialer
	}
	conn, _, err := dialer.DialContext(ctx, url, nil)
	if err != nil {
		return err
	}
	childCtx, cancel := context.WithCancel(ctx)
	c.mu.Lock()
	c.conn = conn
	c.cancel = cancel
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
	return nil
}

func (c *Channel) Deliver(ctx context.Context, to, text string, meta map[string]any) error {
	channelID := strings.TrimSpace(to)
	if channelID == "" {
		channelID = strings.TrimSpace(c.Config.DefaultChannelID)
	}
	if channelID == "" {
		return fmt.Errorf("discord channel id required")
	}
	payload := map[string]any{"content": text}
	if replyID, ok := meta["message_reference"].(string); ok && replyID != "" {
		payload["message_reference"] = map[string]any{"message_id": replyID}
	}
	return c.postJSON(ctx, c.apiBase()+"/channels/"+channelID+"/messages", payload, nil)
}

func (c *Channel) readLoop(ctx context.Context, eventBus *bus.Bus) {
	var heartbeatTicker *time.Ticker
	defer func() {
		if heartbeatTicker != nil {
			heartbeatTicker.Stop()
		}
	}()
	for {
		c.mu.Lock()
		conn := c.conn
		c.mu.Unlock()
		if conn == nil {
			return
		}
		_, raw, err := conn.ReadMessage()
		if err != nil {
			return
		}
		var frame gatewayFrame
		if err := json.Unmarshal(raw, &frame); err != nil {
			continue
		}
		switch frame.Op {
		case 10:
			var hello struct { HeartbeatInterval float64 `json:"heartbeat_interval"` }
			_ = json.Unmarshal(frame.D, &hello)
			_ = conn.WriteJSON(map[string]any{"op": 2, "d": map[string]any{"token": c.Config.Token, "intents": 513, "properties": map[string]string{"$os": "linux", "$browser": "or3-intern", "$device": "or3-intern"}}})
			interval := time.Duration(int64(hello.HeartbeatInterval)) * time.Millisecond
			if interval > 0 {
				heartbeatTicker = time.NewTicker(interval)
				go func() {
					for {
						select {
						case <-ctx.Done():
							return
						case <-heartbeatTicker.C:
							_ = conn.WriteJSON(map[string]any{"op": 1, "d": nil})
						}
					}
				}()
			}
		case 0:
			switch frame.T {
			case "READY":
				var ready struct { User struct { ID string `json:"id"` } `json:"user"` }
				_ = json.Unmarshal(frame.D, &ready)
				c.botID = ready.User.ID
			case "MESSAGE_CREATE":
				var msg inboundMessage
				_ = json.Unmarshal(frame.D, &msg)
				if msg.Author.Bot || strings.TrimSpace(msg.Content) == "" {
					continue
				}
				if !c.allowedUser(msg.Author.ID) {
					continue
				}
				if c.Config.RequireMention && c.botID != "" && !mentioned(msg.Mentions, c.botID) {
					continue
				}
				clean := strings.TrimSpace(stripMention(msg.Content, c.botID))
				eventBus.Publish(bus.Event{Type: bus.EventUserMessage, SessionKey: "discord:" + msg.ChannelID, Channel: "discord", From: msg.Author.ID, Message: clean, Meta: map[string]any{"channel_id": msg.ChannelID, "message_reference": msg.ID}})
			}
		}
		select {
		case <-ctx.Done():
			return
		default:
		}
	}
}

func (c *Channel) apiBase() string {
	base := strings.TrimRight(strings.TrimSpace(c.Config.APIBase), "/")
	if base == "" {
		base = "https://discord.com/api/v10"
	}
	return base
}

func (c *Channel) client() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 20 * time.Second}
}

func (c *Channel) getJSON(ctx context.Context, endpoint string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bot "+c.Config.Token)
	resp, err := c.client().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("discord api error: %s", resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (c *Channel) postJSON(ctx context.Context, endpoint string, payload any, out any) error {
	b, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bot "+c.Config.Token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.client().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("discord api error: %s %s", resp.Status, string(body))
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (c *Channel) allowedUser(user string) bool {
	if len(c.Config.AllowedUserIDs) == 0 {
		return true
	}
	for _, allowed := range c.Config.AllowedUserIDs {
		if strings.TrimSpace(allowed) == user {
			return true
		}
	}
	return false
}

func mentioned(mentions []mention, botID string) bool {
	for _, m := range mentions {
		if m.ID == botID {
			return true
		}
	}
	return false
}

func stripMention(content, botID string) string {
	if botID == "" {
		return content
	}
	content = strings.ReplaceAll(content, "<@"+botID+">", "")
	content = strings.ReplaceAll(content, "<@!"+botID+">", "")
	return content
}

type gatewayFrame struct {
	Op int             `json:"op"`
	T  string          `json:"t"`
	D  json.RawMessage `json:"d"`
}

type mention struct {
	ID string `json:"id"`
}

type inboundMessage struct {
	ID        string    `json:"id"`
	ChannelID string    `json:"channel_id"`
	Content   string    `json:"content"`
	Mentions  []mention `json:"mentions"`
	Author    struct {
		ID  string `json:"id"`
		Bot bool   `json:"bot"`
	} `json:"author"`
}
