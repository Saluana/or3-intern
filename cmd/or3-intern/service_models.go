package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"or3-intern/internal/config"
	"or3-intern/internal/providers"
)

func (s *serviceServer) handleConfigureModels(w http.ResponseWriter, r *http.Request) {
	provider := serviceNormalizeProviderKey(r.URL.Query().Get("provider"))
	if provider == "" {
		writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": "provider is required"})
		return
	}
	kind := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("kind")))
	if kind == "" {
		kind = "chat"
	}
	refresh := r.URL.Query().Get("refresh") == "1" || strings.EqualFold(r.URL.Query().Get("refresh"), "true")
	category := strings.TrimSpace(r.URL.Query().Get("category"))
	userFiltered := r.URL.Query().Get("user") == "1" || strings.EqualFold(r.URL.Query().Get("user"), "true")
	items, fetchedAt, err := s.configureModelCatalog(r.Context(), provider, kind, category, userFiltered, refresh)
	if err != nil {
		writeServiceJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
		return
	}
	writeServiceJSON(w, http.StatusOK, map[string]any{
		"provider":  provider,
		"kind":      kind,
		"fetchedAt": fetchedAt.Format(time.RFC3339),
		"items":     items,
	})
}

func (s *serviceServer) configureModelCatalog(ctx context.Context, provider, kind, category string, userFiltered, refresh bool) ([]serviceModelCatalogItem, time.Time, error) {
	cacheKey := strings.Join([]string{provider, kind, category, strconv.FormatBool(userFiltered)}, "|")
	now := time.Now().UTC()
	if entry, ok := s.serviceModelCatalog().Get(cacheKey, now, refresh); ok {
		return entry.Items, entry.FetchedAt, nil
	}

	items, err := s.fetchConfigureModelCatalog(ctx, provider, kind, category, userFiltered)
	if err != nil {
		return nil, time.Time{}, err
	}
	s.serviceModelCatalog().Put(cacheKey, serviceModelCatalogCacheEntry{FetchedAt: now, Items: items})
	return items, now, nil
}

func (s *serviceServer) serviceModelCatalog() *serviceModelCatalogCache {
	s.components()
	return s.modelCatalog
}

type serviceModelCatalogCache struct {
	mu      sync.Mutex
	max     int
	ttl     time.Duration
	entries map[string]serviceModelCatalogCacheEntry
}

func newServiceModelCatalogCache(max int, ttl time.Duration) *serviceModelCatalogCache {
	return &serviceModelCatalogCache{max: max, ttl: ttl, entries: map[string]serviceModelCatalogCacheEntry{}}
}

