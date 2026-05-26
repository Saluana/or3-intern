// Package discord implements the Discord channel adapter.
package discord

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
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

const (
	discordGatewayIntentGuilds         = 1 << 0
	discordGatewayIntentGuildMessages  = 1 << 9
	discordGatewayIntentDirectMessages = 1 << 12
	discordGatewayIntentMessageContent = 1 << 15
	discordGatewayIntents              = discordGatewayIntentGuilds | discordGatewayIntentGuildMessages | discordGatewayIntentDirectMessages | discordGatewayIntentMessageContent
)

// Channel receives Discord gateway events and sends outbound messages.
type Channel struct {
	Config         config.DiscordChannelConfig
	HTTP           *http.Client
	Dialer         *websocket.Dialer
	Artifacts      *artifacts.Store
	MaxMediaBytes  int
	IsolatePeers   bool
	ApprovalBroker *approval.Broker

	mu       sync.Mutex
	conn     *websocket.Conn
	cancel   context.CancelFunc
	botID    string
	dedupe   *rootchannels.IngressDeduplicator
	guilds   map[string]string
	channels map[string]string
}

// Name returns the registered channel name.
func (c *Channel) Name() string { return "discord" }

// Start connects to the Discord gateway and begins reading events.
func (c *Channel) Start(ctx context.Context, eventBus *bus.Bus) error {
	if strings.TrimSpace(c.Config.Token) == "" {
		return fmt.Errorf("discord token not configured")
	}
	url := strings.TrimSpace(c.Config.GatewayURL)
	if url == "" {
		var resp struct {
			URL string `json:"url"`
		}
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

// Stop closes the Discord gateway connection.
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

// Deliver posts a Discord message or multipart media payload.
func (c *Channel) Deliver(ctx context.Context, to, text string, meta map[string]any) error {
	channelID := strings.TrimSpace(to)
	if channelID == "" {
		channelID = strings.TrimSpace(c.Config.DefaultChannelID)
	}
	if channelID == "" {
		return fmt.Errorf("discord channel id required")
	}
	mediaPaths := rootchannels.MediaPaths(meta)
	if len(mediaPaths) > 0 {
		return c.postMultipart(ctx, channelID, text, mediaPaths, meta)
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
			var hello struct {
				HeartbeatInterval float64 `json:"heartbeat_interval"`
			}
			_ = json.Unmarshal(frame.D, &hello)
			_ = conn.WriteJSON(map[string]any{"op": 2, "d": map[string]any{"token": c.Config.Token, "intents": discordGatewayIntents, "properties": map[string]string{"$os": "linux", "$browser": "or3-intern", "$device": "or3-intern"}}})
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
				var ready struct {
					User struct {
						ID string `json:"id"`
					} `json:"user"`
					Guilds []struct {
						ID   string `json:"id"`
						Name string `json:"name"`
					} `json:"guilds"`
				}
				_ = json.Unmarshal(frame.D, &ready)
				c.botID = ready.User.ID
				for _, guild := range ready.Guilds {
					c.setGuildName(guild.ID, guild.Name)
				}
			case "GUILD_CREATE":
				var guild struct {
					ID       string `json:"id"`
					Name     string `json:"name"`
					Channels []struct {
						ID   string `json:"id"`
						Name string `json:"name"`
					} `json:"channels"`
				}
				_ = json.Unmarshal(frame.D, &guild)
				c.setGuildName(guild.ID, guild.Name)
				for _, channel := range guild.Channels {
					c.setChannelName(channel.ID, channel.Name)
				}
			case "CHANNEL_CREATE":
				var channel struct {
					ID   string `json:"id"`
					Name string `json:"name"`
				}
				_ = json.Unmarshal(frame.D, &channel)
				c.setChannelName(channel.ID, channel.Name)
			case "MESSAGE_CREATE":
				var msg inboundMessage
				_ = json.Unmarshal(frame.D, &msg)
				if msg.Author.Bot {
					continue
				}
				c.recordRecentConversation(msg)
				if key := discordDedupeKey(msg); key != "" && c.ingressDeduper().IsDuplicate(key) {
					continue
				}
				if !c.allowedUser(ctx, msg.Author.ID) {
					continue
				}
				if c.requiresMention(msg) && !mentioned(msg.Mentions, c.botID) {
					continue
				}
				clean := strings.TrimSpace(stripMention(msg.Content, c.botID))
				sessionKey := "discord:" + msg.ChannelID
				if c.IsolatePeers {
					sessionKey += ":" + msg.Author.ID
				}
				attachments, markers := c.captureAttachments(ctx, sessionKey, msg.Attachments)
				content := rootchannels.ComposeMessageText(clean, markers)
				if content == "" {
					continue
				}
				meta := map[string]any{"channel_id": msg.ChannelID, "message_reference": msg.ID, "guild_id": msg.GuildID, "is_private": strings.TrimSpace(msg.GuildID) == ""}
				if len(attachments) > 0 {
					meta["attachments"] = attachments
				}
				if ok := eventBus.Publish(bus.Event{Type: bus.EventUserMessage, SessionKey: sessionKey, Channel: "discord", From: msg.Author.ID, Message: content, Meta: meta}); !ok {
					log.Printf("discord event dropped: queue unavailable for channel=%s user=%s", msg.ChannelID, msg.Author.ID)
				}
			}
		}
		select {
		case <-ctx.Done():
			return
		default:
		}
	}
}

