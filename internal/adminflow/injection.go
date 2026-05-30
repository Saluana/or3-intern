package adminflow

import (
	"regexp"
	"strings"
)

var reRoleMarkerInjection = regexp.MustCompile(`(?im)^\s*(system|user|assistant):\s`)

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

	return reRoleMarkerInjection.MatchString(text)
}

// SanitizeForAI prepares text for AI consumption by redacting secrets and
// marking potential prompt injection.
func SanitizeForAI(text string) string {
	redacted := RedactString(text)
	if IsPromptInjection(redacted) {
		return "[UNTRUSTED CONTENT DETECTED] " + redacted
	}
	return redacted
}
