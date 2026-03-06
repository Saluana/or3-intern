package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"or3-intern/internal/bus"
	"or3-intern/internal/config"
)

type Channel struct {
	Config config.TelegramChannelConfig
	HTTP   *http.Client

	mu      sync.Mutex
	running bool
	cancel  context.CancelFunc
	offset  int64
}

func (c *Channel) Name() string { return "telegram" }

func (c *Channel) Start(ctx context.Context, eventBus *bus.Bus) error {
	if strings.TrimSpace(c.Config.Token) == "" {
		return fmt.Errorf("telegram token not configured")
	}
	if eventBus == nil {
		return fmt.Errorf("event bus not configured")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.running {
		return nil
	}
	childCtx, cancel := context.WithCancel(ctx)
	c.cancel = cancel
	c.running = true
	go c.poll(childCtx, eventBus)
	return nil
}

func (c *Channel) Stop(ctx context.Context) error {
	_ = ctx
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cancel != nil {
		c.cancel()
	}
	c.cancel = nil
	c.running = false
	return nil
}

func (c *Channel) Deliver(ctx context.Context, to, text string, meta map[string]any) error {
	chatID := strings.TrimSpace(to)
	if chatID == "" {
		chatID = strings.TrimSpace(c.Config.DefaultChatID)
	}
	if chatID == "" {
		return fmt.Errorf("telegram target chat id required")
	}
	payload := map[string]any{"chat_id": chatID, "text": text}
	if replyID, ok := meta["reply_to_message_id"].(int64); ok && replyID > 0 {
		payload["reply_to_message_id"] = replyID
	}
	return c.postJSON(ctx, "/sendMessage", payload, nil)
}

func (c *Channel) poll(ctx context.Context, eventBus *bus.Bus) {
	interval := time.Duration(c.Config.PollSeconds) * time.Second
	if interval <= 0 {
		interval = 2 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if err := c.fetchUpdates(ctx, eventBus); err != nil {
			select {
			case <-ctx.Done():
				return
			case <-time.After(interval):
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}

}

func (c *Channel) fetchUpdates(ctx context.Context, eventBus *bus.Bus) error {
	query := map[string]string{"timeout": "0"}
	c.mu.Lock()
	if c.offset > 0 {
		query["offset"] = strconv.FormatInt(c.offset, 10)
	}
	c.mu.Unlock()
	var updates []update
	if err := c.getJSON(ctx, "/getUpdates", query, &updates); err != nil {
		return err
	}
	for _, update := range updates {
		c.mu.Lock()
		if next := update.UpdateID + 1; next > c.offset {
			c.offset = next
		}
		c.mu.Unlock()
		msg := update.Message
		if msg.Text == "" {
			continue
		}
		chatID := strconv.FormatInt(msg.Chat.ID, 10)
		if !c.allowedChat(chatID) {
			continue
		}
		eventBus.Publish(bus.Event{
			Type:       bus.EventUserMessage,
			SessionKey: "telegram:" + chatID,
			Channel:    "telegram",
			From:       strconv.FormatInt(msg.From.ID, 10),
			Message:    strings.TrimSpace(msg.Text),
			Meta: map[string]any{
				"chat_id":             chatID,
				"message_id":          msg.MessageID,
				"reply_to_message_id": int64(msg.MessageID),
				"username":            msg.From.Username,
			},
		})
	}
	return nil
}

func (c *Channel) allowedChat(chatID string) bool {
	if len(c.Config.AllowedChatIDs) == 0 {
		return true
	}
	for _, allowed := range c.Config.AllowedChatIDs {
		if strings.TrimSpace(allowed) == chatID {
			return true
		}
	}
	return false
}

func (c *Channel) client() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 20 * time.Second}
}

func (c *Channel) apiBase() string {
	base := strings.TrimRight(strings.TrimSpace(c.Config.APIBase), "/")
	if base == "" {
		base = "https://api.telegram.org"
	}
	return base + "/bot" + c.Config.Token
}

func (c *Channel) getJSON(ctx context.Context, path string, query map[string]string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.apiBase()+path, nil)
	if err != nil {
		return err
	}
	q := req.URL.Query()
	for k, v := range query {
		q.Set(k, v)
	}
	req.URL.RawQuery = q.Encode()
	resp, err := c.client().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("telegram api error: %s", resp.Status)
	}
	var envelope apiEnvelope
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return err
	}
	if !envelope.OK {
		return fmt.Errorf("telegram api error: %s", envelope.Description)
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(envelope.Result, out)
}

func (c *Channel) postJSON(ctx context.Context, path string, payload any, out any) error {
	b, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.apiBase()+path, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.client().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("telegram api error: %s", resp.Status)
	}
	var envelope apiEnvelope
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return err
	}
	if !envelope.OK {
		return fmt.Errorf("telegram api error: %s", envelope.Description)
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(envelope.Result, out)
}

type apiEnvelope struct {
	OK          bool            `json:"ok"`
	Description string          `json:"description"`
	Result      json.RawMessage `json:"result"`
}

type update struct {
	UpdateID int64          `json:"update_id"`
	Message  inboundMessage `json:"message"`
}

type inboundMessage struct {
	MessageID int   `json:"message_id"`
	Text      string `json:"text"`
	Chat      struct {
		ID int64 `json:"id"`
	} `json:"chat"`
	From struct {
		ID       int64  `json:"id"`
		Username string `json:"username"`
	} `json:"from"`
}
