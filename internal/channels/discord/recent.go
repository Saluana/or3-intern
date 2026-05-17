package discord

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"
	"sync"
	"time"
)

const maxRecentConversationsPerBot = 75

type RecentConversation struct {
	ChannelID       string
	UserID          string
	GuildID         string
	Kind            string
	DisplayName     string
	UserDisplayName string
	ChannelName     string
	GuildName       string
	LastMessageAt   int64
	LastMessageText string
	IsPrivate       bool
}

var recentConversations = struct {
	sync.Mutex
	byBot map[string]map[string]RecentConversation
}{byBot: map[string]map[string]RecentConversation{}}

func recordRecentConversation(apiBase, token string, item RecentConversation) {
	item.ChannelID = strings.TrimSpace(item.ChannelID)
	if item.ChannelID == "" {
		return
	}
	botKey := recentDiscordBotKey(apiBase, token)
	if botKey == "" {
		return
	}
	recentConversations.Lock()
	defer recentConversations.Unlock()
	items := recentConversations.byBot[botKey]
	if items == nil {
		items = map[string]RecentConversation{}
		recentConversations.byBot[botKey] = items
	}
	if existing, ok := items[item.ChannelID]; ok && existing.LastMessageAt > item.LastMessageAt {
		return
	}
	items[item.ChannelID] = item
	if len(items) <= maxRecentConversationsPerBot {
		return
	}
	oldestID := ""
	var oldest int64
	for id, current := range items {
		if oldestID == "" || current.LastMessageAt < oldest {
			oldestID = id
			oldest = current.LastMessageAt
		}
	}
	delete(items, oldestID)
}

func RecentConversations(apiBase, token string, limit int) []RecentConversation {
	botKey := recentDiscordBotKey(apiBase, token)
	if botKey == "" {
		return nil
	}
	recentConversations.Lock()
	items := recentConversations.byBot[botKey]
	out := make([]RecentConversation, 0, len(items))
	for _, item := range items {
		out = append(out, item)
	}
	recentConversations.Unlock()
	sort.Slice(out, func(i, j int) bool {
		if out[i].LastMessageAt != out[j].LastMessageAt {
			return out[i].LastMessageAt > out[j].LastMessageAt
		}
		return out[i].ChannelID < out[j].ChannelID
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

func clearRecentConversationsForTest() {
	recentConversations.Lock()
	defer recentConversations.Unlock()
	recentConversations.byBot = map[string]map[string]RecentConversation{}
}

func recentDiscordBotKey(apiBase, token string) string {
	token = strings.TrimSpace(token)
	if token == "" {
		return ""
	}
	base := strings.TrimRight(strings.TrimSpace(apiBase), "/")
	if base == "" {
		base = "https://discord.com/api/v10"
	}
	sum := sha256.Sum256([]byte(base + "\x00" + token))
	return hex.EncodeToString(sum[:])
}

func parseDiscordUnixTime(raw string) int64 {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0
	}
	parsed, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return 0
	}
	return parsed.Unix()
}
