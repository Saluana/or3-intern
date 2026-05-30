package adminflow

import (
	"fmt"
	"regexp"
	"strings"
)

var (
	reAPIKeyRedact   = regexp.MustCompile(`(?i)(api[_-]?key|apikey)\s*[:=]\s*["']?([^\s"']+)["']?`)
	reTokenRedact    = regexp.MustCompile(`(?i)(token|secret|password|passwd)\s*[:=]\s*["']?([^\s"']+)["']?`)
	reBearerRedact   = regexp.MustCompile(`(?i)\bBearer\s+([a-zA-Z0-9_\-.]+)`)
	reBasicRedact    = regexp.MustCompile(`(?i)\bBasic\s+([a-zA-Z0-9+/=]+)`)
	reEmailRedact    = regexp.MustCompile(`([a-zA-Z0-9._%+-]+)@([a-zA-Z0-9.-]+)\.([a-zA-Z]{2,})`)
	reCredPathRedact = regexp.MustCompile(`(?i)(/[^\s]*)*(credentials?|secrets?|keys?|tokens?)(\.json|\.yaml|\.yml|\.env)`)
	reAWSCredRedact  = regexp.MustCompile(`(?i)\.aws/credentials`)
	reSSHKeyRedact   = regexp.MustCompile(`(?i)\.ssh/[a-zA-Z0-9_]+`)
)

var sensitiveKeyFragments = []string{
	"api_key",
	"apikey",
	"token",
	"secret",
	"password",
	"passwd",
	"auth",
	"credential",
	"private_key",
	"access_key",
	"client_secret",
	"refresh_token",
	"access_token",
}

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
func RedactString(text string) string {
	result := text
	result = reAPIKeyRedact.ReplaceAllString(result, `$1=***REDACTED***`)
	result = reTokenRedact.ReplaceAllString(result, `$1=***REDACTED***`)
	result = reBearerRedact.ReplaceAllString(result, `Bearer=***REDACTED***`)
	result = reBasicRedact.ReplaceAllString(result, `Basic=***REDACTED***`)
	result = reEmailRedact.ReplaceAllString(result, "***@***.$3")

	for _, pattern := range []*regexp.Regexp{reCredPathRedact, reAWSCredRedact, reSSHKeyRedact} {
		result = pattern.ReplaceAllString(result, "***PATH_REDACTED***")
	}

	return result
}

func keyLooksSensitive(key string) bool {
	lowerKey := strings.ToLower(key)
	for _, fragment := range sensitiveKeyFragments {
		if strings.Contains(lowerKey, fragment) {
			return true
		}
	}
	return false
}

// RedactJSON redacts sensitive fields from a JSON-like map.
func RedactJSON(data map[string]any) map[string]any {
	if data == nil {
		return nil
	}

	result := make(map[string]any, len(data))
	for key, value := range data {
		if keyLooksSensitive(key) {
			if value == nil || value == "" {
				result[key] = "***NOT_SET***"
			} else {
				result[key] = "***REDACTED***"
			}
			continue
		}
		if nested, ok := value.(map[string]any); ok {
			result[key] = RedactJSON(nested)
		} else {
			result[key] = value
		}
	}

	return result
}