func (c *Channel) requiresMention(msg inboundMessage) bool {
	if !c.Config.RequireMention {
		return false
	}
	if c.botID == "" {
		return false
	}
	return strings.TrimSpace(msg.GuildID) != ""
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
	return shared.DefaultHTTPClient()
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
	if resp.StatusCode == http.StatusTooManyRequests {
		return discordRateLimitError(resp)
	}
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
	if resp.StatusCode == http.StatusTooManyRequests {
		return discordRateLimitError(resp)
	}
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("discord api error: %s %s", resp.Status, string(body))
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (c *Channel) ingressDeduper() *rootchannels.IngressDeduplicator {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.dedupe == nil {
		c.dedupe = rootchannels.NewIngressDeduplicator(0)
	}
	return c.dedupe
}

func (c *Channel) setGuildName(id, name string) {
	id = strings.TrimSpace(id)
	name = strings.TrimSpace(name)
	if id == "" || name == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.guilds == nil {
		c.guilds = map[string]string{}
	}
	c.guilds[id] = name
}

func (c *Channel) setChannelName(id, name string) {
	id = strings.TrimSpace(id)
	name = strings.TrimSpace(name)
	if id == "" || name == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.channels == nil {
		c.channels = map[string]string{}
	}
	c.channels[id] = name
}

func (c *Channel) lookupGuildName(id string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return strings.TrimSpace(c.guilds[id])
}

func (c *Channel) lookupChannelName(id string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return strings.TrimSpace(c.channels[id])
}

func (c *Channel) recordRecentConversation(msg inboundMessage) {
	userDisplay := strings.TrimSpace(msg.Member.Nick)
	if userDisplay == "" {
		userDisplay = strings.TrimSpace(msg.Author.GlobalName)
	}
	if userDisplay == "" {
		userDisplay = strings.TrimSpace(msg.Author.Username)
	}
	if userDisplay == "" {
		userDisplay = strings.TrimSpace(msg.Author.ID)
	}
	channelName := c.lookupChannelName(msg.ChannelID)
	guildName := c.lookupGuildName(msg.GuildID)
	kind := "channel"
	displayName := "Discord channel"
	isPrivate := strings.TrimSpace(msg.GuildID) == ""
	if isPrivate {
		kind = "dm"
		displayName = "DM with " + userDisplay
	} else {
		switch {
		case channelName != "" && guildName != "":
			displayName = "#" + channelName + " in " + guildName
		case channelName != "":
			displayName = "#" + channelName
		case guildName != "":
			displayName = "Conversation in " + guildName
		default:
			displayName = "Discord channel " + strings.TrimSpace(msg.ChannelID)
		}
	}
	recordRecentConversation(c.Config.APIBase, c.Config.Token, RecentConversation{
		ChannelID:       strings.TrimSpace(msg.ChannelID),
		UserID:          strings.TrimSpace(msg.Author.ID),
		GuildID:         strings.TrimSpace(msg.GuildID),
		Kind:            kind,
		DisplayName:     displayName,
		UserDisplayName: userDisplay,
		ChannelName:     channelName,
		GuildName:       guildName,
		LastMessageAt:   parseDiscordUnixTime(msg.Timestamp),
		LastMessageText: strings.TrimSpace(stripMention(msg.Content, c.botID)),
		IsPrivate:       isPrivate,
	})
}

func discordDedupeKey(msg inboundMessage) string {
	if strings.TrimSpace(msg.ID) != "" {
		return msg.ID
	}
	if strings.TrimSpace(msg.ChannelID) == "" || strings.TrimSpace(msg.Author.ID) == "" {
		return ""
	}
	return strings.Join([]string{msg.ChannelID, msg.Author.ID, msg.Content}, "|")
}

func discordRateLimitError(resp *http.Response) error {
	if resp == nil {
		return rootchannels.FormatRateLimitError("discord", 0, "")
	}
	var payload struct {
		Message    string  `json:"message"`
		RetryAfter float64 `json:"retry_after"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&payload)
	return rootchannels.FormatRateLimitError("discord", time.Duration(payload.RetryAfter*float64(time.Second)), payload.Message)
}

func (c *Channel) captureAttachments(ctx context.Context, sessionKey string, refs []discordAttachment) ([]artifacts.Attachment, []string) {
	attachments := make([]artifacts.Attachment, 0, len(refs))
	markers := make([]string, 0, len(refs))
	for _, ref := range refs {
		filename := artifacts.NormalizeFilename(ref.Filename, ref.ContentType)
		kind := artifacts.DetectKind(filename, ref.ContentType)
		if c.MaxMediaBytes == 0 {
			markers = append(markers, artifacts.FailureMarker(kind, filename, "disabled by config"))
			continue
		}
		if c.MaxMediaBytes > 0 && ref.Size > int64(c.MaxMediaBytes) {
			markers = append(markers, artifacts.FailureMarker(kind, filename, "too large"))
			continue
		}
		if c.Artifacts == nil {
			markers = append(markers, artifacts.FailureMarker(kind, filename, "storage unavailable"))
			continue
		}
		data, err := c.downloadAttachment(ctx, ref.URL)
		if err != nil {
			markers = append(markers, artifacts.FailureMarker(kind, filename, "download failed"))
			continue
		}
		att, err := c.Artifacts.SaveNamed(ctx, sessionKey, filename, ref.ContentType, data)
		if err != nil {
			markers = append(markers, artifacts.FailureMarker(kind, filename, "save failed"))
			continue
		}
		attachments = append(attachments, att)
		markers = append(markers, artifacts.Marker(att))
	}
	return attachments, markers
}

func (c *Channel) downloadAttachment(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.client().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("discord attachment error: %s", resp.Status)
	}
	limit := int64(c.MaxMediaBytes)
	if limit <= 0 {
		limit = 25 << 20
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if c.MaxMediaBytes > 0 && len(data) > c.MaxMediaBytes {
		return nil, fmt.Errorf("discord attachment exceeds maxMediaBytes")
	}
	return data, nil
}

func (c *Channel) postMultipart(ctx context.Context, channelID, text string, mediaPaths []string, meta map[string]any) error {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	payload := map[string]any{}
	if strings.TrimSpace(text) != "" {
		payload["content"] = text
	}
	if replyID, ok := meta["message_reference"].(string); ok && replyID != "" {
		payload["message_reference"] = map[string]any{"message_id": replyID}
	}
	payloadJSON, _ := json.Marshal(payload)
	if err := writer.WriteField("payload_json", string(payloadJSON)); err != nil {
		return err
	}
	for i, mediaPath := range mediaPaths {
		if err := c.attachFilePart(writer, i, mediaPath); err != nil {
			return err
		}
	}
	if err := writer.Close(); err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.apiBase()+"/channels/"+channelID+"/messages", &body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bot "+c.Config.Token)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	resp, err := c.client().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("discord api error: %s %s", resp.Status, string(respBody))
	}
	return nil
}

func (c *Channel) attachFilePart(writer *multipart.Writer, index int, mediaPath string) error {
	info, err := os.Stat(mediaPath)
	if err != nil {
		return err
	}
	if c.MaxMediaBytes == 0 {
		return fmt.Errorf("media attachments disabled by config")
	}
	if c.MaxMediaBytes > 0 && info.Size() > int64(c.MaxMediaBytes) {
		return fmt.Errorf("media path exceeds maxMediaBytes: %s", mediaPath)
	}
	file, err := os.Open(mediaPath)
	if err != nil {
		return err
	}
	defer file.Close()
	part, err := writer.CreateFormFile(fmt.Sprintf("files[%d]", index), filepath.Base(mediaPath))
	if err != nil {
		return err
	}
	if _, err := io.Copy(part, file); err != nil {
		return err
	}
	return nil
}

func (c *Channel) allowedUser(ctx context.Context, user string) bool {
	return shared.AllowInboundIdentity(ctx, shared.InboundAccessInput{Policy: c.Config.InboundPolicy, OpenAccess: c.Config.OpenAccess, Allowlist: c.Config.AllowedUserIDs, Channel: "discord", Identity: user, Broker: c.ApprovalBroker})
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
	ID          string              `json:"id"`
	ChannelID   string              `json:"channel_id"`
	GuildID     string              `json:"guild_id"`
	Timestamp   string              `json:"timestamp"`
	Content     string              `json:"content"`
	Mentions    []mention           `json:"mentions"`
	Attachments []discordAttachment `json:"attachments"`
	Member      struct {
		Nick string `json:"nick"`
	} `json:"member"`
	Author struct {
		ID         string `json:"id"`
		Username   string `json:"username"`
		GlobalName string `json:"global_name"`
		Bot        bool   `json:"bot"`
	} `json:"author"`
}

type discordAttachment struct {
	URL         string `json:"url"`
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	Size        int64  `json:"size"`
}
