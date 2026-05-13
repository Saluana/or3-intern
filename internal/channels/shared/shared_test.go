package shared

import (
	"context"
	"testing"

	"or3-intern/internal/config"
)

type fakePairingBroker struct {
	allowed  bool
	channel  string
	identity string
}

func (f *fakePairingBroker) IsPairedChannelIdentity(_ context.Context, channel, identity string) (bool, error) {
	f.channel = channel
	f.identity = identity
	return f.allowed, nil
}

func TestAllowInboundIdentityDefaultsToAllowlistWhenPresent(t *testing.T) {
	input := InboundAccessInput{OpenAccess: true, Allowlist: []string{"allowed"}, Identity: "blocked"}
	if AllowInboundIdentity(context.Background(), input) {
		t.Fatal("expected non-allowlisted identity to be blocked when open access does not override allowlist")
	}
	input.Identity = "allowed"
	if !AllowInboundIdentity(context.Background(), input) {
		t.Fatal("expected allowlisted identity to be allowed")
	}
}

func TestAllowInboundIdentityOpenAccessCanOverrideAllowlist(t *testing.T) {
	input := InboundAccessInput{OpenAccess: true, OpenAccessOverridesAllowlist: true, Allowlist: []string{"allowed"}, Identity: "blocked"}
	if !AllowInboundIdentity(context.Background(), input) {
		t.Fatal("expected open access override to allow identity")
	}
}

func TestAllowInboundIdentityPairingUsesBroker(t *testing.T) {
	broker := &fakePairingBroker{allowed: true}
	input := InboundAccessInput{Policy: config.InboundPolicyPairing, Channel: "slack", Identity: "U42", Broker: broker}
	if !AllowInboundIdentity(context.Background(), input) {
		t.Fatal("expected paired identity to be allowed")
	}
	if broker.channel != "slack" || broker.identity != "U42" {
		t.Fatalf("expected broker to receive channel/identity, got %q/%q", broker.channel, broker.identity)
	}
}

func TestDedupeKeySkipsEmptyParts(t *testing.T) {
	if got := DedupeKey("telegram", "", "123"); got != "telegram:123" {
		t.Fatalf("expected dedupe key telegram:123, got %q", got)
	}
}
