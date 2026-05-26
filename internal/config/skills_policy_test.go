package config

import "testing"

func TestEnsureSkillsExecTrustPolicy_FillsEmptyTrustLists(t *testing.T) {
	cfg := Default()
	cfg.Skills.EnableExec = true
	cfg.Skills.Policy.TrustedOwners = nil
	cfg.Skills.Policy.TrustedRegistries = nil
	cfg.Skills.ClawHub.RegistryURL = "https://example.test/skills"

	EnsureSkillsExecTrustPolicy(&cfg)

	if len(cfg.Skills.Policy.TrustedOwners) != 1 || cfg.Skills.Policy.TrustedOwners[0] != "local" {
		t.Fatalf("TrustedOwners = %#v, want [local]", cfg.Skills.Policy.TrustedOwners)
	}
	if len(cfg.Skills.Policy.TrustedRegistries) != 1 || cfg.Skills.Policy.TrustedRegistries[0] != "https://example.test/skills" {
		t.Fatalf("TrustedRegistries = %#v, want claw hub registry", cfg.Skills.Policy.TrustedRegistries)
	}
}

func TestEnsureSkillsExecTrustPolicy_NoOpWhenDisabled(t *testing.T) {
	cfg := Default()
	cfg.Skills.EnableExec = false

	EnsureSkillsExecTrustPolicy(&cfg)

	if len(cfg.Skills.Policy.TrustedOwners) != 0 || len(cfg.Skills.Policy.TrustedRegistries) != 0 {
		t.Fatalf("expected empty trust policy, got owners=%#v registries=%#v", cfg.Skills.Policy.TrustedOwners, cfg.Skills.Policy.TrustedRegistries)
	}
}
