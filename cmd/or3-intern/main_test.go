package main

import "testing"

func TestConfigErrorHint(t *testing.T) {
	hint := configErrorHint(assertErr("telegram enabled: set channels.telegram.allowedChatIds, channels.telegram.inboundPolicy=pairing, or channels.telegram.openAccess=true"))
	if hint == "" {
		t.Fatal("expected config error hint for channel access validation error")
	}

	if got := configErrorHint(assertErr("unrelated config error")); got != "" {
		t.Fatalf("expected no hint for unrelated error, got %q", got)
	}
}

func assertErr(text string) error {
	return testError(text)
}

type testError string

func (e testError) Error() string { return string(e) }
