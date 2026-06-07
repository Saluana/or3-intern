package config

import "testing"

func TestSetChannelAccessLevelEnsuresBuiltins(t *testing.T) {
	var profiles AccessProfilesConfig
	if !SetChannelAccessLevel(&profiles, "Telegram", "operator") {
		t.Fatal("expected channel access level to be set")
	}
	if !profiles.Enabled {
		t.Fatal("expected profiles to be enabled")
	}
	if profiles.Channels["telegram"] != AccessLevelOperator {
		t.Fatalf("expected telegram operator mapping, got %q", profiles.Channels["telegram"])
	}
	if profiles.Profiles[AccessLevelOperator].MaxCapability != "guarded" {
		t.Fatalf("expected operator builtin profile, got %#v", profiles.Profiles[AccessLevelOperator])
	}
}
