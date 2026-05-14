package log

import (
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"
)

const DefaultBufferSize = 1000

type Entry struct {
	ID        string            `json:"id"`
	Timestamp time.Time         `json:"timestamp"`
	Level     Level             `json:"level"`
	Component string            `json:"component"`
	Message   string            `json:"message"`
	TraceID   string            `json:"trace_id,omitempty"`
	Session   string            `json:"session,omitempty"`
	Fields    map[string]string `json:"fields,omitempty"`
}

type Filter struct {
	MinLevel  Level
	Component string
	TraceID   string
	Session   string
}

type Buffer struct {
	mu          sync.Mutex
	max         int
	next        uint64
	entries     []Entry
	subscribers map[chan Entry]struct{}
}

func NewBuffer(max int) *Buffer {
	if max <= 0 {
		max = DefaultBufferSize
	}
	return &Buffer{max: max, subscribers: map[chan Entry]struct{}{}}
}

func (b *Buffer) Append(entry Entry) Entry {
	if b == nil {
		return entry
	}
	entry = normalizeEntry(entry)
	b.mu.Lock()
	b.next++
	entry.ID = fmt.Sprintf("log_%d", b.next)
	if len(b.entries) >= b.max {
		copy(b.entries, b.entries[1:])
		b.entries[len(b.entries)-1] = entry
	} else {
		b.entries = append(b.entries, entry)
	}
	for subscriber := range b.subscribers {
		select {
		case subscriber <- entry:
		default:
		}
	}
	b.mu.Unlock()
	return entry
}

func (b *Buffer) Snapshot(filter Filter) []Entry {
	if b == nil {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]Entry, 0, len(b.entries))
	for _, entry := range b.entries {
		if filter.Matches(entry) {
			out = append(out, entry)
		}
	}
	return out
}

func (b *Buffer) Subscribe(bufferSize int) (<-chan Entry, func()) {
	if bufferSize <= 0 {
		bufferSize = 64
	}
	ch := make(chan Entry, bufferSize)
	if b == nil {
		close(ch)
		return ch, func() {}
	}
	b.mu.Lock()
	b.subscribers[ch] = struct{}{}
	b.mu.Unlock()
	var once sync.Once
	unsubscribe := func() {
		once.Do(func() {
			b.mu.Lock()
			delete(b.subscribers, ch)
			close(ch)
			b.mu.Unlock()
		})
	}
	return ch, unsubscribe
}

func (f Filter) Matches(entry Entry) bool {
	minLevel := f.MinLevel
	if minLevel == "" {
		minLevel = LevelInfo
	}
	if levelRank(entry.Level) < levelRank(minLevel) {
		return false
	}
	if f.Component != "" && !strings.EqualFold(entry.Component, f.Component) {
		return false
	}
	if f.TraceID != "" && entry.TraceID != f.TraceID {
		return false
	}
	if f.Session != "" && entry.Session != f.Session {
		return false
	}
	return true
}

func normalizeEntry(entry Entry) Entry {
	if entry.Timestamp.IsZero() {
		entry.Timestamp = time.Now().UTC()
	}
	if entry.Level == "" {
		entry.Level = LevelInfo
	}
	entry.Component = strings.TrimSpace(entry.Component)
	entry.Message = redact(strings.TrimSpace(entry.Message))
	entry.Fields = redactFields(entry.Fields)
	entry.TraceID = strings.TrimSpace(entry.TraceID)
	entry.Session = strings.TrimSpace(entry.Session)
	if entry.Component == "" {
		entry.Component = inferComponent(entry.Message)
	}
	if entry.TraceID == "" {
		entry.TraceID = extractKeyValue(entry.Message, "trace")
	}
	if entry.TraceID == "" {
		entry.TraceID = extractKeyValue(entry.Message, "trace_id")
	}
	if entry.Session == "" {
		entry.Session = extractKeyValue(entry.Message, "session")
	}
	if len([]rune(entry.Message)) > 2000 {
		runes := []rune(entry.Message)
		entry.Message = string(runes[:2000]) + "..."
	}
	return entry
}

func inferComponent(message string) string {
	message = strings.TrimSpace(message)
	if message == "" {
		return "service"
	}
	if index := strings.Index(message, ":"); index > 0 && index <= 48 {
		candidate := strings.TrimSpace(message[:index])
		if componentNamePattern.MatchString(candidate) {
			return strings.ReplaceAll(candidate, " ", "_")
		}
	}
	if strings.HasPrefix(message, "service ") {
		return "service"
	}
	return "service"
}

func inferLevel(message string) Level {
	lower := strings.ToLower(message)
	switch {
	case strings.Contains(lower, "error") || strings.Contains(lower, "failed") || strings.Contains(lower, "panic"):
		return LevelError
	case strings.Contains(lower, "warn") || strings.Contains(lower, "timeout") || strings.Contains(lower, "unavailable"):
		return LevelWarn
	default:
		return LevelInfo
	}
}

var componentNamePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_. -]*$`)
var stdlibPrefixPattern = regexp.MustCompile(`^\d{4}/\d{2}/\d{2} \d{2}:\d{2}:\d{2}(?:\.\d+)?\s+`)
var keyValuePattern = regexp.MustCompile(`\b([A-Za-z0-9_]+)=([^\s]+)`)
var redactionPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\bBearer\s+[A-Za-z0-9._~+/=-]+`),
	regexp.MustCompile(`(?i)(token|secret|password|authorization)=([^\s]+)`),
	regexp.MustCompile(`(?i)"(token|secret|password|authorization)"\s*:\s*"[^"]+"`),
}

func normalizeStdlibLine(line string) string {
	return strings.TrimSpace(stdlibPrefixPattern.ReplaceAllString(strings.TrimSpace(line), ""))
}

func extractKeyValue(message, key string) string {
	for _, match := range keyValuePattern.FindAllStringSubmatch(message, -1) {
		if len(match) == 3 && strings.EqualFold(match[1], key) {
			value := strings.Trim(match[2], `"'`)
			if value == "" || value == "<nil>" {
				return ""
			}
			return value
		}
	}
	return ""
}

func redact(message string) string {
	for _, pattern := range redactionPatterns {
		message = pattern.ReplaceAllStringFunc(message, func(match string) string {
			if strings.HasPrefix(strings.ToLower(match), "bearer ") {
				return "Bearer [redacted]"
			}
			parts := strings.SplitN(match, "=", 2)
			if len(parts) == 2 {
				return parts[0] + "=[redacted]"
			}
			if index := strings.Index(match, ":"); index > 0 {
				return match[:index+1] + ` "[redacted]"`
			}
			return "[redacted]"
		})
	}
	return message
}

func redactFields(fields map[string]string) map[string]string {
	if len(fields) == 0 {
		return nil
	}
	out := make(map[string]string, len(fields))
	for key, value := range fields {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		if isSensitiveFieldKey(key) {
			out[key] = "[redacted]"
			continue
		}
		out[key] = redact(strings.TrimSpace(value))
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func isSensitiveFieldKey(key string) bool {
	key = strings.ToLower(strings.TrimSpace(key))
	if key == "" {
		return false
	}
	for _, token := range []string{"token", "secret", "password", "authorization", "api_key", "apikey", "api-key"} {
		if key == token || strings.Contains(key, token) {
			return true
		}
	}
	return false
}