func (c *serviceModelCatalogCache) Get(key string, now time.Time, refresh bool) (serviceModelCatalogCacheEntry, bool) {
	if c == nil || refresh {
		return serviceModelCatalogCacheEntry{}, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[key]
	if !ok || now.Sub(entry.FetchedAt) >= c.ttl {
		if ok {
			delete(c.entries, key)
		}
		return serviceModelCatalogCacheEntry{}, false
	}
	entry.Items = append([]serviceModelCatalogItem(nil), entry.Items...)
	return entry, true
}

func (c *serviceModelCatalogCache) Put(key string, entry serviceModelCatalogCacheEntry) {
	if c == nil || key == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries == nil {
		c.entries = map[string]serviceModelCatalogCacheEntry{}
	}
	entry.Items = append([]serviceModelCatalogItem(nil), entry.Items...)
	c.entries[key] = entry
	for c.max > 0 && len(c.entries) > c.max {
		oldestKey := ""
		oldestAt := time.Time{}
		for key, entry := range c.entries {
			if oldestKey == "" || entry.FetchedAt.Before(oldestAt) {
				oldestKey = key
				oldestAt = entry.FetchedAt
			}
		}
		delete(c.entries, oldestKey)
	}
}

func (c *serviceModelCatalogCache) Clear() {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries = map[string]serviceModelCatalogCacheEntry{}
}

func (s *serviceServer) fetchConfigureModelCatalog(ctx context.Context, provider, kind, category string, userFiltered bool) ([]serviceModelCatalogItem, error) {
	profile, ok := s.config.ProviderProfile(provider)
	if !ok {
		return nil, fmt.Errorf("provider is not configured: %s", provider)
	}
	base := strings.TrimRight(strings.TrimSpace(profile.APIBase), "/")
	if base == "" {
		return nil, fmt.Errorf("provider missing API base: %s", provider)
	}
	endpoint := base + "/models"
	query := url.Values{}
	if provider == "openrouter" {
		if kind == "embeddings" {
			endpoint = base + "/embeddings/models"
		} else if userFiltered && strings.TrimSpace(profile.APIKey) != "" {
			endpoint = base + "/models/user"
		}
		if category != "" {
			query.Set("category", category)
		}
	} else if kind == "embeddings" && strings.Contains(base, "openrouter.ai") {
		endpoint = base + "/embeddings/models"
	}
	if len(query) > 0 {
		endpoint += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(profile.APIKey) != "" {
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(profile.APIKey))
	}
	req.Header.Set("Accept", "application/json")
	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("model list failed: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	var payload struct {
		Data []map[string]any `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&payload); err != nil {
		return nil, err
	}
	items := make([]serviceModelCatalogItem, 0, len(payload.Data))
	for _, raw := range payload.Data {
		item := serviceModelCatalogItem{
			ID:          serviceString(raw["id"]),
			Name:        serviceString(raw["name"]),
			Description: serviceString(raw["description"]),
			Provider:    provider,
			Pricing:     serviceMap(raw["pricing"]),
		}
		if item.ID == "" {
			continue
		}
		item.ContextLength = serviceInt(raw["context_length"])
		if item.ContextLength == 0 {
			item.ContextLength = serviceInt(raw["contextLength"])
		}
		if arch := serviceMap(raw["architecture"]); arch != nil {
			item.InputModalities = serviceStringSlice(arch["input_modalities"])
			item.OutputModalities = serviceStringSlice(arch["output_modalities"])
		}
		if topProvider := serviceMap(raw["top_provider"]); topProvider != nil {
			item.RawProvider = serviceString(topProvider["name"])
		}
		if kind == "embeddings" && len(item.OutputModalities) > 0 && !slices.Contains(item.OutputModalities, "embeddings") {
			continue
		}
		items = append(items, item)
	}
	slices.SortFunc(items, func(a, b serviceModelCatalogItem) int {
		return strings.Compare(strings.ToLower(a.ID), strings.ToLower(b.ID))
	})
	return items, nil
}

func serviceString(value any) string {
	if value == nil {
		return ""
	}
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v)
	default:
		return strings.TrimSpace(fmt.Sprint(v))
	}
}

func serviceMap(value any) map[string]any {
	if value == nil {
		return nil
	}
	if v, ok := value.(map[string]any); ok {
		return v
	}
	return nil
}

func serviceInt(value any) int {
	switch v := value.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	case json.Number:
		n, _ := v.Int64()
		return int(n)
	case string:
		n, _ := strconv.Atoi(strings.TrimSpace(v))
		return n
	default:
		return 0
	}
}

func serviceStringSlice(value any) []string {
	raw, ok := value.([]any)
	if !ok {
		if strings.TrimSpace(serviceString(value)) == "" {
			return nil
		}
		return []string{serviceString(value)}
	}
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		if s := serviceString(item); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func (s *serviceServer) handleConfigureProviderTest(w http.ResponseWriter, r *http.Request) {
	limitServiceRequestBody(w, r, serviceConfigureBodyLimit)
	var body struct {
		Role     string `json:"role"`
		Provider string `json:"provider"`
		Model    string `json:"model"`
	}
	if err := decodeServiceRequestBody(r.Body, &body); err != nil {
		writeServiceRequestDecodeError(w, err)
		return
	}
	cfg := s.config
	roleName := strings.TrimSpace(body.Role)
	if roleName == "" {
		roleName = config.ModelRoleChat
	}
	role := cfg.ModelRole(roleName)
	if strings.TrimSpace(body.Provider) != "" {
		role.Primary.Provider = strings.TrimSpace(body.Provider)
	}
	if strings.TrimSpace(body.Model) != "" {
		role.Primary.Model = strings.TrimSpace(body.Model)
	}
	client := newModelRefClient(cfg, role.Primary, 15*time.Second)
	if client == nil {
		writeServiceJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "provider is not configured"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	switch roleName {
	case config.ModelRoleEmbeddings:
		_, err := client.Embed(ctx, role.Primary.Model, "or3 provider test")
		if err != nil {
			writeServiceJSON(w, http.StatusBadGateway, map[string]any{"ok": false, "error": err.Error(), "transient": providers.IsTransientError(err)})
			return
		}
	default:
		_, err := client.Chat(ctx, providers.ChatCompletionRequest{Model: role.Primary.Model, Messages: []providers.ChatMessage{{Role: "user", Content: "Reply with ok."}}, Temperature: 0})
		if err != nil {
			writeServiceJSON(w, http.StatusBadGateway, map[string]any{"ok": false, "error": err.Error(), "transient": providers.IsTransientError(err)})
			return
		}
	}
	writeServiceJSON(w, http.StatusOK, map[string]any{"ok": true})
}
