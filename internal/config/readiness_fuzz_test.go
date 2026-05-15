package config

import "testing"

func FuzzEvaluateReadinessDoesNotPanic(f *testing.F) {
	f.Add("", "", "", "", "")
	f.Add("service", "hosted-service", "0.0.0.0:9100", "sk-test", "safe")
	f.Fuzz(func(t *testing.T, command, runtimeProfile, listen, apiKey, maxCapability string) {
		cfg := Default()
		cfg.RuntimeProfile = RuntimeProfile(runtimeProfile)
		cfg.Service.Enabled = true
		cfg.Service.Listen = listen
		cfg.Service.MaxCapability = maxCapability
		cfg.Provider.APIKey = apiKey
		_ = EvaluateReadiness(cfg, ReadinessOptions{Command: command})
	})
}
