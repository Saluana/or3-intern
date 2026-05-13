package shared

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"or3-intern/internal/config"
)

type PairingBroker interface {
	IsPairedChannelIdentity(ctx context.Context, channel, identity string) (bool, error)
}

type InboundAccessInput struct {
	Policy                       config.InboundPolicy
	OpenAccess                   bool
	Allowlist                    []string
	Channel                      string
	Identity                     string
	Broker                       PairingBroker
	Normalize                    func(string) string
	OpenAccessOverridesAllowlist bool
}

func AllowInboundIdentity(ctx context.Context, input InboundAccessInput) bool {
	normalize := input.Normalize
	if normalize == nil {
		normalize = NormalizeIdentity
	}
	identity := normalize(input.Identity)
	if identity == "" {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(string(input.Policy))) {
	case string(config.InboundPolicyDeny):
		return false
	case string(config.InboundPolicyPairing):
		if input.Broker == nil {
			return false
		}
		allowed, err := input.Broker.IsPairedChannelIdentity(ctx, strings.TrimSpace(input.Channel), identity)
		return err == nil && allowed
	case string(config.InboundPolicyAllowlist):
		return MatchesAllowlist(input.Allowlist, identity, normalize)
	}
	if input.OpenAccess && (input.OpenAccessOverridesAllowlist || !HasAllowlist(input.Allowlist, normalize)) {
		return true
	}
	return MatchesAllowlist(input.Allowlist, identity, normalize)
}

func MatchesAllowlist(allowlist []string, identity string, normalize func(string) string) bool {
	if normalize == nil {
		normalize = NormalizeIdentity
	}
	identity = normalize(identity)
	for _, allowed := range allowlist {
		if normalize(allowed) == identity {
			return true
		}
	}
	return false
}

func HasAllowlist(allowlist []string, normalize func(string) string) bool {
	if normalize == nil {
		normalize = NormalizeIdentity
	}
	for _, allowed := range allowlist {
		if normalize(allowed) != "" {
			return true
		}
	}
	return false
}

func NormalizeIdentity(value string) string {
	return strings.TrimSpace(value)
}

func DedupeKey(parts ...string) string {
	clean := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			clean = append(clean, trimmed)
		}
	}
	return strings.Join(clean, ":")
}

func DefaultHTTPClient() *http.Client {
	return &http.Client{Timeout: 20 * time.Second}
}

func APIStatusError(channel string, status string, body string) error {
	message := strings.TrimSpace(channel) + " api error: " + strings.TrimSpace(status)
	if trimmed := strings.TrimSpace(body); trimmed != "" {
		return fmt.Errorf("%s %s", message, trimmed)
	}
	return fmt.Errorf("%s", message)
}
