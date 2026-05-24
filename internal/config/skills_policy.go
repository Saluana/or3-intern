package config

import "strings"

// EnsureSkillsExecTrustPolicy fills minimal skill trust policy when skill execution
// is enabled but trustedOwners or trustedRegistries are empty. Startup validation
// requires non-empty policies; local setups often enable exec before configuring them.
func EnsureSkillsExecTrustPolicy(cfg *Config) {
	if cfg == nil || !cfg.Skills.EnableExec {
		return
	}
	if len(cfg.Skills.Policy.TrustedRegistries) == 0 {
		registry := strings.TrimSpace(cfg.Skills.ClawHub.RegistryURL)
		if registry == "" {
			registry = defaultClawHubURL
		}
		cfg.Skills.Policy.TrustedRegistries = []string{registry}
	}
	if len(cfg.Skills.Policy.TrustedOwners) == 0 {
		cfg.Skills.Policy.TrustedOwners = []string{"local"}
	}
}
