package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	rootchannels "or3-intern/internal/channels"
	"or3-intern/internal/artifacts"
	"or3-intern/internal/bus"
	"or3-intern/internal/config"
)

type Channel struct {
	Config config.TelegramChannelConfig
	HTTP   *http.Client
	Artifacts *artifacts.Store
	MaxMediaBytes int

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
	mediaPaths := rootchannels.MediaPaths(meta)
	if len(mediaPaths) > 0 {
		return c.deliverMedia(ctx, chatID, text, mediaPaths, meta)
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
		chatID := strconv.FormatInt(msg.Chat.ID, 10)
		if !c.allowedChat(chatID) {
			continue
		}
		sessionKey := "telegram:" + chatID
		text := strings.TrimSpace(msg.Text)
		if text == "" {
			text = strings.TrimSpace(msg.Caption)
		}
		attachments, markers := c.captureAttachments(ctx, sessionKey, msg)
		content := rootchannels.ComposeMessageText(text, markers)
		if content == "" {
			continue
		}
		meta := map[string]any{
			"chat_id":             chatID,
			"chat_type":           msg.Chat.Type,
			"message_id":          msg.MessageID,
			"reply_to_message_id": int64(msg.MessageID),
			"username":            msg.From.Username,
		}
		if msg.MediaGroupID != "" {
			meta["media_group_id"] = msg.MediaGroupID
		}
		if len(attachments) > 0 {
			meta["attachments"] = attachments
		}
		eventBus.Publish(bus.Event{
			Type:       bus.EventUserMessage,
			SessionKey: sessionKey,
			Channel:    "telegram",
			From:       strconv.FormatInt(msg.From.ID, 10),
			Message:    content,
			Meta:       meta,
		})
	}
	return nil
}

