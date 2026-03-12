package channels

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// FormatRateLimitError returns a stable, user-facing error for upstream
// delivery throttling so callers can distinguish it from generic API failures.
func FormatRateLimitError(service string, retryAfter time.Duration, detail string) error {
	service = strings.TrimSpace(service)
	if service == "" {
		service = "remote service"
	}
	detail = strings.TrimSpace(detail)
	if retryAfter > 0 {
		if detail != "" {
			return fmt.Errorf("%s rate limited: retry after %s (%s)", service, retryAfter.Round(time.Millisecond), detail)
		}
		return fmt.Errorf("%s rate limited: retry after %s", service, retryAfter.Round(time.Millisecond))
	}
	if detail != "" {
		return fmt.Errorf("%s rate limited: %s", service, detail)
	}
	return fmt.Errorf("%s rate limited", service)
}

// ParseRetryAfterSeconds accepts the common integer or floating-point retry
// formats used by HTTP APIs and returns 0 when the value is missing/invalid.
func ParseRetryAfterSeconds(raw string) time.Duration {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0
	}
	if seconds, err := strconv.ParseFloat(raw, 64); err == nil && seconds > 0 {
		return time.Duration(seconds * float64(time.Second))
	}
	return 0
}
