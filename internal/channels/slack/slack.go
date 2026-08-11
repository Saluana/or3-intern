// Package slack implements the Slack socket-mode channel adapter.
package slack

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
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

// Channel receives Slack events over Socket Mode and sends outbound messages.
type Channel struct {
	Config         config.SlackChannelConfig
	HTTP           *http.Client
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
	botID  string
	dedupe *rootchannels.IngressDeduplicator

	// reconnectDelay is test-only injection for prompt deterministic retries.
	reconnectDelay func(int) time.Duration
}

// Name returns the registered channel name.
func (c *Channel) Name() string { return "slack" }

// Start opens the Socket Mode connection and begins reading events.
func (c *Channel) Start(ctx context.Context, eventBus *bus.Bus) error {
	if strings.TrimSpace(c.Config.AppToken) == "" || strings.TrimSpace(c.Config.BotToken) == "" {
		return fmt.Errorf("slack tokens not configured")
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
		return fmt.Errorf("slack start interrupted")
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

// Stop closes the Socket Mode connection.
func (c *Channel) Stop(ctx context.Context) error {
	c.mu.Lock()
	cancel := c.cancel
	conn := c.conn
	done := c.done
	c.cancel = nil
	c.conn = nil
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

// ConnectionStatus reports the current Socket Mode state for diagnostics.
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

// Deliver posts a Slack message or uploads media attachments.
func (c *Channel) Deliver(ctx context.Context, to, text string, meta map[string]any) error {
	channelID := strings.TrimSpace(to)
	if channelID == "" {
		channelID = strings.TrimSpace(c.Config.DefaultChannelID)
	}
	if channelID == "" {
		return fmt.Errorf("slack channel id required")
	}
	mediaPaths := rootchannels.MediaPaths(meta)
	if len(mediaPaths) > 0 {
		return c.uploadFiles(ctx, channelID, text, mediaPaths, meta)
	}
	payload := map[string]any{"channel": channelID, "text": text}
	if threadTS, ok := meta["thread_ts"].(string); ok && threadTS != "" {
		payload["thread_ts"] = threadTS
	}
	return c.postJSON(ctx, c.apiBase()+"/chat.postMessage", c.Config.BotToken, payload, nil)
}

func (c *Channel) connect(ctx context.Context) (*websocket.Conn, error) {
	url, err := c.openSocketURL(ctx)
	if err != nil {
		return nil, err
	}
	conn, _, err := shared.DialWebSocketContext(ctx, c.Dialer, url, nil)
	return conn, err
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
		if !slackRetryable(err) {
			log.Printf("slack socket mode stopped: %v", err)
			return
		}
		c.setConnectionState(done, nil, "reconnecting")
		for {
			delay := c.nextReconnectDelay(attempt)
			attempt++
			log.Printf("slack socket mode disconnected: %v; reconnecting in %s", err, delay.Round(time.Millisecond))
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
			if !slackRetryable(err) {
				log.Printf("slack socket mode stopped: %v", err)
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

func slackRetryable(err error) bool {
	return err != nil && !shared.IsPermanentConnectionError(err)
}

func (c *Channel) readConnection(ctx context.Context, eventBus *bus.Bus, conn *websocket.Conn) error {
	for {
		_, raw, err := conn.ReadMessage()
		if err != nil {
			return err
		}
		var envelope socketEnvelope
		if err := json.Unmarshal(raw, &envelope); err != nil {
			continue
		}
		if envelope.EnvelopeID != "" {
			if err := conn.WriteJSON(map[string]any{"envelope_id": envelope.EnvelopeID}); err != nil {
				return err
			}
		}
		if envelope.Type == "hello" {
			continue
		}
		if envelope.Type != "events_api" || envelope.Payload.Event.Type != "message" {
			continue
		}
		if key := slackDedupeKey(envelope); key != "" && c.ingressDeduper().IsDuplicate(key) {
			continue
		}
		ev := envelope.Payload.Event
		if ev.BotID != "" || ev.User == "" {
			continue
		}
		if !c.allowedUser(ctx, ev.User) {
			continue
		}
		if len(envelope.Payload.Authorizations) > 0 && envelope.Payload.Authorizations[0].UserID != "" && c.botID == "" {
			c.botID = envelope.Payload.Authorizations[0].UserID
		}
		if c.Config.RequireMention && c.botID != "" && !strings.Contains(ev.Text, "<@"+c.botID+">") && len(ev.Files) == 0 {
			continue
		}
		clean := strings.TrimSpace(strings.ReplaceAll(ev.Text, "<@"+c.botID+">", ""))
		sessionKey := "slack:" + ev.Channel
		if c.IsolatePeers {
			sessionKey += ":" + ev.User
		}
		attachments, markers := c.captureFiles(ctx, sessionKey, ev.Files)
		content := rootchannels.ComposeMessageText(clean, markers)
		if content == "" {
			continue
		}
		meta := map[string]any{"channel_id": ev.Channel, "thread_ts": ev.ThreadTS, "channel_type": ev.ChannelType}
		if len(attachments) > 0 {
			meta["attachments"] = attachments
		}
		if ok := eventBus.Publish(bus.Event{Type: bus.EventUserMessage, SessionKey: sessionKey, Channel: "slack", From: ev.User, Message: content, Meta: meta}); !ok {
			log.Printf("slack event dropped: queue unavailable for channel=%s user=%s", ev.Channel, ev.User)
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
	}
}

func (c *Channel) openSocketURL(ctx context.Context) (string, error) {
	var resp struct {
		OK    bool   `json:"ok"`
		URL   string `json:"url"`
		Error string `json:"error"`
	}
	if err := c.postJSON(ctx, c.apiBase()+"/apps.connections.open", c.Config.AppToken, nil, &resp); err != nil {
		return "", err
	}
	if !resp.OK || resp.URL == "" {
		if code := strings.TrimSpace(resp.Error); code != "" {
			if shared.IsPermanentConnectionError(fmt.Errorf("%s", code)) {
				return "", fmt.Errorf("slack socket authentication failed: %s", code)
			}
			return "", fmt.Errorf("slack socket url unavailable: %s", code)
		}
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
	return shared.DefaultHTTPClient()
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
	if resp.StatusCode == http.StatusTooManyRequests {
		return rootchannels.FormatRateLimitError("slack", rootchannels.ParseRetryAfterSeconds(resp.Header.Get("Retry-After")), "")
	}
	if resp.StatusCode >= 300 {
		return fmt.Errorf("slack api error: %s", resp.Status)
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (c *Channel) postForm(ctx context.Context, endpoint, token string, values url.Values, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(values.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := c.client().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusTooManyRequests {
		return rootchannels.FormatRateLimitError("slack", rootchannels.ParseRetryAfterSeconds(resp.Header.Get("Retry-After")), "")
	}
	if resp.StatusCode >= 300 {
		return fmt.Errorf("slack api error: %s", resp.Status)
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

func slackDedupeKey(envelope socketEnvelope) string {
	if strings.TrimSpace(envelope.EnvelopeID) != "" {
		return envelope.EnvelopeID
	}
	ev := envelope.Payload.Event
	if strings.TrimSpace(ev.Channel) == "" || strings.TrimSpace(ev.User) == "" {
		return ""
	}
	return strings.Join([]string{ev.Channel, ev.User, ev.ThreadTS, ev.Text}, "|")
}

func (c *Channel) captureFiles(ctx context.Context, sessionKey string, files []slackFile) ([]artifacts.Attachment, []string) {
	attachments := make([]artifacts.Attachment, 0, len(files))
	markers := make([]string, 0, len(files))
	for _, file := range files {
		filename := artifacts.NormalizeFilename(file.Name, file.Mimetype)
		kind := artifacts.DetectKind(filename, file.Mimetype)
		if c.MaxMediaBytes == 0 {
			markers = append(markers, artifacts.FailureMarker(kind, filename, "disabled by config"))
			continue
		}
		if c.MaxMediaBytes > 0 && file.Size > int64(c.MaxMediaBytes) {
			markers = append(markers, artifacts.FailureMarker(kind, filename, "too large"))
			continue
		}
		if c.Artifacts == nil {
			markers = append(markers, artifacts.FailureMarker(kind, filename, "storage unavailable"))
			continue
		}
		data, err := c.downloadPrivateFile(ctx, firstNonEmpty(file.URLPrivateDownload, file.URLPrivate))
		if err != nil {
			markers = append(markers, artifacts.FailureMarker(kind, filename, "download failed"))
			continue
		}
		att, err := c.Artifacts.SaveNamed(ctx, sessionKey, filename, file.Mimetype, data)
		if err != nil {
			markers = append(markers, artifacts.FailureMarker(kind, filename, "save failed"))
			continue
		}
		attachments = append(attachments, att)
		markers = append(markers, artifacts.Marker(att))
	}
	return attachments, markers
}

func (c *Channel) downloadPrivateFile(ctx context.Context, endpoint string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.Config.BotToken)
	resp, err := c.client().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("slack file download error: %s", resp.Status)
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
		return nil, fmt.Errorf("slack file exceeds maxMediaBytes")
	}
	return data, nil
}

func (c *Channel) uploadFiles(ctx context.Context, channelID, text string, mediaPaths []string, meta map[string]any) error {
	files := make([]map[string]any, 0, len(mediaPaths))
	for _, mediaPath := range mediaPaths {
		fileID, title, err := c.uploadFile(ctx, mediaPath)
		if err != nil {
			return err
		}
		files = append(files, map[string]any{"id": fileID, "title": title})
	}
	payload := map[string]any{
		"channel_id": channelID,
		"files":      files,
	}
	if strings.TrimSpace(text) != "" {
		payload["initial_comment"] = text
	}
	if threadTS, ok := meta["thread_ts"].(string); ok && threadTS != "" {
		payload["thread_ts"] = threadTS
	}
	var resp struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	if err := c.postJSON(ctx, c.apiBase()+"/files.completeUploadExternal", c.Config.BotToken, payload, &resp); err != nil {
		return err
	}
	if !resp.OK {
		return fmt.Errorf("slack complete upload failed: %s", resp.Error)
	}
	return nil
}

func (c *Channel) uploadFile(ctx context.Context, mediaPath string) (string, string, error) {
	info, err := os.Stat(mediaPath)
	if err != nil {
		return "", "", err
	}
	if c.MaxMediaBytes == 0 {
		return "", "", fmt.Errorf("media attachments disabled by config")
	}
	if c.MaxMediaBytes > 0 && info.Size() > int64(c.MaxMediaBytes) {
		return "", "", fmt.Errorf("media path exceeds maxMediaBytes: %s", mediaPath)
	}
	var start struct {
		OK        bool   `json:"ok"`
		UploadURL string `json:"upload_url"`
		FileID    string `json:"file_id"`
		Error     string `json:"error"`
	}
	form := url.Values{}
	form.Set("filename", filepath.Base(mediaPath))
	form.Set("length", fmt.Sprintf("%d", info.Size()))
	if err := c.postForm(ctx, c.apiBase()+"/files.getUploadURLExternal", c.Config.BotToken, form, &start); err != nil {
		return "", "", err
	}
	if !start.OK || start.UploadURL == "" || start.FileID == "" {
		return "", "", fmt.Errorf("slack upload init failed: %s", start.Error)
	}
	file, err := os.Open(mediaPath)
	if err != nil {
		return "", "", err
	}
	defer file.Close()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, start.UploadURL, file)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	resp, err := c.client().Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return "", "", fmt.Errorf("slack upload error: %s", resp.Status)
	}
	return start.FileID, filepath.Base(mediaPath), nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func (c *Channel) allowedUser(ctx context.Context, user string) bool {
	return shared.AllowInboundIdentity(ctx, shared.InboundAccessInput{Policy: c.Config.InboundPolicy, OpenAccess: c.Config.OpenAccess, Allowlist: c.Config.AllowedUserIDs, Channel: "slack", Identity: user, Broker: c.ApprovalBroker})
}

type socketEnvelope struct {
	EnvelopeID string `json:"envelope_id"`
	Type       string `json:"type"`
	Payload    struct {
		Event struct {
			Type        string      `json:"type"`
			Text        string      `json:"text"`
			User        string      `json:"user"`
			BotID       string      `json:"bot_id"`
			Channel     string      `json:"channel"`
			ChannelType string      `json:"channel_type"`
			ThreadTS    string      `json:"thread_ts"`
			Files       []slackFile `json:"files"`
		} `json:"event"`
		Authorizations []struct {
			UserID string `json:"user_id"`
		} `json:"authorizations"`
	} `json:"payload"`
}

type slackFile struct {
	ID                 string `json:"id"`
	Name               string `json:"name"`
	Mimetype           string `json:"mimetype"`
	Filetype           string `json:"filetype"`
	Size               int64  `json:"size"`
	URLPrivate         string `json:"url_private"`
	URLPrivateDownload string `json:"url_private_download"`
}
