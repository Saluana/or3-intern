package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

type serviceTelegramChatCandidate struct {
	ID              string `json:"id"`
	Type            string `json:"type,omitempty"`
	Title           string `json:"title,omitempty"`
	Username        string `json:"username,omitempty"`
	DisplayName     string `json:"displayName"`
	LastMessageAt   int64  `json:"lastMessageAt,omitempty"`
	LastMessageText string `json:"lastMessageText,omitempty"`
}

type serviceTelegramUpdatesEnvelope struct {
	OK          bool                    `json:"ok"`
	Description string                  `json:"description"`
	Result      []serviceTelegramUpdate `json:"result"`
}

type serviceTelegramUpdate struct {
	UpdateID int64                   `json:"update_id"`
	Message  *serviceTelegramMessage `json:"message"`
}

type serviceTelegramMessage struct {
	MessageID int64               `json:"message_id"`
	Date      int64               `json:"date"`
	Text      string              `json:"text"`
	Caption   string              `json:"caption"`
	Chat      serviceTelegramChat `json:"chat"`
}

type serviceTelegramChat struct {
	ID        int64  `json:"id"`
	Type      string `json:"type"`
	Title     string `json:"title"`
	Username  string `json:"username"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
}

func (s *serviceServer) handleConfigureTelegramChats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		return
	}
	limitServiceRequestBody(w, r, serviceConfigureBodyLimit)
	token := strings.TrimSpace(s.config.Channels.Telegram.Token)
	if r.Method == http.MethodPost {
		var body struct {
			Token string `json:"token"`
			Limit int    `json:"limit"`
		}
		if err := decodeServiceRequestBody(r.Body, &body); err != nil {
			writeServiceRequestDecodeError(w, err)
			return
		}
		if strings.TrimSpace(body.Token) != "" && !strings.EqualFold(strings.TrimSpace(body.Token), secretDisplay("set")) {
			token = strings.TrimSpace(body.Token)
		}
		if body.Limit > 0 {
			r.URL.RawQuery = "limit=" + strconv.Itoa(body.Limit)
		}
	}
	if token == "" {
		writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": "Paste a Telegram bot token first, then message the bot and try discovery again."})
		return
	}
	limit := serviceParsePositiveInt(r.URL.Query().Get("limit"), 20, 100)
	items, err := s.discoverTelegramChats(r.Context(), token, s.config.Channels.Telegram.APIBase, limit)
	if err != nil {
		writeServiceError(w, r, http.StatusBadGateway, "telegram chat discovery failed", err)
		return
	}
	writeServiceJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *serviceServer) discoverTelegramChats(ctx context.Context, token, apiBase string, limit int) ([]serviceTelegramChatCandidate, error) {
	base := strings.TrimRight(strings.TrimSpace(apiBase), "/")
	if base == "" {
		base = "https://api.telegram.org"
	}
	endpoint, err := url.Parse(base + "/bot" + token + "/getUpdates")
	if err != nil {
		return nil, err
	}
	query := endpoint.Query()
	query.Set("timeout", "0")
	query.Set("limit", strconv.Itoa(limit))
	endpoint.RawQuery = query.Encode()

	reqCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("telegram api error: %s", resp.Status)
	}
	var envelope serviceTelegramUpdatesEnvelope
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return nil, err
	}
	if !envelope.OK {
		if strings.TrimSpace(envelope.Description) == "" {
			envelope.Description = "unknown Telegram API error"
		}
		return nil, fmt.Errorf("telegram api error: %s", envelope.Description)
	}
	byID := map[string]serviceTelegramChatCandidate{}
	for _, update := range envelope.Result {
		if update.Message == nil || update.Message.Chat.ID == 0 {
			continue
		}
		candidate := telegramChatCandidateFromMessage(*update.Message)
		if existing, ok := byID[candidate.ID]; ok && existing.LastMessageAt > candidate.LastMessageAt {
			continue
		}
		byID[candidate.ID] = candidate
	}
	items := make([]serviceTelegramChatCandidate, 0, len(byID))
	for _, item := range byID {
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].LastMessageAt != items[j].LastMessageAt {
			return items[i].LastMessageAt > items[j].LastMessageAt
		}
		return items[i].ID < items[j].ID
	})
	return items, nil
}

func telegramChatCandidateFromMessage(msg serviceTelegramMessage) serviceTelegramChatCandidate {
	chat := msg.Chat
	name := strings.TrimSpace(chat.Title)
	if name == "" {
		name = strings.TrimSpace(strings.TrimSpace(chat.FirstName + " " + chat.LastName))
	}
	if name == "" && chat.Username != "" {
		name = "@" + chat.Username
	}
	if name == "" {
		name = strconv.FormatInt(chat.ID, 10)
	}
	preview := strings.TrimSpace(msg.Text)
	if preview == "" {
		preview = strings.TrimSpace(msg.Caption)
	}
	return serviceTelegramChatCandidate{
		ID:              strconv.FormatInt(chat.ID, 10),
		Type:            chat.Type,
		Title:           chat.Title,
		Username:        chat.Username,
		DisplayName:     name,
		LastMessageAt:   msg.Date,
		LastMessageText: preview,
	}
}

func serviceParsePositiveInt(raw string, fallback, max int) int {
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || value <= 0 {
		return fallback
	}
	if max > 0 && value > max {
		return max
	}
	return value
}
