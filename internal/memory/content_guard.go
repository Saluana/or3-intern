package memory

import (
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

const (
	MaxPinKeyLen     = 64
	MaxPinContentLen = 4000
	MaxNoteTextLen   = 8000
)

var (
	pinKeyPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)
	secretPatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)\b(api[_-]?key|apikey)\b\s*[:=]\s*\S+`),
		regexp.MustCompile(`(?i)\b(bearer|authorization)\b\s*[:=]\s*\S+`),
		regexp.MustCompile(`(?i)\b(password|passwd|pwd|secret|token|private[_-]?key)\b\s*[:=]\s*\S+`),
		regexp.MustCompile(`(?i)\b(session[_-]?id|cookie|set-cookie)\b\s*[:=]\s*\S+`),
		regexp.MustCompile(`(?i)sk-[a-z0-9]{10,}`),
		regexp.MustCompile(`(?i)-----BEGIN (RSA |EC |OPENSSH )?PRIVATE KEY-----`),
		regexp.MustCompile(`(?i)\bAKIA[0-9A-Z]{16}\b`),
	}
)

// ValidatePinKey checks pinned-memory key format and length.
func ValidatePinKey(key string) error {
	key = strings.TrimSpace(key)
	if key == "" {
		return fmt.Errorf("pin key is required")
	}
	if utf8.RuneCountInString(key) > MaxPinKeyLen {
		return fmt.Errorf("pin key must be at most %d characters", MaxPinKeyLen)
	}
	if !pinKeyPattern.MatchString(key) {
		return fmt.Errorf("pin key must start with a letter and use lowercase letters, numbers, or underscores")
	}
	return nil
}

// ValidatePinContent checks pinned value length and rejects secret-like text.
func ValidatePinContent(content string) error {
	content = strings.TrimSpace(content)
	if content == "" {
		return fmt.Errorf("pin content is required")
	}
	if utf8.RuneCountInString(content) > MaxPinContentLen {
		return fmt.Errorf("pin content must be at most %d characters", MaxPinContentLen)
	}
	return rejectSecretLikeContent(content, "pin content")
}

// ValidateNoteText checks note length and rejects secret-like text.
func ValidateNoteText(text string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return fmt.Errorf("note text is required")
	}
	if utf8.RuneCountInString(text) > MaxNoteTextLen {
		return fmt.Errorf("note text must be at most %d characters", MaxNoteTextLen)
	}
	return rejectSecretLikeContent(text, "note text")
}

// RejectSecretLikeStrings validates each string in a list (consolidation/tool output).
func RejectSecretLikeStrings(items []string, field string) error {
	for _, item := range items {
		if err := rejectSecretLikeContent(item, field); err != nil {
			return err
		}
	}
	return nil
}

func rejectSecretLikeContent(text, field string) error {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return nil
	}
	for _, pattern := range secretPatterns {
		if pattern.MatchString(trimmed) {
			return fmt.Errorf("%s appears to contain credential or secret material", field)
		}
	}
	return nil
}
