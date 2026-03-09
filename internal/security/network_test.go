package security

import (
	"context"
	"net/url"
	"testing"
)

func TestHostPolicy_DefaultDenyBlocksUnknownHost(t *testing.T) {
	policy := HostPolicy{Enabled: true, DefaultDeny: true, AllowedHosts: []string{"example.com"}}
	target, _ := url.Parse("https://api.openai.com/v1/chat/completions")
	if err := policy.ValidateURL(context.Background(), target); err == nil {
		t.Fatal("expected host policy denial")
	}
}

func TestHostPolicy_AllowsWildcardHost(t *testing.T) {
	policy := HostPolicy{Enabled: true, DefaultDeny: true, AllowedHosts: []string{"*.openai.com"}}
	target, _ := url.Parse("https://api.openai.com/v1/chat/completions")
	if err := policy.ValidateURL(context.Background(), target); err != nil {
		t.Fatalf("expected wildcard host allow, got %v", err)
	}
}
