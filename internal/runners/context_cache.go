package runners

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

const defaultContextCacheTTL = 5 * time.Minute

type contextCacheEntry struct {
	value     string
	expiresAt time.Time
}

// RunnerContextCache memoizes safe, non-secret prompt fragments for runner turns.
type RunnerContextCache struct {
	mu    sync.RWMutex
	ttl   time.Duration
	items map[string]contextCacheEntry
	now   func() time.Time
}

// NewRunnerContextCache constructs a bounded in-memory cache.
func NewRunnerContextCache(ttl time.Duration) *RunnerContextCache {
	if ttl <= 0 {
		ttl = defaultContextCacheTTL
	}
	return &RunnerContextCache{
		ttl:   ttl,
		items: make(map[string]contextCacheEntry),
		now:   time.Now,
	}
}

func (c *RunnerContextCache) Get(key string) (string, bool) {
	if c == nil || strings.TrimSpace(key) == "" {
		return "", false
	}
	c.mu.RLock()
	entry, ok := c.items[key]
	c.mu.RUnlock()
	if !ok || c.now().After(entry.expiresAt) {
		if ok {
			c.mu.Lock()
			delete(c.items, key)
			c.mu.Unlock()
		}
		return "", false
	}
	return entry.value, true
}

func (c *RunnerContextCache) Put(key, value string) {
	if c == nil || strings.TrimSpace(key) == "" {
		return
	}
	value = sanitizeCacheValue(value)
	if value == "" {
		return
	}
	c.mu.Lock()
	c.items[key] = contextCacheEntry{value: value, expiresAt: c.now().Add(c.ttl)}
	c.mu.Unlock()
}

// Invalidate removes a single cache entry.
func (c *RunnerContextCache) Invalidate(key string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	delete(c.items, key)
	c.mu.Unlock()
}

// InvalidateAll clears the cache.
func (c *RunnerContextCache) InvalidateAll() {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.items = make(map[string]contextCacheEntry)
	c.mu.Unlock()
}

// BootstrapCacheKey keys bootstrap file content by path and file metadata.
func BootstrapCacheKey(path string, info os.FileInfo) string {
	if info == nil {
		return hashKey("bootstrap", path)
	}
	return hashKey("bootstrap", path, info.ModTime().UTC().String(), fmt.Sprintf("%d", info.Size()))
}

// SessionReplayCacheKey keys replay prompt fragments for a runner chat session.
func SessionReplayCacheKey(sessionID, lastTurnID, runnerID, model, mode, isolation string) string {
	return hashKey("replay", PromptBuilderVersion(), sessionID, lastTurnID, runnerID, model, mode, isolation)
}

func hashKey(parts ...string) string {
	h := sha256.New()
	for _, part := range parts {
		h.Write([]byte(strings.TrimSpace(part)))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil)[:16])
}

func sanitizeCacheValue(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	lower := strings.ToLower(value)
	for _, needle := range []string{"approval_token", "bearer ", "api_key", "secret", "password"} {
		if strings.Contains(lower, needle) {
			return ""
		}
	}
	return value
}

// LoadBootstrapCached reads bootstrap text from cache or `loader`.
func (c *RunnerContextCache) LoadBootstrapCached(path string, loader func() (string, error)) (string, error) {
	if c == nil {
		return loadBootstrapDirect(path, loader)
	}
	info, err := os.Stat(path)
	if err != nil {
		return loadBootstrapDirect(path, loader)
	}
	key := BootstrapCacheKey(path, info)
	if cached, ok := c.Get(key); ok {
		return cached, nil
	}
	text, err := loadBootstrapDirect(path, loader)
	if err != nil {
		return "", err
	}
	c.Put(key, text)
	return text, nil
}

func loadBootstrapDirect(path string, loader func() (string, error)) (string, error) {
	if loader != nil {
		return loader()
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}