func (c *Channel) allowedChat(chatID string) bool {
	if len(c.Config.AllowedChatIDs) == 0 {
		return c.Config.OpenAccess
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

func (c *Channel) captureAttachments(ctx context.Context, sessionKey string, msg inboundMessage) ([]artifacts.Attachment, []string) {
	attachments := make([]artifacts.Attachment, 0, 4)
	markers := make([]string, 0, 4)

	// Telegram media groups are processed one update at a time in v1.
	if len(msg.Photo) > 0 {
		att, marker := c.captureRemoteAttachment(ctx, sessionKey, remoteAttachment{
			FileID:   msg.Photo[len(msg.Photo)-1].FileID,
			FileSize: msg.Photo[len(msg.Photo)-1].FileSize,
			Filename: "photo.jpg",
			Mime:     "image/jpeg",
			Kind:     artifacts.KindImage,
		})
		if marker != "" {
			markers = append(markers, marker)
		}
		if att.ArtifactID != "" {
			attachments = append(attachments, att)
		}
	}
	if msg.Voice.FileID != "" {
		filename := "voice.ogg"
		if msg.Voice.FileUniqueID != "" {
			filename = msg.Voice.FileUniqueID + ".ogg"
		}
		att, marker := c.captureRemoteAttachment(ctx, sessionKey, remoteAttachment{
			FileID:   msg.Voice.FileID,
			FileSize: msg.Voice.FileSize,
			Filename: filename,
			Mime:     "audio/ogg",
			Kind:     artifacts.KindAudio,
		})
		if marker != "" {
			markers = append(markers, marker)
		}
		if att.ArtifactID != "" {
			attachments = append(attachments, att)
		}
	}
	if msg.Audio.FileID != "" {
		att, marker := c.captureRemoteAttachment(ctx, sessionKey, remoteAttachment{
			FileID:   msg.Audio.FileID,
			FileSize: msg.Audio.FileSize,
			Filename: msg.Audio.FileName,
			Mime:     msg.Audio.MimeType,
			Kind:     artifacts.KindAudio,
		})
		if marker != "" {
			markers = append(markers, marker)
		}
		if att.ArtifactID != "" {
			attachments = append(attachments, att)
		}
	}
	if msg.Document.FileID != "" {
		att, marker := c.captureRemoteAttachment(ctx, sessionKey, remoteAttachment{
			FileID:   msg.Document.FileID,
			FileSize: msg.Document.FileSize,
			Filename: msg.Document.FileName,
			Mime:     msg.Document.MimeType,
			Kind:     artifacts.KindFile,
		})
		if marker != "" {
			markers = append(markers, marker)
		}
		if att.ArtifactID != "" {
			attachments = append(attachments, att)
		}
	}
	return attachments, markers
}

type remoteAttachment struct {
	FileID   string
	FileSize int64
	Filename string
	Mime     string
	Kind     string
}

func (c *Channel) captureRemoteAttachment(ctx context.Context, sessionKey string, remote remoteAttachment) (artifacts.Attachment, string) {
	filename := artifacts.NormalizeFilename(remote.Filename, remote.Mime)
	if remote.Kind == "" {
		remote.Kind = artifacts.DetectKind(filename, remote.Mime)
	}
	if c.MaxMediaBytes == 0 {
		return artifacts.Attachment{}, artifacts.FailureMarker(remote.Kind, filename, "disabled by config")
	}
	if c.MaxMediaBytes > 0 && remote.FileSize > int64(c.MaxMediaBytes) {
		return artifacts.Attachment{}, artifacts.FailureMarker(remote.Kind, filename, "too large")
	}
	if c.Artifacts == nil {
		return artifacts.Attachment{}, artifacts.FailureMarker(remote.Kind, filename, "storage unavailable")
	}
	info, err := c.getFile(ctx, remote.FileID)
	if err != nil {
		return artifacts.Attachment{}, artifacts.FailureMarker(remote.Kind, filename, "download failed")
	}
	if c.MaxMediaBytes > 0 && info.FileSize > int64(c.MaxMediaBytes) {
		return artifacts.Attachment{}, artifacts.FailureMarker(remote.Kind, filename, "too large")
	}
	data, err := c.downloadFile(ctx, info.FilePath)
	if err != nil {
		return artifacts.Attachment{}, artifacts.FailureMarker(remote.Kind, filename, "download failed")
	}
	att, err := c.Artifacts.SaveNamed(ctx, sessionKey, filename, firstNonEmpty(remote.Mime, mime.TypeByExtension(filepath.Ext(filename))), data)
	if err != nil {
		return artifacts.Attachment{}, artifacts.FailureMarker(remote.Kind, filename, "save failed")
	}
	return att, artifacts.Marker(att)
}

func (c *Channel) getFile(ctx context.Context, fileID string) (fileInfo, error) {
	var info fileInfo
	err := c.getJSON(ctx, "/getFile", map[string]string{"file_id": fileID}, &info)
	return info, err
}

func (c *Channel) downloadFile(ctx context.Context, filePath string) ([]byte, error) {
	endpoint := strings.TrimRight(strings.TrimSpace(c.Config.APIBase), "/")
	if endpoint == "" {
		endpoint = "https://api.telegram.org"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"/file/bot"+c.Config.Token+"/"+strings.TrimLeft(filePath, "/"), nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.client().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("telegram file error: %s", resp.Status)
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
		return nil, fmt.Errorf("telegram file exceeds maxMediaBytes")
	}
	return data, nil
}

func (c *Channel) deliverMedia(ctx context.Context, chatID, text string, mediaPaths []string, meta map[string]any) error {
	replyID := replyToMessageID(meta)
	for i, mediaPath := range mediaPaths {
		caption := ""
		if i == 0 {
			caption = text
		}
		if err := c.sendMediaFile(ctx, chatID, mediaPath, caption, replyID); err != nil {
			return err
		}
	}
	if strings.TrimSpace(text) != "" && len(mediaPaths) == 0 {
		return c.postJSON(ctx, "/sendMessage", map[string]any{"chat_id": chatID, "text": text}, nil)
	}
	return nil
}

func (c *Channel) sendMediaFile(ctx context.Context, chatID, mediaPath, caption string, replyID int64) error {
	endpoint, fieldName, mimeType := telegramSendSpec(mediaPath)
	file, err := os.Open(mediaPath)
	if err != nil {
		return err
	}
	defer file.Close()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("chat_id", chatID); err != nil {
		return err
	}
	if replyID > 0 {
		if err := writer.WriteField("reply_to_message_id", strconv.FormatInt(replyID, 10)); err != nil {
			return err
		}
	}
	if strings.TrimSpace(caption) != "" {
		if err := writer.WriteField("caption", caption); err != nil {
			return err
		}
	}
	part, err := writer.CreateFormFile(fieldName, filepath.Base(mediaPath))
	if err != nil {
		return err
	}
	if _, err := io.Copy(part, file); err != nil {
		return err
	}
	if err := writer.Close(); err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.apiBase()+endpoint, &body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	if mimeType != "" {
		req.Header.Set("X-Or3-Media-Type", mimeType)
	}
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
	return nil
}

func telegramSendSpec(path string) (endpoint string, fieldName string, mimeType string) {
	mimeType = mime.TypeByExtension(strings.ToLower(filepath.Ext(path)))
	switch artifacts.DetectKind(path, mimeType) {
	case artifacts.KindImage:
		return "/sendPhoto", "photo", mimeType
	case artifacts.KindAudio:
		if strings.HasSuffix(strings.ToLower(path), ".ogg") || strings.HasSuffix(strings.ToLower(path), ".opus") {
			return "/sendVoice", "voice", mimeType
		}
		return "/sendAudio", "audio", mimeType
	default:
		return "/sendDocument", "document", mimeType
	}
}

func replyToMessageID(meta map[string]any) int64 {
	switch v := meta["reply_to_message_id"].(type) {
	case int64:
		return v
	case int:
		return int64(v)
	case float64:
		return int64(v)
	default:
		return 0
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
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
	MessageID    int    `json:"message_id"`
	Text         string `json:"text"`
	Caption      string `json:"caption"`
	MediaGroupID string `json:"media_group_id"`
	Chat      struct {
		ID   int64  `json:"id"`
		Type string `json:"type"`
	} `json:"chat"`
	From struct {
		ID       int64  `json:"id"`
		Username string `json:"username"`
	} `json:"from"`
	Photo []struct {
		FileID   string `json:"file_id"`
		FileSize int64  `json:"file_size"`
	} `json:"photo"`
	Voice struct {
		FileID       string `json:"file_id"`
		FileUniqueID string `json:"file_unique_id"`
		FileSize     int64  `json:"file_size"`
	} `json:"voice"`
	Audio struct {
		FileID   string `json:"file_id"`
		FileName string `json:"file_name"`
		MimeType string `json:"mime_type"`
		FileSize int64  `json:"file_size"`
	} `json:"audio"`
	Document struct {
		FileID   string `json:"file_id"`
		FileName string `json:"file_name"`
		MimeType string `json:"mime_type"`
		FileSize int64  `json:"file_size"`
	} `json:"document"`
}

type fileInfo struct {
	FilePath string `json:"file_path"`
	FileSize int64  `json:"file_size"`
}
