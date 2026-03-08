package config

import "testing"

func TestDefault_SkillsCompatibilityDefaults(t *testing.T) {
	cfg := Default()
	if cfg.Skills.ManagedDir == "" {
		t.Fatal("expected managedDir default")
	}
	if cfg.Skills.Load.Watch {
		t.Fatal("expected skills watcher off by default")
	}
	if cfg.Skills.Load.WatchDebounceMS != 250 {
		t.Fatalf("expected watch debounce 250, got %d", cfg.Skills.Load.WatchDebounceMS)
	}
	if cfg.Skills.Entries == nil {
		t.Fatal("expected entries map to be initialized")
	}
	if cfg.Skills.ClawHub.SiteURL != "https://clawhub.ai" {
		t.Fatalf("unexpected clawhub site: %q", cfg.Skills.ClawHub.SiteURL)
	}
	if cfg.Skills.ClawHub.RegistryURL != "https://clawhub.ai" {
		t.Fatalf("unexpected clawhub registry: %q", cfg.Skills.ClawHub.RegistryURL)
	}
	if cfg.Skills.ClawHub.InstallDir != "skills" {
		t.Fatalf("unexpected install dir: %q", cfg.Skills.ClawHub.InstallDir)
	}
}
