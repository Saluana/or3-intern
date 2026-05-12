package main

import (
	"strings"
	"sync"
	"time"
)

type serviceNonceReplayGuard struct {
	mu      sync.Mutex
	seen    map[string]time.Time
	maxSize int
}

func newServiceNonceReplayGuard(maxSize int) *serviceNonceReplayGuard {
	if maxSize <= 0 {
		maxSize = 4096
	}
	return &serviceNonceReplayGuard{seen: map[string]time.Time{}, maxSize: maxSize}
}

func (g *serviceNonceReplayGuard) Accept(nonce string, expiresAt time.Time, now time.Time) bool {
	if g == nil {
		return true
	}
	nonce = strings.TrimSpace(nonce)
	if nonce == "" {
		return false
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	for existing, expires := range g.seen {
		if !expires.After(now) {
			delete(g.seen, existing)
		}
	}
	if _, ok := g.seen[nonce]; ok {
		return false
	}
	if len(g.seen) >= g.maxSize {
		var oldestNonce string
		var oldest time.Time
		for existing, expires := range g.seen {
			if oldestNonce == "" || expires.Before(oldest) {
				oldestNonce = existing
				oldest = expires
			}
		}
		delete(g.seen, oldestNonce)
	}
	g.seen[nonce] = expiresAt
	return true
}
