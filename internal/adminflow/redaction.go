package adminflow

import (
	"fmt"
	"regexp"
	"strings"
)

// RedactValue creates a redacted representation of a config value.
// For secret fields, it returns presence/empty status instead of the raw value.
func RedactValue(value any, isSecret bool) RedactedValue {
	if isSecret {
		return redactSecret(value)
	}
	return RedactedValue{
		Value:   value,
		Present: value != nil && value != "",
	}
}

// redactSecret handles redaction for secret fields.
func redactSecret(value any) RedactedValue {
	if value == nil {
		return RedactedValue{
			Redacted: true,
			Present:  false,
			Summary:  "not set",
		}
	}

	str, ok := value.(string)
	if !ok {
		return RedactedValue{
			Redacted: true,
			Present:  true,
			Summary:  "configured",
		}
	}

	str = strings.TrimSpace(str)
	if str == "" {
		return RedactedValue{
			Redacted: true,
			Present:  false,
			Summary:  "not set",
		}
	}

	// Show length hint for configured secrets
	length := len(str)
	summary := "configured"
	if length > 0 {
		summary = fmt.Sprintf("configured (%d chars)", length)
	}

	return RedactedValue{
		Redacted: true,
		Present:  true,
		Summary:  summary,
	}
}

// RedactString redacts sensitive information from a string.
// Useful for redacting log lines, config comments, and other text.
func RedactString(text string) string {
	result := text

	// Redact API keys and secrets first.
	result = regexp.MustCompile(`(?i)(api[_-]?key|apikey)\s*[:=]\s*["']?([^\s"']+)["']?`).ReplaceAllString(result, `$1=***REDACTED***`)
	result = regexp.MustCompile(`(?i)(token|secret|password|passwd)\s*[:=]\s*["']?([^\s"']+)["']?`).ReplaceAllString(result, `$1=***REDACTED***`)
	result = regexp.MustCompile(`(?i)\bBearer\s+([a-zA-Z0-9_\-.]+)`).ReplaceAllString(result, `Bearer=***REDACTED***`)
	result = regexp.MustCompile(`(?i)\bBasic\s+([a-zA-Z0-9+/=]+)`).ReplaceAllString(result, `Basic=***REDACTED***`)

	// Redact email addresses (partial) - keep domain
	emailPattern := regexp.MustCompile(`([a-zA-Z0-9._%+-]+)@([a-zA-Z0-9.-]+)\.([a-zA-Z]{2,})`)
	result = emailPattern.ReplaceAllString(result, "***@***.$3")

	// Redact file paths that look like credentials (only match actual paths with /)
	pathPatterns := []*regexp.Regexp{
		regexp.MustCompile(`(?i)(/[^\s]*)*(credentials?|secrets?|keys?|tokens?)(\.json|\.yaml|\.yml|\.env)`),
		regexp.MustCompile(`(?i)\.aws/credentials`),
		regexp.MustCompile(`(?i)\.ssh/[a-zA-Z0-9_]+`),
	}

	for _, pattern := range pathPatterns {
		result = pattern.ReplaceAllString(result, "***PATH_REDACTED***")
	}

	return result
}

// RedactEnvMap redacts sensitive values from an environment variable map.
func RedactEnvMap(env map[string]string) map[string]string {
	if env == nil {
		return nil
	}

	result := make(map[string]string, len(env))
	sensitiveKeys := map[string]bool{
		"api_key":       true,
		"apikey":        true,
		"token":         true,
		"secret":        true,
		"password":      true,
		"passwd":        true,
		"auth":          true,
		"credential":    true,
		"private_key":   true,
		"access_key":    true,
		"client_secret": true,
	}

	for key, value := range env {
		lowerKey := strings.ToLower(key)
		isSensitive := false
		for sensitiveKey := range sensitiveKeys {
			if strings.Contains(lowerKey, sensitiveKey) {
				isSensitive = true
				break
			}
		}

		if isSensitive {
			if strings.TrimSpace(value) == "" {
				result[key] = "***NOT_SET***"
			} else {
				result[key] = "***REDACTED***"
			}
		} else {
			result[key] = value
		}
	}

	return result
}

// RedactJSON redacts sensitive fields from a JSON-like map.
func RedactJSON(data map[string]any) map[string]any {
	if data == nil {
		return nil
	}

	result := make(map[string]any, len(data))
	sensitiveKeys := map[string]bool{
		"api_key":       true,
		"apikey":        true,
		"token":         true,
		"secret":        true,
		"password":      true,
		"passwd":        true,
		"credential":    true,
		"private_key":   true,
		"access_key":    true,
		"client_secret": true,
		"refresh_token": true,
		"access_token":  true,
	}

	for key, value := range data {
		lowerKey := strings.ToLower(key)
		isSensitive := false
		for sensitiveKey := range sensitiveKeys {
			if strings.Contains(lowerKey, sensitiveKey) {
				isSensitive = true
				break
			}
		}

		if isSensitive {
			if value == nil || value == "" {
				result[key] = "***NOT_SET***"
			} else {
				result[key] = "***REDACTED***"
			}
		} else {
			// Recursively redact nested maps
			if nested, ok := value.(map[string]any); ok {
				result[key] = RedactJSON(nested)
			} else {
				result[key] = value
			}
		}
	}

	return result
}

// IsPromptInjection checks if text contains potential prompt injection patterns.
func IsPromptInjection(text string) bool {
	lower := strings.ToLower(text)
	injectionPatterns := []string{
		"ignore previous instructions",
		"ignore all instructions",
		"disregard previous",
		"forget everything",
		"you are now",
		"act as if",
		"pretend you are",
		"system:",
		"assistant:",
		"user:",
		"[inst]",
		"[system]",
		"<script>",
		"javascript:",
	}

	for _, pattern := range injectionPatterns {
		if strings.Contains(lower, pattern) {
			return true
		}
	}

	return false
}

// SanitizeForAI prepares text for AI consumption by redacting secrets and
// marking potential prompt injection.
func SanitizeForAI(text string) string {
	// First redact sensitive data
	redacted := RedactString(text)

	// Mark potential prompt injection
	if IsPromptInjection(redacted) {
		redacted = "[UNTRUSTED CONTENT DETECTED] " + redacted
	}

	return redacted
}
