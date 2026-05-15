package agent

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"or3-intern/internal/bus"
)

type sessionLock struct {
	mu   sync.Mutex
	refs int
}

func (r *Runtime) acquireSessionLock(key string) *sessionLock {
	r.locksMu.Lock()
	if r.locks == nil {
		r.locks = map[string]*sessionLock{}
	}
	entry := r.locks[key]
	if entry == nil {
		entry = &sessionLock{}
		r.locks[key] = entry
	}
	entry.refs++
	r.locksMu.Unlock()
	return entry
}

func (r *Runtime) releaseSessionLock(key string, entry *sessionLock) {
	if r == nil || entry == nil {
		return
	}
	r.locksMu.Lock()
	if entry.refs > 0 {
		entry.refs--
	}
	if entry.refs == 0 {
		if current := r.locks[key]; current == entry {
			delete(r.locks, key)
		}
	}
	r.locksMu.Unlock()
}

func (r *Runtime) markSessionActivity(sessionKey string) {
	if r == nil || strings.TrimSpace(sessionKey) == "" {
		return
	}
	r.idleMu.Lock()
	defer r.idleMu.Unlock()
	if r.idleVersion == nil {
		r.idleVersion = map[string]uint64{}
	}
	r.idleVersion[sessionKey]++
	if r.idleTimers != nil {
		if timer := r.idleTimers[sessionKey]; timer != nil {
			timer.Stop()
			delete(r.idleTimers, sessionKey)
		}
	}
}

func (r *Runtime) scheduleIdlePrune(ctx context.Context, ev bus.Event) {
	if r == nil || !r.ContextManager.Enabled || r.DB == nil || strings.TrimSpace(ev.SessionKey) == "" {
		return
	}
	if r.contextManagerProvider() == nil && (r.Consolidator == nil || r.Consolidator.Provider == nil) {
		return
	}
	delay := time.Duration(r.ContextManager.IdlePruneSeconds) * time.Second
	if delay <= 0 {
		delay = 5 * time.Minute
	}
	sessionKey := ev.SessionKey
	channel := ev.Channel
	replyTarget := deliveryTarget(ev)
	meta := cloneMap(ev.Meta)
	r.idleMu.Lock()
	if r.idleTimers == nil {
		r.idleTimers = map[string]*time.Timer{}
	}
	version := r.idleVersion[sessionKey]
	if timer := r.idleTimers[sessionKey]; timer != nil {
		timer.Stop()
	}
	r.idleTimers[sessionKey] = time.AfterFunc(delay, func() {
		r.runIdlePrune(context.Background(), sessionKey, channel, replyTarget, meta, version)
	})
	r.idleMu.Unlock()
}

func (r *Runtime) runIdlePrune(ctx context.Context, sessionKey, channel, replyTarget string, meta map[string]any, expectedVersion uint64) {
	entry := r.acquireSessionLock(sessionKey)
	entry.mu.Lock()
	defer func() {
		entry.mu.Unlock()
		r.releaseSessionLock(sessionKey, entry)
	}()
	if !r.sessionIdleVersionMatches(sessionKey, expectedVersion) {
		return
	}
	msg, err := r.pruneSessionContext(ctx, sessionKey, "idle")
	if err != nil {
		msg = "Context prune skipped. Cause: " + oneLine(err.Error(), 180)
	}
	if r.Deliver != nil && strings.TrimSpace(channel) != "" && strings.TrimSpace(replyTarget) != "" {
		if err := r.deliver(ctx, channel, replyTarget, msg, meta); err != nil {
			log.Printf("deliver idle prune notice failed: %v", err)
		}
	}
}

func (r *Runtime) sessionIdleVersionMatches(sessionKey string, expected uint64) bool {
	r.idleMu.Lock()
	defer r.idleMu.Unlock()
	if r.idleTimers != nil {
		delete(r.idleTimers, sessionKey)
	}
	return r.idleVersion != nil && r.idleVersion[sessionKey] == expected
}

