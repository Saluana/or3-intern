package telegram

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strconv"
	"strings"
	"sync"
)

const maxRecentChatsPerBot = 50

// RecentChat is a lightweight, in-memory record of chats the live Telegram
// poller has seen. It lets setup UI show known chats even after getUpdates has
// consumed Telegram's update queue.
type RecentChat struct {
	ID              string
	Type            string
	Title           string
	Username        string
	DisplayName     string
	LastMessageAt   int64
	LastMessageText string
}

var recentChats = struct {
	sync.Mutex
	byBot map[string]map[string]RecentChat
}{byBot: map[string]map[string]RecentChat{}}

func recordRecentChat(apiBase, token string, msg inboundMessage) {
	if msg.Chat.ID == 0 {
		return
	}
	botKey := recentBotKey(apiBase, token)
	if botKey == "" {
		return
	}
	chat := recentChatFromMessage(msg)
	recentChats.Lock()
	defer recentChats.Unlock()
	items := recentChats.byBot[botKey]
	if items == nil {
		items = map[string]RecentChat{}
		recentChats.byBot[botKey] = items
	}
	if existing, ok := items[chat.ID]; ok && existing.LastMessageAt > chat.LastMessageAt {
		return
	}
	items[chat.ID] = chat
	if len(items) <= maxRecentChatsPerBot {
		return
	}
	oldestID := ""
	var oldest int64
	for id, item := range items {
		if oldestID == "" || item.LastMessageAt < oldest {
			oldestID = id
			oldest = item.LastMessageAt
		}
	}
	delete(items, oldestID)
}

// RecentChats returns known chats for this bot, newest first.
func RecentChats(apiBase, token string, limit int) []RecentChat {
	botKey := recentBotKey(apiBase, token)
	if botKey == "" {
		return nil
	}
	recentChats.Lock()
	items := recentChats.byBot[botKey]
	out := make([]RecentChat, 0, len(items))
	for _, item := range items {
		out = append(out, item)
	}
	recentChats.Unlock()
	sort.Slice(out, func(i, j int) bool {
		if out[i].LastMessageAt != out[j].LastMessageAt {
			return out[i].LastMessageAt > out[j].LastMessageAt
		}
		return out[i].ID < out[j].ID
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

func clearRecentChatsForTest() {
	recentChats.Lock()
	defer recentChats.Unlock()
	recentChats.byBot = map[string]map[string]RecentChat{}
}

func recentBotKey(apiBase, token string) string {
	token = strings.TrimSpace(token)
	if token == "" {
		return ""
	}
	base := strings.TrimRight(strings.TrimSpace(apiBase), "/")
	if base == "" {
		base = "https://api.telegram.org"
	}
	sum := sha256.Sum256([]byte(base + "\x00" + token))
	return hex.EncodeToString(sum[:])
}

func recentChatFromMessage(msg inboundMessage) RecentChat {
	name := strings.TrimSpace(msg.Chat.Title)
	if name == "" {
		name = strings.TrimSpace(strings.TrimSpace(msg.Chat.FirstName + " " + msg.Chat.LastName))
	}
	if name == "" && strings.TrimSpace(msg.Chat.Username) != "" {
		name = "@" + strings.TrimSpace(msg.Chat.Username)
	}
	if name == "" {
		name = strconv.FormatInt(msg.Chat.ID, 10)
	}
	preview := strings.TrimSpace(msg.Text)
	if preview == "" {
		preview = strings.TrimSpace(msg.Caption)
	}
	return RecentChat{
		ID:              strconv.FormatInt(msg.Chat.ID, 10),
		Type:            msg.Chat.Type,
		Title:           msg.Chat.Title,
		Username:        msg.Chat.Username,
		DisplayName:     name,
		LastMessageAt:   msg.Date,
		LastMessageText: preview,
	}
}
