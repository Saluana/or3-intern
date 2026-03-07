package slack

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
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

type Channel struct {
	Config        config.SlackChannelConfig
	HTTP          *http.Client
	Dialer        *websocket.Dialer
	Artifacts     *artifacts.Store
	MaxMediaBytes int

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
		if ev.BotID != "" || ev.User == "" {
			continue
		}
		if !c.allowedUser(ev.User) {
			continue
		}
		if envelope.Payload.Authorizations[0].UserID != "" && c.botID == "" {
			c.botID = envelope.Payload.Authorizations[0].UserID
		}
		if c.Config.RequireMention && c.botID != "" && !strings.Contains(ev.Text, "<@"+c.botID+">") && len(ev.Files) == 0 {
			continue
		}
		clean := strings.TrimSpace(strings.ReplaceAll(ev.Text, "<@"+c.botID+">", ""))
		sessionKey := "slack:" + ev.Channel
		attachments, markers := c.captureFiles(ctx, sessionKey, ev.Files)
		content := rootchannels.ComposeMessageText(clean, markers)
		if content == "" {
			continue
		}
		meta := map[string]any{"channel_id": ev.Channel, "thread_ts": ev.ThreadTS}
		if len(attachments) > 0 {
			meta["attachments"] = attachments
		}
		eventBus.Publish(bus.Event{Type: bus.EventUserMessage, SessionKey: sessionKey, Channel: "slack", From: ev.User, Message: content, Meta: meta})
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
	if resp.StatusCode >= 300 {
		return fmt.Errorf("slack api error: %s", resp.Status)
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
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
			Type     string      `json:"type"`
			Text     string      `json:"text"`
			User     string      `json:"user"`
			BotID    string      `json:"bot_id"`
			Channel  string      `json:"channel"`
			ThreadTS string      `json:"thread_ts"`
			Files    []slackFile `json:"files"`
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
