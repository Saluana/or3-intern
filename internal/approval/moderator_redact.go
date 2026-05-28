package approval

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
)

var (
	moderatorSecretPatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)\b(api[_-]?key|apikey)\b\s*[:=]\s*\S+`),
		regexp.MustCompile(`(?i)\b(bearer|authorization)\b\s*[:=]\s*\S+`),
		regexp.MustCompile(`(?i)\b(password|passwd|pwd|secret|token|private[_-]?key)\b\s*[:=]\s*\S+`),
		regexp.MustCompile(`(?i)sk-[a-z0-9]{10,}`),
		regexp.MustCompile(`(?i)\bAKIA[0-9A-Z]{16}\b`),
		regexp.MustCompile(`(?i)[A-Za-z0-9_\-]{20,}\.[A-Za-z0-9_\-]{20,}\.[A-Za-z0-9_\-]{10,}`),
	}
	moderatorApprovalTokenPattern = regexp.MustCompile(`[A-Za-z0-9_-]{20,}\.[A-Fa-f0-9]{32,}`)
)

type redactionStats struct {
	Secrets     int
	Tokens      int
	Truncations int
}

func mergeRedactionStats(dst *redactionStats, src redactionStats) {
	dst.Secrets += src.Secrets
	dst.Tokens += src.Tokens
	dst.Truncations += src.Truncations
}

func redactModeratorText(text string, maxChars int) (string, redactionStats) {
	stats := redactionStats{}
	if maxChars <= 0 {
		maxChars = 4000
	}
	out := strings.TrimSpace(text)
	for _, pattern := range moderatorSecretPatterns {
		if pattern.MatchString(out) {
			stats.Secrets++
		}
		out = pattern.ReplaceAllString(out, "[REDACTED]")
	}
	if moderatorApprovalTokenPattern.MatchString(out) {
		stats.Tokens++
		out = moderatorApprovalTokenPattern.ReplaceAllString(out, "[REDACTED_TOKEN]")
	}
	if len(out) > maxChars {
		stats.Truncations++
		out = out[:maxChars] + "…"
	}
	return out, stats
}

func redactModeratorMap(values map[string]any, maxChars int) (map[string]any, redactionStats) {
	stats := redactionStats{}
	if len(values) == 0 {
		return map[string]any{}, stats
	}
	out := make(map[string]any, len(values))
	for key, value := range values {
		redacted, itemStats := redactModeratorValue(value, maxChars)
		out[key] = redacted
		mergeRedactionStats(&stats, itemStats)
	}
	return out, stats
}

func redactModeratorValue(value any, maxChars int) (any, redactionStats) {
	switch typed := value.(type) {
	case string:
		return redactModeratorText(typed, maxChars)
	case []string:
		items := make([]string, 0, len(typed))
		stats := redactionStats{}
		for _, item := range typed {
			redacted, itemStats := redactModeratorText(item, maxChars)
			items = append(items, redacted)
			mergeRedactionStats(&stats, itemStats)
		}
		return items, stats
	case []any:
		items := make([]any, 0, len(typed))
		stats := redactionStats{}
		for _, item := range typed {
			redacted, itemStats := redactModeratorValue(item, maxChars)
			items = append(items, redacted)
			mergeRedactionStats(&stats, itemStats)
		}
		return items, stats
	case map[string]any:
		return redactModeratorMap(typed, maxChars)
	default:
		text := strings.TrimSpace(strings.Trim(fmt.Sprint(typed), "\""))
		if text == "" {
			return value, redactionStats{}
		}
		return redactModeratorText(text, maxChars)
	}
}

func redactRequesterContext(requester RequesterContext) map[string]any {
	channel := strings.TrimSpace(requester.Channel)
	sessionKey := hashForDiagnostics(requester.SessionKey)
	return map[string]any{
		"channel":           channel,
		"session_key_hash":  sessionKey,
		"has_reply_meta":    len(requester.ReplyMeta) > 0,
		"has_reply_target":  strings.TrimSpace(requester.ReplyTarget) != "",
		"has_source_msg_id": strings.TrimSpace(requester.SourceMessageID) != "",
	}
}

func hashForDiagnostics(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:8])
}