func (r *Runtime) ensureSessionScope(ctx context.Context, ev bus.Event) {
	if r == nil || r.DB == nil || strings.TrimSpace(ev.SessionKey) == "" {
		return
	}
	scopeKey, ok := r.scopeKeyForEvent(ev)
	if !ok {
		return
	}
	scopeKey = strings.TrimSpace(scopeKey)
	if scopeKey == "" || scopeKey == ev.SessionKey {
		return
	}
	meta := map[string]any{"auto": true, "channel": ev.Channel}
	_ = r.DB.LinkSession(ctx, ev.SessionKey, scopeKey, meta)
}

func (r *Runtime) scopeKeyForEvent(ev bus.Event) (string, bool) {
	if r == nil {
		return "", false
	}
	if scopeKey := strings.TrimSpace(r.IdentityScopeMap[ev.SessionKey]); scopeKey != "" {
		return scopeKey, true
	}
	if r.LinkDirectMessages && isDirectMessageEvent(ev) {
		scopeKey := strings.TrimSpace(r.DefaultScopeKey)
		if scopeKey == "" {
			scopeKey = ev.SessionKey
		}
		return scopeKey, true
	}
	return "", false
}

func isDirectMessageEvent(ev bus.Event) bool {
	if len(ev.Meta) == 0 {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(ev.Channel)) {
	case "telegram":
		return strings.EqualFold(strings.TrimSpace(fmt.Sprint(ev.Meta["chat_type"])), "private")
	case "slack":
		return strings.EqualFold(strings.TrimSpace(fmt.Sprint(ev.Meta["channel_type"])), "im")
	case "discord":
		if v, ok := ev.Meta["is_private"].(bool); ok {
			return v
		}
		return strings.TrimSpace(fmt.Sprint(ev.Meta["guild_id"])) == ""
	case "whatsapp":
		if v, ok := ev.Meta["is_group"].(bool); ok {
			return !v
		}
	case "email":
		return true
	}
	return false
}

func (r *Runtime) handleNewSession(ctx context.Context, ev bus.Event) error {
	replyTarget := deliveryTarget(ev)
	if r.Consolidator != nil && r.Builder != nil {
		historyMax := r.Builder.HistoryMax
		if historyMax <= 0 {
			historyMax = 40
		}
		if err := r.Consolidator.ArchiveResetWindow(ctx, ev.SessionKey, historyMax); err != nil {
			log.Printf("new session archive failed: session=%s err=%v", ev.SessionKey, err)
			msg := "Memory archival failed, session not cleared. Cause: " + oneLine(err.Error(), 180)
			if r.Deliver != nil {
				if derr := r.deliver(ctx, ev.Channel, replyTarget, msg, ev.Meta); derr != nil {
					log.Printf("deliver failed: %v", derr)
				}
			}
			return nil
		}
	}
	if err := r.DB.ResetSessionHistory(ctx, ev.SessionKey); err != nil {
		log.Printf("new session reset failed: session=%s err=%v", ev.SessionKey, err)
		msg := "New session failed. Cause: " + oneLine(err.Error(), 180)
		if r.Deliver != nil {
			if derr := r.deliver(ctx, ev.Channel, replyTarget, msg, ev.Meta); derr != nil {
				log.Printf("deliver failed: %v", derr)
			}
		}
		return nil
	}
	if r.Deliver != nil {
		deliverCtx := ContextWithConversationAction(ctx, ConversationActionSessionReset)
		if err := r.deliver(deliverCtx, ev.Channel, replyTarget, "New session started.", ev.Meta); err != nil {
			log.Printf("deliver failed: %v", err)
		}
	}
	return nil
}

func (r *Runtime) handlePruneSession(ctx context.Context, ev bus.Event, reason string) error {
	msg, err := r.pruneSessionContext(ctx, ev.SessionKey, reason)
	if err != nil {
		log.Printf("context prune failed: session=%s err=%v", ev.SessionKey, err)
		msg = "Context prune failed. Cause: " + oneLine(err.Error(), 180)
	}
	if r.Deliver != nil {
		if derr := r.deliver(ctx, ev.Channel, deliveryTarget(ev), msg, ev.Meta); derr != nil {
			log.Printf("deliver failed: %v", derr)
		}
	}
	return nil
}
