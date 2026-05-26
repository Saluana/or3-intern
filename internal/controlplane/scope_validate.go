package controlplane

import (
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

const (
	maxScopeSessionKeyLen = 256
	maxScopeKeyLen        = 256
	maxScopeMetaKeys      = 16
	maxScopeMetaValueLen  = 512
	maxScopeReasonLen     = 512
)

var scopeKeyPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9:._-]{0,255}$`)

// ValidateScopeLinkInput checks session/scope identifiers and optional metadata.
func ValidateScopeLinkInput(sessionKey, scopeKey string, meta map[string]any) error {
	sessionKey = strings.TrimSpace(sessionKey)
	scopeKey = strings.TrimSpace(scopeKey)
	if sessionKey == "" {
		return fmt.Errorf("session_key is required")
	}
	if scopeKey == "" {
		return fmt.Errorf("scope_key is required")
	}
	if len(sessionKey) > maxScopeSessionKeyLen || !scopeKeyPattern.MatchString(sessionKey) {
		return fmt.Errorf("session_key must be 1-%d characters using letters, numbers, :, ., _, or -", maxScopeSessionKeyLen)
	}
	if len(scopeKey) > maxScopeKeyLen || !scopeKeyPattern.MatchString(scopeKey) {
		return fmt.Errorf("scope_key must be 1-%d characters using letters, numbers, :, ., _, or -", maxScopeKeyLen)
	}
	return validateScopeMeta(meta)
}

func validateScopeMeta(meta map[string]any) error {
	if len(meta) == 0 {
		return nil
	}
	if len(meta) > maxScopeMetaKeys {
		return fmt.Errorf("scope metadata may contain at most %d keys", maxScopeMetaKeys)
	}
	for key, value := range meta {
		key = strings.TrimSpace(key)
		if key == "" {
			return fmt.Errorf("scope metadata keys must be non-empty")
		}
		if len(key) > 64 {
			return fmt.Errorf("scope metadata key %q is too long", key)
		}
		text := strings.TrimSpace(fmt.Sprint(value))
		if key == "reason" {
			if len(text) > maxScopeReasonLen {
				return fmt.Errorf("scope link reason must be at most %d characters", maxScopeReasonLen)
			}
			if strings.HasPrefix(strings.ToLower(text), "secret:") || strings.HasPrefix(strings.ToLower(text), "token:") {
				return fmt.Errorf("scope link reason must not use reserved prefixes")
			}
			continue
		}
		if utf8.RuneCountInString(text) > maxScopeMetaValueLen {
			return fmt.Errorf("scope metadata value for %q is too long", key)
		}
	}
	return nil
}

// ValidateScopeSessionKey validates a session key for resolve operations.
func ValidateScopeSessionKey(sessionKey string) error {
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" {
		return fmt.Errorf("session_key is required")
	}
	if len(sessionKey) > maxScopeSessionKeyLen || !scopeKeyPattern.MatchString(sessionKey) {
		return fmt.Errorf("session_key must be 1-%d characters using letters, numbers, :, ., _, or -", maxScopeSessionKeyLen)
	}
	return nil
}

// ValidateScopeKey validates a scope key for list operations.
func ValidateScopeKey(scopeKey string) error {
	scopeKey = strings.TrimSpace(scopeKey)
	if scopeKey == "" {
		return fmt.Errorf("scope_key is required")
	}
	if len(scopeKey) > maxScopeKeyLen || !scopeKeyPattern.MatchString(scopeKey) {
		return fmt.Errorf("scope_key must be 1-%d characters using letters, numbers, :, ., _, or -", maxScopeKeyLen)
	}
	return nil
}
