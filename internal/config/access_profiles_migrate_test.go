package config

import "testing"

func TestMigrateLegacyServiceAccessChannel_RemapsElectronLocalService(t *testing.T) {
	cfg := Default()
	cfg.Security.Profiles.Enabled = true
	cfg.Security.Profiles.Channels["service"] = LegacyElectronServiceProfile
	cfg.Service.MaxCapability = "privileged"

	MigrateLegacyServiceAccessChannel(&cfg)

	if cfg.Security.Profiles.Channels["service"] != AccessLevelAdmin {
		t.Fatalf("channels.service = %q, want %q", cfg.Security.Profiles.Channels["service"], AccessLevelAdmin)
	}
}
