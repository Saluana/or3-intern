package main

import (
	"net/http"
	"sort"
	"strconv"
	"strings"

	discordchannel "or3-intern/internal/channels/discord"
)

type serviceDiscordTargetCandidate struct {
	ChannelID       string `json:"channelId"`
	UserID          string `json:"userId,omitempty"`
	GuildID         string `json:"guildId,omitempty"`
	Kind            string `json:"kind,omitempty"`
	DisplayName     string `json:"displayName"`
	UserDisplayName string `json:"userDisplayName,omitempty"`
	ChannelName     string `json:"channelName,omitempty"`
	GuildName       string `json:"guildName,omitempty"`
	LastMessageAt   int64  `json:"lastMessageAt,omitempty"`
	LastMessageText string `json:"lastMessageText,omitempty"`
	IsPrivate       bool   `json:"isPrivate,omitempty"`
}

func (s *serviceServer) handleConfigureDiscordTargets(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		return
	}
	limitServiceRequestBody(w, r, serviceConfigureBodyLimit)
	token := strings.TrimSpace(s.config.Channels.Discord.Token)
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
	limit := serviceParsePositiveInt(r.URL.Query().Get("limit"), 20, 100)
	items := s.knownDiscordTargetCandidates(token, limit)
	if token == "" {
		if len(items) > 0 {
			writeServiceJSON(w, http.StatusOK, map[string]any{"items": items, "warning": "Add and save a Discord bot token, then restart or3-intern to discover recent conversations."})
			return
		}
		writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": "Paste a Discord bot token first, save it, restart or3-intern, then message the bot and try again."})
		return
	}
	writeServiceJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *serviceServer) knownDiscordTargetCandidates(token string, limit int) []serviceDiscordTargetCandidate {
	items := make([]serviceDiscordTargetCandidate, 0)
	defaultID := strings.TrimSpace(s.config.Channels.Discord.DefaultChannelID)
	if defaultID != "" {
		items = append(items, serviceDiscordTargetCandidate{ChannelID: defaultID, Kind: "saved", DisplayName: "Primary Discord destination", LastMessageText: "Saved in OR3 settings."})
	}
	for _, item := range discordchannel.RecentConversations(s.config.Channels.Discord.APIBase, token, limit) {
		items = append(items, serviceDiscordTargetCandidate{
			ChannelID:       item.ChannelID,
			UserID:          item.UserID,
			GuildID:         item.GuildID,
			Kind:            item.Kind,
			DisplayName:     item.DisplayName,
			UserDisplayName: item.UserDisplayName,
			ChannelName:     item.ChannelName,
			GuildName:       item.GuildName,
			LastMessageAt:   item.LastMessageAt,
			LastMessageText: item.LastMessageText,
			IsPrivate:       item.IsPrivate,
		})
	}
	return mergeDiscordTargetCandidates(nil, items, limit)
}

func mergeDiscordTargetCandidates(base []serviceDiscordTargetCandidate, next []serviceDiscordTargetCandidate, limit int) []serviceDiscordTargetCandidate {
	byChannelID := map[string]serviceDiscordTargetCandidate{}
	for _, item := range append(base, next...) {
		item.ChannelID = strings.TrimSpace(item.ChannelID)
		if item.ChannelID == "" {
			continue
		}
		if strings.TrimSpace(item.DisplayName) == "" {
			item.DisplayName = item.ChannelID
		}
		if existing, ok := byChannelID[item.ChannelID]; ok {
			if existing.LastMessageAt > item.LastMessageAt {
				continue
			}
			if item.LastMessageAt == 0 && existing.LastMessageAt == 0 && existing.DisplayName != existing.ChannelID {
				continue
			}
		}
		byChannelID[item.ChannelID] = item
	}
	items := make([]serviceDiscordTargetCandidate, 0, len(byChannelID))
	for _, item := range byChannelID {
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].LastMessageAt != items[j].LastMessageAt {
			return items[i].LastMessageAt > items[j].LastMessageAt
		}
		if items[i].DisplayName != items[j].DisplayName {
			return items[i].DisplayName == "Primary Discord destination"
		}
		if items[i].Kind != items[j].Kind {
			return items[i].Kind == "saved"
		}
		return items[i].ChannelID < items[j].ChannelID
	})
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	return items
}
