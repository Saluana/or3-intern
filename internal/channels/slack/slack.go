package slack

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
	Config config.SlackChannelConfig
	HTTP   *http.Client
	Dialer *websocket.Dialer

	mu     sync.Mutex
	conn   *websocket.Conn
	cancel context.CancelFunc
	botID  string
}

func (c *Channel) Name() string { return "slack" }

func (c *Channel) Start(ctx context.Context, eventBus *bus.Bus) error {
	if strings.TrimSpace(c.Config.AppToken) == "" || strings.TrimSpace(c.Config.BotToken) == "" {
		return fmt.Errorf("slack tokens not configured")
	}
	url, err := c.openSocketURL(ctx)
	if err != nil {
		return err
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
	c.cancel = nil
	c.conn = nil
	return nil
}

func (c *Channel) Deliver(ctx context.Context, to, text string, meta map[string]any) error {
	channelID := strings.TrimSpace(to)
	if channelID == "" {
		channelID = strings.TrimSpace(c.Config.DefaultChannelID)
	}
	if channelID == "" {
		return fmt.Errorf("slack channel id required")
	}
	payload := map[string]any{"channel": channelID, "text": text}
	if threadTS, ok := meta["thread_ts"].(string); ok && threadTS != "" {
		payload["thread_ts"] = threadTS
	}
	return c.postJSON(ctx, c.apiBase()+"/chat.postMessage", c.Config.BotToken, payload, nil)
}

func (c *Channel) readLoop(ctx context.Context, eventBus *bus.Bus) {
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
		var envelope socketEnvelope
		if err := json.Unmarshal(raw, &envelope); err != nil {
			continue
		}
		if envelope.EnvelopeID != "" {
			_ = conn.WriteJSON(map[string]any{"envelope_id": envelope.EnvelopeID})
		}
		if envelope.Type == "hello" {
			continue
		}
		if envelope.Type != "events_api" || envelope.Payload.Event.Type != "message" {
			continue
		}
		ev := envelope.Payload.Event
		if ev.BotID != "" || ev.User == "" || strings.TrimSpace(ev.Text) == "" {
			continue
		}
		if !c.allowedUser(ev.User) {
			continue
		}
		if c.Config.RequireMention && c.botID != "" && !strings.Contains(ev.Text, "<@"+c.botID+">") {
			continue
		}
		if envelope.Payload.Authorizations[0].UserID != "" && c.botID == "" {
			c.botID = envelope.Payload.Authorizations[0].UserID
		}
		clean := strings.TrimSpace(strings.ReplaceAll(ev.Text, "<@"+c.botID+">", ""))
		eventBus.Publish(bus.Event{Type: bus.EventUserMessage, SessionKey: "slack:" + ev.Channel, Channel: "slack", From: ev.User, Message: clean, Meta: map[string]any{"channel_id": ev.Channel, "thread_ts": ev.ThreadTS}})
		select {
		case <-ctx.Done():
			return
		default:
		}
	}
}

func (c *Channel) openSocketURL(ctx context.Context) (string, error) {
	var resp struct {
		OK  bool   `json:"ok"`
		URL string `json:"url"`
	}
	if err := c.postJSON(ctx, c.apiBase()+"/apps.connections.open", c.Config.AppToken, nil, &resp); err != nil {
		return "", err
	}
	if !resp.OK || resp.URL == "" {
		return "", fmt.Errorf("slack socket url missing")
	}
	return resp.URL, nil
}

func (c *Channel) apiBase() string {
	base := strings.TrimRight(strings.TrimSpace(c.Config.APIBase), "/")
	if base == "" {
		base = "https://slack.com/api"
	}
	return base
}

func (c *Channel) client() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 20 * time.Second}
}

func (c *Channel) postJSON(ctx context.Context, endpoint, token string, payload any, out any) error {
	var body io.Reader
	if payload != nil {
		b, _ := json.Marshal(payload)
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := c.client().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("slack api error: %s", resp.Status)
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

type socketEnvelope struct {
	EnvelopeID string `json:"envelope_id"`
	Type       string `json:"type"`
	Payload    struct {
		Event struct {
			Type     string `json:"type"`
			Text     string `json:"text"`
			User     string `json:"user"`
			BotID    string `json:"bot_id"`
			Channel  string `json:"channel"`
			ThreadTS string `json:"thread_ts"`
		} `json:"event"`
		Authorizations []struct {
			UserID string `json:"user_id"`
		} `json:"authorizations"`
	} `json:"payload"`
}
